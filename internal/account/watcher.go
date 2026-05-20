package account

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/google/uuid"
)

const defaultWatcherDebounce = 500 * time.Millisecond

// Watcher keeps the account store reconciled with a declarative credentials JSON file.
type Watcher struct {
	path     string
	store    *Store
	logger   *slog.Logger
	debounce time.Duration
}

// NewWatcher creates a credentials file watcher.
func NewWatcher(path string, store *Store, logger *slog.Logger) *Watcher {
	return &Watcher{path: path, store: store, logger: logger, debounce: defaultWatcherDebounce}
}

// Run performs an initial sync, then watches the credentials file's parent directory.
func (w *Watcher) Run(ctx context.Context) error {
	if w.store == nil {
		return errors.New("account watcher requires store")
	}
	if w.path == "" {
		return errors.New("account watcher requires path")
	}
	if w.logger == nil {
		w.logger = slog.Default()
	}
	if w.debounce <= 0 {
		w.debounce = defaultWatcherDebounce
	}

	if err := w.sync(ctx); err != nil {
		return err
	}

	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create fsnotify watcher: %w", err)
	}
	defer fw.Close()

	dir := filepath.Dir(w.path)
	if err := fw.Add(dir); err != nil {
		return fmt.Errorf("watch credentials directory: %w", err)
	}

	var timer *time.Timer
	var timerC <-chan time.Time
	stopTimer := func() {
		if timer != nil {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
		timer = nil
		timerC = nil
	}
	defer stopTimer()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-fw.Errors:
			if err != nil {
				w.logger.Warn("account watcher error", "error", err)
			}
		case ev := <-fw.Events:
			if !w.isCredentialsEvent(ev) {
				continue
			}
			if timer == nil {
				timer = time.NewTimer(w.debounce)
				timerC = timer.C
			} else {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(w.debounce)
			}
		case <-timerC:
			timer = nil
			timerC = nil
			if err := w.sync(ctx); err != nil {
				w.logger.Warn("account credentials sync failed", "path", w.path, "error", err)
			}
		}
	}
}

func (w *Watcher) isCredentialsEvent(ev fsnotify.Event) bool {
	if ev.Name == "" {
		return false
	}
	cleanEvent := filepath.Clean(ev.Name)
	cleanPath := filepath.Clean(w.path)
	if cleanEvent != cleanPath {
		return false
	}
	return ev.Has(fsnotify.Create) || ev.Has(fsnotify.Write) || ev.Has(fsnotify.Rename) || ev.Has(fsnotify.Remove)
}

func (w *Watcher) sync(ctx context.Context) error {
	entries, removeUnlisted, ok, err := w.parseFile()
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	return w.reconcile(ctx, entries, removeUnlisted)
}

func (w *Watcher) parseFile() ([]watcherEntry, bool, bool, error) {
	b, err := os.ReadFile(w.path)
	if err != nil {
		return nil, false, false, fmt.Errorf("read credentials file: %w", err)
	}

	var raw []json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		var object map[string]json.RawMessage
		if json.Unmarshal(b, &object) == nil {
			w.logger.Warn("account credentials file must be a JSON array", "path", w.path)
			return nil, false, false, nil
		}
		w.logger.Warn("account credentials file is invalid JSON", "path", w.path, "error", err)
		return nil, false, false, nil
	}

	entries := make([]watcherEntry, 0, len(raw))
	removeUnlisted := false
	for i, item := range raw {
		var entry watcherEntry
		if err := json.Unmarshal(item, &entry); err != nil {
			w.logger.Warn("account credentials entry is invalid", "path", w.path, "index", i, "error", err)
			return nil, false, false, nil
		}
		if entry.RemoveUnlisted {
			removeUnlisted = true
			continue
		}
		entries = append(entries, entry)
	}

	return entries, removeUnlisted, true, nil
}

