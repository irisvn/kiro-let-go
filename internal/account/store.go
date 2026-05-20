package account

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/irisvn/kiro-let-go/internal/errs"
	_ "modernc.org/sqlite"
)

var immediateTxOptions = &sql.TxOptions{Isolation: sql.LevelSerializable}

// Store provides CRUD operations for accounts backed by SQLite.
type Store struct {
	db *sql.DB

	stmtCreate          *sql.Stmt
	stmtGet             *sql.Stmt
	stmtList            *sql.Stmt
	stmtListEnabled     *sql.Stmt
	stmtListAuthMethod  *sql.Stmt
	stmtListEnabledAuth *sql.Stmt
	stmtUpdate          *sql.Stmt
	stmtDelete          *sql.Stmt
	stmtRecordSuccess   *sql.Stmt
	stmtRecordFailure   *sql.Stmt
	stmtSetEnabled      *sql.Stmt
	stmtUpsertQuota     *sql.Stmt
	stmtGetQuota        *sql.Stmt
}

// NewStore creates a new Store with prepared statements.
func NewStore(db *sql.DB) (*Store, error) {
	ctx := context.Background()

	stmtCreate, err := db.PrepareContext(ctx, `
		INSERT INTO accounts (
			id, label, auth_method, access_token, refresh_token, api_key,
			expires_at, profile_arn, region, auth_region, api_region,
			machine_id, proxy_url, proxy_username, proxy_password,
			enabled, disabled_reason, failure_count, last_failure_at,
			success_count, last_used_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return nil, fmt.Errorf("prepare create: %w", err)
	}

	stmtGet, err := db.PrepareContext(ctx, `
		SELECT id, label, auth_method, access_token, refresh_token, api_key,
			expires_at, profile_arn, region, auth_region, api_region,
			machine_id, proxy_url, proxy_username, proxy_password,
			enabled, disabled_reason, failure_count, last_failure_at,
			success_count, last_used_at, created_at, updated_at
		FROM accounts WHERE id = ?
	`)
	if err != nil {
		return nil, fmt.Errorf("prepare get: %w", err)
	}

	stmtList, err := db.PrepareContext(ctx, `
		SELECT id, label, auth_method, access_token, refresh_token, api_key,
			expires_at, profile_arn, region, auth_region, api_region,
			machine_id, proxy_url, proxy_username, proxy_password,
			enabled, disabled_reason, failure_count, last_failure_at,
			success_count, last_used_at, created_at, updated_at
		FROM accounts
		ORDER BY created_at
	`)
	if err != nil {
		return nil, fmt.Errorf("prepare list: %w", err)
	}

	stmtListEnabled, err := db.PrepareContext(ctx, `
		SELECT id, label, auth_method, access_token, refresh_token, api_key,
			expires_at, profile_arn, region, auth_region, api_region,
			machine_id, proxy_url, proxy_username, proxy_password,
			enabled, disabled_reason, failure_count, last_failure_at,
			success_count, last_used_at, created_at, updated_at
		FROM accounts WHERE enabled = 1
		ORDER BY created_at
	`)
	if err != nil {
		return nil, fmt.Errorf("prepare list enabled: %w", err)
	}

	stmtListAuthMethod, err := db.PrepareContext(ctx, `
		SELECT id, label, auth_method, access_token, refresh_token, api_key,
			expires_at, profile_arn, region, auth_region, api_region,
			machine_id, proxy_url, proxy_username, proxy_password,
			enabled, disabled_reason, failure_count, last_failure_at,
			success_count, last_used_at, created_at, updated_at
		FROM accounts WHERE auth_method = ?
		ORDER BY created_at
	`)
	if err != nil {
		return nil, fmt.Errorf("prepare list auth method: %w", err)
	}

	stmtListEnabledAuth, err := db.PrepareContext(ctx, `
		SELECT id, label, auth_method, access_token, refresh_token, api_key,
			expires_at, profile_arn, region, auth_region, api_region,
			machine_id, proxy_url, proxy_username, proxy_password,
			enabled, disabled_reason, failure_count, last_failure_at,
			success_count, last_used_at, created_at, updated_at
		FROM accounts WHERE enabled = 1 AND auth_method = ?
		ORDER BY created_at
	`)
	if err != nil {
		return nil, fmt.Errorf("prepare list enabled auth: %w", err)
	}

	stmtUpdate, err := db.PrepareContext(ctx, `
		UPDATE accounts SET
			label = ?, auth_method = ?, access_token = ?, refresh_token = ?,
			api_key = ?, expires_at = ?, profile_arn = ?, region = ?,
			auth_region = ?, api_region = ?, machine_id = ?, proxy_url = ?,
			proxy_username = ?, proxy_password = ?, enabled = ?,
			disabled_reason = ?, updated_at = ?
		WHERE id = ?
	`)
	if err != nil {
		return nil, fmt.Errorf("prepare update: %w", err)
	}

	stmtDelete, err := db.PrepareContext(ctx, `DELETE FROM accounts WHERE id = ?`)
	if err != nil {
		return nil, fmt.Errorf("prepare delete: %w", err)
	}

	stmtRecordSuccess, err := db.PrepareContext(ctx, `
		UPDATE accounts SET
			success_count = success_count + 1,
			last_used_at = ?,
			failure_count = 0
		WHERE id = ?
	`)
	if err != nil {
		return nil, fmt.Errorf("prepare record success: %w", err)
	}

	stmtRecordFailure, err := db.PrepareContext(ctx, `
		UPDATE accounts SET
			failure_count = failure_count + 1,
			last_failure_at = ?
		WHERE id = ?
	`)
	if err != nil {
		return nil, fmt.Errorf("prepare record failure: %w", err)
	}

	stmtSetEnabled, err := db.PrepareContext(ctx, `
		UPDATE accounts SET enabled = ?, disabled_reason = ? WHERE id = ?
	`)
	if err != nil {
		return nil, fmt.Errorf("prepare set enabled: %w", err)
	}

	stmtUpsertQuota, err := db.PrepareContext(ctx, `
		INSERT INTO quota_cache (account_id, payload_json, fetched_at)
		VALUES (?, ?, ?)
		ON CONFLICT(account_id) DO UPDATE SET
			payload_json = excluded.payload_json,
			fetched_at = excluded.fetched_at
	`)
	if err != nil {
		return nil, fmt.Errorf("prepare upsert quota: %w", err)
	}

	stmtGetQuota, err := db.PrepareContext(ctx, `
		SELECT payload_json, fetched_at FROM quota_cache WHERE account_id = ?
	`)
	if err != nil {
		return nil, fmt.Errorf("prepare get quota: %w", err)
	}

	return &Store{
		db:                  db,
		stmtCreate:          stmtCreate,
		stmtGet:             stmtGet,
		stmtList:            stmtList,
		stmtListEnabled:     stmtListEnabled,
		stmtListAuthMethod:  stmtListAuthMethod,
		stmtListEnabledAuth: stmtListEnabledAuth,
		stmtUpdate:          stmtUpdate,
		stmtDelete:          stmtDelete,
		stmtRecordSuccess:   stmtRecordSuccess,
		stmtRecordFailure:   stmtRecordFailure,
		stmtSetEnabled:      stmtSetEnabled,
		stmtUpsertQuota:     stmtUpsertQuota,
		stmtGetQuota:        stmtGetQuota,
	}, nil
}

// Close releases all prepared statements.
func (s *Store) Close() error {
	var firstErr error
	for _, stmt := range []*sql.Stmt{
		s.stmtCreate, s.stmtGet, s.stmtList, s.stmtListEnabled,
		s.stmtListAuthMethod, s.stmtListEnabledAuth, s.stmtUpdate,
		s.stmtDelete, s.stmtRecordSuccess, s.stmtRecordFailure,
		s.stmtSetEnabled, s.stmtUpsertQuota, s.stmtGetQuota,
	} {
		if stmt != nil {
			if err := stmt.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// Create inserts a new account. If acc.ID is empty, a UUID v4 is generated.
// CreatedAt and UpdatedAt are set to the current time.
func (s *Store) Create(ctx context.Context, acc *Account) error {
	if acc.ID == "" {
		acc.ID = uuid.New().String()
	}
	now := time.Now().UTC()
	acc.CreatedAt = now
	acc.UpdatedAt = now

	tx, err := s.db.BeginTx(ctx, immediateTxOptions)
	if err != nil {
		return errs.Wrap(err, errs.ClassFatal, "begin create transaction")
	}
	defer tx.Rollback()

	_, err = tx.StmtContext(ctx, s.stmtCreate).ExecContext(ctx,
		acc.ID, acc.Label, acc.AuthMethod,
		strPtrToNullString(acc.AccessToken),
		strPtrToNullString(acc.RefreshToken),
		strPtrToNullString(acc.APIKey),
		timePtrToNullString(acc.ExpiresAt),
		strPtrToNullString(acc.ProfileARN),
		acc.Region,
		strPtrToNullString(acc.AuthRegion),
		strPtrToNullString(acc.APIRegion),
		acc.MachineID,
		strPtrToNullString(acc.ProxyURL),
		strPtrToNullString(acc.ProxyUsername),
		strPtrToNullString(acc.ProxyPassword),
		boolToInt(acc.Enabled),
		strPtrToNullString(acc.DisabledReason),
		acc.FailureCount,
		timePtrToNullString(acc.LastFailureAt),
		acc.SuccessCount,
		timePtrToNullString(acc.LastUsedAt),
		acc.CreatedAt.Format(time.RFC3339),
		acc.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return errs.Wrap(err, errs.ClassFatal, "insert account")
	}

	if err := tx.Commit(); err != nil {
		return errs.Wrap(err, errs.ClassFatal, "commit create transaction")
	}
	return nil
}

// Get retrieves an account by ID. Returns ErrNotFound if the account does not exist.
func (s *Store) Get(ctx context.Context, id string) (*Account, error) {
	row := s.stmtGet.QueryRowContext(ctx, id)
	acc, err := scanAccount(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, errs.Wrap(err, errs.ClassFatal, "get account")
	}
	return acc, nil
}

// List retrieves accounts matching the provided filter.
func (s *Store) List(ctx context.Context, filter ListFilter) ([]Account, error) {
	var rows *sql.Rows
	var err error

	switch {
	case filter.EnabledOnly && filter.AuthMethod != "":
		rows, err = s.stmtListEnabledAuth.QueryContext(ctx, filter.AuthMethod)
	case filter.EnabledOnly:
		rows, err = s.stmtListEnabled.QueryContext(ctx)
	case filter.AuthMethod != "":
		rows, err = s.stmtListAuthMethod.QueryContext(ctx, filter.AuthMethod)
	default:
		rows, err = s.stmtList.QueryContext(ctx)
	}
	if err != nil {
		return nil, errs.Wrap(err, errs.ClassFatal, "list accounts")
	}
	defer rows.Close()

	return scanAccounts(rows)
}

// Update modifies an existing account. The caller must ensure the account exists.
func (s *Store) Update(ctx context.Context, acc *Account) error {
	acc.UpdatedAt = time.Now().UTC()

	tx, err := s.db.BeginTx(ctx, immediateTxOptions)
	if err != nil {
		return errs.Wrap(err, errs.ClassFatal, "begin update transaction")
	}
	defer tx.Rollback()

	res, err := tx.StmtContext(ctx, s.stmtUpdate).ExecContext(ctx,
		acc.Label, acc.AuthMethod,
		strPtrToNullString(acc.AccessToken),
		strPtrToNullString(acc.RefreshToken),
		strPtrToNullString(acc.APIKey),
		timePtrToNullString(acc.ExpiresAt),
		strPtrToNullString(acc.ProfileARN),
		acc.Region,
		strPtrToNullString(acc.AuthRegion),
		strPtrToNullString(acc.APIRegion),
		acc.MachineID,
		strPtrToNullString(acc.ProxyURL),
		strPtrToNullString(acc.ProxyUsername),
		strPtrToNullString(acc.ProxyPassword),
		boolToInt(acc.Enabled),
		strPtrToNullString(acc.DisabledReason),
		acc.UpdatedAt.Format(time.RFC3339),
		acc.ID,
	)
	if err != nil {
		return errs.Wrap(err, errs.ClassFatal, "update account")
	}

	n, err := res.RowsAffected()
	if err != nil {
		return errs.Wrap(err, errs.ClassFatal, "check update rows affected")
	}
	if n == 0 {
		return ErrNotFound
	}

	if err := tx.Commit(); err != nil {
		return errs.Wrap(err, errs.ClassFatal, "commit update transaction")
	}
	return nil
}

// Delete removes an account by ID.
func (s *Store) Delete(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, immediateTxOptions)
	if err != nil {
		return errs.Wrap(err, errs.ClassFatal, "begin delete transaction")
	}
	defer tx.Rollback()

	res, err := tx.StmtContext(ctx, s.stmtDelete).ExecContext(ctx, id)
	if err != nil {
		return errs.Wrap(err, errs.ClassFatal, "delete account")
	}

	n, err := res.RowsAffected()
	if err != nil {
		return errs.Wrap(err, errs.ClassFatal, "check delete rows affected")
	}
	if n == 0 {
		return ErrNotFound
	}

	if err := tx.Commit(); err != nil {
		return errs.Wrap(err, errs.ClassFatal, "commit delete transaction")
	}
	return nil
}

// RecordSuccess atomically increments success_count, sets last_used_at to now,
// and resets failure_count to zero.
func (s *Store) RecordSuccess(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, immediateTxOptions)
	if err != nil {
		return errs.Wrap(err, errs.ClassFatal, "begin record success transaction")
	}
	defer tx.Rollback()

	res, err := tx.StmtContext(ctx, s.stmtRecordSuccess).ExecContext(ctx,
		time.Now().UTC().Format(time.RFC3339), id,
	)
	if err != nil {
		return errs.Wrap(err, errs.ClassFatal, "record success")
	}

	n, err := res.RowsAffected()
	if err != nil {
		return errs.Wrap(err, errs.ClassFatal, "check record success rows affected")
	}
	if n == 0 {
		return ErrNotFound
	}

	if err := tx.Commit(); err != nil {
		return errs.Wrap(err, errs.ClassFatal, "commit record success transaction")
	}
	return nil
}

// RecordFailure atomically increments failure_count and sets last_failure_at to now.
func (s *Store) RecordFailure(ctx context.Context, id string, reason string) error {
	slog.Warn("recording account failure", "account_id", id, "reason", reason)

	tx, err := s.db.BeginTx(ctx, immediateTxOptions)
	if err != nil {
		return errs.Wrap(err, errs.ClassFatal, "begin record failure transaction")
	}
	defer tx.Rollback()

	res, err := tx.StmtContext(ctx, s.stmtRecordFailure).ExecContext(ctx,
		time.Now().UTC().Format(time.RFC3339), id,
	)
	if err != nil {
		return errs.Wrap(err, errs.ClassFatal, "record failure")
	}

	n, err := res.RowsAffected()
	if err != nil {
		return errs.Wrap(err, errs.ClassFatal, "check record failure rows affected")
	}
	if n == 0 {
		return ErrNotFound
	}

	if err := tx.Commit(); err != nil {
		return errs.Wrap(err, errs.ClassFatal, "commit record failure transaction")
	}
	return nil
}

// SetEnabled atomically updates the enabled status and disabled reason.
func (s *Store) SetEnabled(ctx context.Context, id string, enabled bool, reason *string) error {
	tx, err := s.db.BeginTx(ctx, immediateTxOptions)
	if err != nil {
		return errs.Wrap(err, errs.ClassFatal, "begin set enabled transaction")
	}
	defer tx.Rollback()

	res, err := tx.StmtContext(ctx, s.stmtSetEnabled).ExecContext(ctx,
		boolToInt(enabled), strPtrToNullString(reason), id,
	)
	if err != nil {
		return errs.Wrap(err, errs.ClassFatal, "set enabled")
	}

	n, err := res.RowsAffected()
	if err != nil {
		return errs.Wrap(err, errs.ClassFatal, "check set enabled rows affected")
	}
	if n == 0 {
		return ErrNotFound
	}

	if err := tx.Commit(); err != nil {
		return errs.Wrap(err, errs.ClassFatal, "commit set enabled transaction")
	}
	return nil
}

// UpsertQuota inserts or updates quota cache for an account.
func (s *Store) UpsertQuota(ctx context.Context, qc *QuotaCache) error {
	tx, err := s.db.BeginTx(ctx, immediateTxOptions)
	if err != nil {
		return errs.Wrap(err, errs.ClassFatal, "begin upsert quota transaction")
	}
	defer tx.Rollback()

	_, err = tx.StmtContext(ctx, s.stmtUpsertQuota).ExecContext(ctx,
		qc.AccountID, qc.PayloadJSON, qc.FetchedAt.Format(time.RFC3339),
	)
	if err != nil {
		return errs.Wrap(err, errs.ClassFatal, "upsert quota")
	}

	if err := tx.Commit(); err != nil {
		return errs.Wrap(err, errs.ClassFatal, "commit upsert quota transaction")
	}
	return nil
}

// GetQuota retrieves quota cache for an account. Returns ErrNotFound if missing.
func (s *Store) GetQuota(ctx context.Context, accountID string) (*QuotaCache, error) {
	row := s.stmtGetQuota.QueryRowContext(ctx, accountID)

	var payloadJSON string
	var fetchedAtStr string
	err := row.Scan(&payloadJSON, &fetchedAtStr)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, errs.Wrap(err, errs.ClassFatal, "get quota")
	}

	fetchedAt, err := time.Parse(time.RFC3339, fetchedAtStr)
	if err != nil {
		return nil, errs.Wrap(err, errs.ClassFatal, "parse quota fetched_at")
	}

	return &QuotaCache{
		AccountID:   accountID,
		PayloadJSON: payloadJSON,
		FetchedAt:   fetchedAt,
	}, nil
}

// scanAccount scans a single account from a QueryRow result.
func scanAccount(row *sql.Row) (*Account, error) {
	var acc Account
	var accessToken, refreshToken, apiKey, expiresAtStr, profileARN, authRegion, apiRegion sql.NullString
	var proxyURL, proxyUsername, proxyPassword, disabledReason sql.NullString
	var lastFailureAtStr, lastUsedAtStr sql.NullString
	var enabledInt int
	var createdAtStr, updatedAtStr string

	err := row.Scan(
		&acc.ID, &acc.Label, &acc.AuthMethod,
		&accessToken, &refreshToken, &apiKey,
		&expiresAtStr, &profileARN, &acc.Region,
		&authRegion, &apiRegion, &acc.MachineID,
		&proxyURL, &proxyUsername, &proxyPassword,
		&enabledInt, &disabledReason,
		&acc.FailureCount, &lastFailureAtStr,
		&acc.SuccessCount, &lastUsedAtStr,
		&createdAtStr, &updatedAtStr,
	)
	if err != nil {
		return nil, err
	}

	acc.AccessToken = nullStringToStrPtr(accessToken)
	acc.RefreshToken = nullStringToStrPtr(refreshToken)
	acc.APIKey = nullStringToStrPtr(apiKey)
	acc.ExpiresAt = nullStringToTimePtr(expiresAtStr)
	acc.ProfileARN = nullStringToStrPtr(profileARN)
	acc.AuthRegion = nullStringToStrPtr(authRegion)
	acc.APIRegion = nullStringToStrPtr(apiRegion)
	acc.ProxyURL = nullStringToStrPtr(proxyURL)
	acc.ProxyUsername = nullStringToStrPtr(proxyUsername)
	acc.ProxyPassword = nullStringToStrPtr(proxyPassword)
	acc.Enabled = enabledInt != 0
	acc.DisabledReason = nullStringToStrPtr(disabledReason)
	acc.LastFailureAt = nullStringToTimePtr(lastFailureAtStr)
	acc.LastUsedAt = nullStringToTimePtr(lastUsedAtStr)

	acc.CreatedAt, err = time.Parse(time.RFC3339, createdAtStr)
	if err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}
	acc.UpdatedAt, err = time.Parse(time.RFC3339, updatedAtStr)
	if err != nil {
		return nil, fmt.Errorf("parse updated_at: %w", err)
	}

	return &acc, nil
}

// scanAccounts scans multiple accounts from a Rows result.
func scanAccounts(rows *sql.Rows) ([]Account, error) {
	var accounts []Account
	for rows.Next() {
		var acc Account
		var accessToken, refreshToken, apiKey, expiresAtStr, profileARN, authRegion, apiRegion sql.NullString
		var proxyURL, proxyUsername, proxyPassword, disabledReason sql.NullString
		var lastFailureAtStr, lastUsedAtStr sql.NullString
		var enabledInt int
		var createdAtStr, updatedAtStr string

		err := rows.Scan(
			&acc.ID, &acc.Label, &acc.AuthMethod,
			&accessToken, &refreshToken, &apiKey,
			&expiresAtStr, &profileARN, &acc.Region,
			&authRegion, &apiRegion, &acc.MachineID,
			&proxyURL, &proxyUsername, &proxyPassword,
			&enabledInt, &disabledReason,
			&acc.FailureCount, &lastFailureAtStr,
			&acc.SuccessCount, &lastUsedAtStr,
			&createdAtStr, &updatedAtStr,
		)
		if err != nil {
			return nil, err
		}

		acc.AccessToken = nullStringToStrPtr(accessToken)
		acc.RefreshToken = nullStringToStrPtr(refreshToken)
		acc.APIKey = nullStringToStrPtr(apiKey)
		acc.ExpiresAt = nullStringToTimePtr(expiresAtStr)
		acc.ProfileARN = nullStringToStrPtr(profileARN)
		acc.AuthRegion = nullStringToStrPtr(authRegion)
		acc.APIRegion = nullStringToStrPtr(apiRegion)
		acc.ProxyURL = nullStringToStrPtr(proxyURL)
		acc.ProxyUsername = nullStringToStrPtr(proxyUsername)
		acc.ProxyPassword = nullStringToStrPtr(proxyPassword)
		acc.Enabled = enabledInt != 0
		acc.DisabledReason = nullStringToStrPtr(disabledReason)
		acc.LastFailureAt = nullStringToTimePtr(lastFailureAtStr)
		acc.LastUsedAt = nullStringToTimePtr(lastUsedAtStr)

		acc.CreatedAt, err = time.Parse(time.RFC3339, createdAtStr)
		if err != nil {
			return nil, fmt.Errorf("parse created_at: %w", err)
		}
		acc.UpdatedAt, err = time.Parse(time.RFC3339, updatedAtStr)
		if err != nil {
			return nil, fmt.Errorf("parse updated_at: %w", err)
		}

		accounts = append(accounts, acc)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return accounts, nil
}

// nullStringToStrPtr converts a sql.NullString to *string.
func nullStringToStrPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	return &ns.String
}

// strPtrToNullString converts a *string to sql.NullString.
func strPtrToNullString(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}

// timePtrToNullString converts a *time.Time to sql.NullString in RFC3339 format.
func timePtrToNullString(t *time.Time) sql.NullString {
	if t == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: t.Format(time.RFC3339), Valid: true}
}

// nullStringToTimePtr converts a sql.NullString to *time.Time.
func nullStringToTimePtr(ns sql.NullString) *time.Time {
	if !ns.Valid {
		return nil
	}
	t, err := time.Parse(time.RFC3339, ns.String)
	if err != nil {
		return nil
	}
	return &t
}

// boolToInt converts a bool to an int (0 or 1).
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
