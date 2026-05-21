package account

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenDB(t *testing.T) {
	db, err := OpenDB(":memory:")
	require.NoError(t, err)
	defer db.Close()

	var busyTimeout int
	err = db.QueryRowContext(context.Background(), "PRAGMA busy_timeout").Scan(&busyTimeout)
	require.NoError(t, err)
	assert.Equal(t, 5000, busyTimeout)

	var foreignKeys int
	err = db.QueryRowContext(context.Background(), "PRAGMA foreign_keys").Scan(&foreignKeys)
	require.NoError(t, err)
	assert.Equal(t, 1, foreignKeys)
}

func TestApplyCreatesSchema(t *testing.T) {
	db, err := OpenDB(":memory:")
	require.NoError(t, err)
	defer db.Close()

	ctx := context.Background()
	require.NoError(t, Apply(ctx, db))

	assertTableExists(t, db, "accounts")
	assertTableExists(t, db, "quota_cache")
	assertTableExists(t, db, "settings")
	assertTableExists(t, db, "_migrations")
	assertIndexExists(t, db, "idx_accounts_enabled")
	assertIndexExists(t, db, "idx_accounts_auth_method")
}

func TestApplyIsIdempotent(t *testing.T) {
	db, err := OpenDB(":memory:")
	require.NoError(t, err)
	defer db.Close()

	ctx := context.Background()
	require.NoError(t, Apply(ctx, db))
	require.NoError(t, Apply(ctx, db))

	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM _migrations").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestApplyRecordsMigration(t *testing.T) {
	db, err := OpenDB(":memory:")
	require.NoError(t, err)
	defer db.Close()

	ctx := context.Background()
	require.NoError(t, Apply(ctx, db))

	var version int
	var appliedAt string
	err = db.QueryRowContext(ctx, "SELECT version, applied_at FROM _migrations WHERE version = 1").Scan(&version, &appliedAt)
	require.NoError(t, err)
	assert.Equal(t, 1, version)
	assert.NotEmpty(t, appliedAt)
}

func TestForeignKeysEnforced(t *testing.T) {
	db, err := OpenDB(":memory:")
	require.NoError(t, err)
	defer db.Close()

	ctx := context.Background()
	require.NoError(t, Apply(ctx, db))

	_, err = db.ExecContext(ctx, `
		INSERT INTO accounts (id, label, auth_method, machine_id, created_at, updated_at)
		VALUES ('acc1', 'test', 'api_key', 'machine1', '2024-01-01T00:00:00Z', '2024-01-01T00:00:00Z')
	`)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `
		INSERT INTO quota_cache (account_id, payload_json, fetched_at)
		VALUES ('acc1', '{"quota": 100}', '2024-01-01T00:00:00Z')
	`)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, "DELETE FROM accounts WHERE id = 'acc1'")
	require.NoError(t, err)

	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM quota_cache WHERE account_id = 'acc1'").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func assertTableExists(t *testing.T, db *sql.DB, name string) {
	t.Helper()
	var count int
	err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", name).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "table %s should exist", name)
}

func assertIndexExists(t *testing.T, db *sql.DB, name string) {
	t.Helper()
	var count int
	err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?", name).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "index %s should exist", name)
}