func (w *Watcher) reconcile(ctx context.Context, entries []watcherEntry, removeUnlisted bool) error {
	tx, err := w.store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin credentials reconcile: %w", err)
	}
	defer tx.Rollback()

	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if entry.Delete {
			if entry.ID != "" {
				if _, err := tx.StmtContext(ctx, w.store.stmtDelete).ExecContext(ctx, entry.ID); err != nil {
					return fmt.Errorf("delete account %s: %w", entry.ID, err)
				}
			}
			continue
		}

		acc, found, err := w.findAccount(ctx, tx, entry)
		if err != nil {
			return err
		}
		if found {
			entry.applyTo(acc)
			if acc.MachineID == "" {
				acc.MachineID = generateMachineID(acc.Label, acc.ID)
			}
			if err := updateAccountTx(ctx, tx, w.store, acc); err != nil {
				return err
			}
			seen[acc.ID] = struct{}{}
			continue
		}

		acc = entry.newAccount()
		if acc.ID == "" {
			acc.ID = uuid.NewString()
		}
		acc.MachineID = generateMachineID(acc.Label, acc.ID)
		if err := createAccountTx(ctx, tx, w.store, acc); err != nil {
			return err
		}
		seen[acc.ID] = struct{}{}
	}

	if removeUnlisted {
		accounts, err := listAccountsTx(ctx, tx)
		if err != nil {
			return err
		}
		for _, acc := range accounts {
			if _, ok := seen[acc.ID]; ok {
				continue
			}
			if _, err := tx.StmtContext(ctx, w.store.stmtDelete).ExecContext(ctx, acc.ID); err != nil {
				return fmt.Errorf("delete unlisted account %s: %w", acc.ID, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit credentials reconcile: %w", err)
	}
	return nil
}

func (w *Watcher) findAccount(ctx context.Context, tx *sql.Tx, entry watcherEntry) (*Account, bool, error) {
	if entry.ID != "" {
		acc, err := getAccountTx(ctx, tx, entry.ID)
		if err != nil {
			return nil, false, err
		}
		if acc != nil {
			return acc, true, nil
		}
		return nil, false, nil
	}
	if entry.AuthMethod == "" {
		return nil, false, nil
	}
	if entry.RefreshToken != nil && *entry.RefreshToken != "" {
		acc, err := lookupAccountBySecretTx(ctx, tx, "refresh_token", entry.AuthMethod, *entry.RefreshToken)
		return acc, acc != nil, err
	}
	if entry.APIKey != nil && *entry.APIKey != "" {
		acc, err := lookupAccountBySecretTx(ctx, tx, "api_key", entry.AuthMethod, *entry.APIKey)
		return acc, acc != nil, err
	}
	return nil, false, nil
}

type watcherEntry struct {
	ID             string  `json:"id"`
	Label          string  `json:"label"`
	AuthMethod     string  `json:"auth_method"`
	AccessToken    *string `json:"access_token"`
	RefreshToken   *string `json:"refresh_token"`
	APIKey         *string `json:"api_key"`
	ExpiresAt      *string `json:"expires_at"`
	ProfileARN     *string `json:"profile_arn"`
	Region         string  `json:"region"`
	AuthRegion     *string `json:"auth_region"`
	APIRegion      *string `json:"api_region"`
	MachineID      string  `json:"machine_id"`
	ProxyURL       *string `json:"proxy_url"`
	ProxyUsername  *string `json:"proxy_username"`
	ProxyPassword  *string `json:"proxy_password"`
	Enabled        *bool   `json:"enabled"`
	DisabledReason *string `json:"disabled_reason"`
	Delete         bool    `json:"_delete"`
	RemoveUnlisted bool    `json:"_remove_unlisted"`
	fields         map[string]json.RawMessage
}

func (e *watcherEntry) UnmarshalJSON(b []byte) error {
	type alias watcherEntry
	var aux alias
	if err := json.Unmarshal(b, &aux); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil {
		return err
	}
	*e = watcherEntry(aux)
	e.fields = fields
	return nil
}

func (e watcherEntry) newAccount() *Account {
	acc := &Account{ID: e.ID, Enabled: true}
	e.applyTo(acc)
	if e.Enabled == nil {
		acc.Enabled = true
	}
	return acc
}

func (e watcherEntry) applyTo(acc *Account) {
	if e.has("label") {
		acc.Label = e.Label
	}
	if e.has("auth_method") {
		acc.AuthMethod = e.AuthMethod
	}
	if e.has("access_token") {
		acc.AccessToken = e.AccessToken
	}
	if e.has("refresh_token") {
		acc.RefreshToken = e.RefreshToken
	}
	if e.has("api_key") {
		acc.APIKey = e.APIKey
	}
	if e.has("expires_at") {
		acc.ExpiresAt = parseTimePtr(e.ExpiresAt)
	}
	if e.has("profile_arn") {
		acc.ProfileARN = e.ProfileARN
	}
	if e.has("region") {
		acc.Region = e.Region
	}
	if e.has("auth_region") {
		acc.AuthRegion = e.AuthRegion
	}
	if e.has("api_region") {
		acc.APIRegion = e.APIRegion
	}
	if e.has("machine_id") {
		acc.MachineID = e.MachineID
	}
	if e.has("proxy_url") {
		acc.ProxyURL = e.ProxyURL
	}
	if e.has("proxy_username") {
		acc.ProxyUsername = e.ProxyUsername
	}
	if e.has("proxy_password") {
		acc.ProxyPassword = e.ProxyPassword
	}
	if e.has("enabled") && e.Enabled != nil {
		acc.Enabled = *e.Enabled
	}
	if e.has("disabled_reason") {
		acc.DisabledReason = e.DisabledReason
	}
}

func (e watcherEntry) has(field string) bool {
	_, ok := e.fields[field]
	return ok
}

func parseTimePtr(v *string) *time.Time {
	if v == nil || *v == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, *v)
	if err != nil {
		return nil
	}
	return &t
}

func generateMachineID(label, id string) string {
	seed := label
	if seed == "" {
		seed = id
	}
	sum := sha256.Sum256([]byte(seed + "KiroIDE-MachineID-v1"))
	return hex.EncodeToString(sum[:])
}

func createAccountTx(ctx context.Context, tx *sql.Tx, store *Store, acc *Account) error {
	now := time.Now().UTC()
	acc.CreatedAt = now
	acc.UpdatedAt = now
	_, err := tx.StmtContext(ctx, store.stmtCreate).ExecContext(ctx,
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
		return fmt.Errorf("insert account %s: %w", acc.ID, err)
	}
	return nil
}

func updateAccountTx(ctx context.Context, tx *sql.Tx, store *Store, acc *Account) error {
	acc.UpdatedAt = time.Now().UTC()
	res, err := tx.StmtContext(ctx, store.stmtUpdate).ExecContext(ctx,
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
		acc.FailureCount,
		timePtrToNullString(acc.LastFailureAt),
		acc.UpdatedAt.Format(time.RFC3339),
		acc.ID,
	)
	if err != nil {
		return fmt.Errorf("update account %s: %w", acc.ID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("check account update %s: %w", acc.ID, err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func getAccountTx(ctx context.Context, tx *sql.Tx, id string) (*Account, error) {
	row := tx.QueryRowContext(ctx, `SELECT id, label, auth_method, access_token, refresh_token, api_key,
		expires_at, profile_arn, region, auth_region, api_region, machine_id,
		proxy_url, proxy_username, proxy_password, enabled, disabled_reason,
		failure_count, last_failure_at, success_count, last_used_at, created_at, updated_at
		FROM accounts WHERE id = ?`, id)
	acc, err := scanAccount(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get account %s: %w", id, err)
	}
	return acc, nil
}

func lookupAccountBySecretTx(ctx context.Context, tx *sql.Tx, column, authMethod, secret string) (*Account, error) {
	if column != "refresh_token" && column != "api_key" {
		return nil, fmt.Errorf("unsupported lookup column %s", column)
	}
	row := tx.QueryRowContext(ctx, fmt.Sprintf(`SELECT id, label, auth_method, access_token, refresh_token, api_key,
		expires_at, profile_arn, region, auth_region, api_region, machine_id,
		proxy_url, proxy_username, proxy_password, enabled, disabled_reason,
		failure_count, last_failure_at, success_count, last_used_at, created_at, updated_at
		FROM accounts WHERE auth_method = ? AND %s = ? ORDER BY created_at LIMIT 1`, column), authMethod, secret)
	acc, err := scanAccount(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lookup account by %s: %w", column, err)
	}
	return acc, nil
}

func listAccountsTx(ctx context.Context, tx *sql.Tx) ([]Account, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, label, auth_method, access_token, refresh_token, api_key,
		expires_at, profile_arn, region, auth_region, api_region, machine_id,
		proxy_url, proxy_username, proxy_password, enabled, disabled_reason,
		failure_count, last_failure_at, success_count, last_used_at, created_at, updated_at
		FROM accounts ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	defer rows.Close()
	accounts, err := scanAccounts(rows)
	if err != nil {
		return nil, fmt.Errorf("scan accounts: %w", err)
	}
	return accounts, nil
}
