package account

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// OpenDB opens a SQLite database with the recommended pragmas.
func OpenDB(dsn string) (*sql.DB, error) {
	if !strings.Contains(dsn, "?") {
		dsn += "?"
	} else {
		dsn += "&"
	}
	dsn += "_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	return db, nil
}

// Apply runs embedded migrations idempotently against db.
func Apply(ctx context.Context, db *sql.DB) error {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	var versions []int
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		base := strings.TrimSuffix(name, ".sql")
		parts := strings.SplitN(base, "_", 2)
		if len(parts) < 1 {
			continue
		}
		v, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}
		versions = append(versions, v)
	}

	sort.Ints(versions)

	for _, v := range versions {
		applied, err := isMigrationApplied(ctx, db, v)
		if err != nil {
			return fmt.Errorf("check migration %d: %w", v, err)
		}
		if applied {
			continue
		}

		filename := fmt.Sprintf("%04d_*.sql", v)
		entries, err := migrationsFS.ReadDir("migrations")
		if err != nil {
			return fmt.Errorf("read migrations dir: %w", err)
		}

		var match string
		for _, entry := range entries {
			if matched, _ := path.Match(filename, entry.Name()); matched {
				match = entry.Name()
				break
			}
		}
		if match == "" {
			return fmt.Errorf("migration file for version %d not found", v)
		}

		sqlBytes, err := migrationsFS.ReadFile("migrations/" + match)
		if err != nil {
			return fmt.Errorf("read migration %d: %w", v, err)
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin tx for migration %d: %w", v, err)
		}

		if _, err := tx.ExecContext(ctx, string(sqlBytes)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("exec migration %d: %w", v, err)
		}

		if _, err := tx.ExecContext(ctx,
			"INSERT INTO _migrations (version, applied_at) VALUES (?, ?)",
			v, time.Now().UTC().Format(time.RFC3339),
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %d: %w", v, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", v, err)
		}
	}

	return nil
}

func isMigrationApplied(ctx context.Context, db *sql.DB, version int) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM _migrations WHERE version = ?", version,
	).Scan(&count)
	if err != nil {
		if isNoSuchTable(err) {
			return false, nil
		}
		return false, err
	}
	return count > 0, nil
}

func isNoSuchTable(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "no such table")
}
