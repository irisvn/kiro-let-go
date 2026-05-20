package account

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWatcherInitialSyncCreatesAndUpdatesDeclarativeAccounts(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	path := filepath.Join(t.TempDir(), "credentials.json")
	expiresAt := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	existing := &Account{
		ID:           "existing-id",
		Label:        "old-label",
		AuthMethod:   "social",
		AccessToken:  strPtr("preserved-access"),
		RefreshToken: strPtr("old-refresh"),
		ExpiresAt:    &expiresAt,
		Region:       "us-west-2",
		MachineID:    "machine-old",
		Enabled:      true,
	}
	require.NoError(t, store.Create(context.Background(), existing))
	require.NoError(t, store.RecordFailure(context.Background(), existing.ID, "preexisting failure"))
	require.NoError(t, store.RecordSuccess(context.Background(), existing.ID))

	writeCredentials(t, path, `[
		{"id":"existing-id","label":"acct1","auth_method":"social","refresh_token":"new-refresh","profile_arn":"arn","region":"us-east-1","proxy_url":"http://proxy","enabled":true},
		{"label":"acct2","auth_method":"apikey","api_key":"ksk_new","enabled":true}
	]`)

	w := testWatcher(path, store)
	require.NoError(t, w.sync(context.Background()))

	updated, err := store.Get(context.Background(), "existing-id")
	require.NoError(t, err)
	assert.Equal(t, "acct1", updated.Label)
	assert.Equal(t, "new-refresh", *updated.RefreshToken)
	assert.Equal(t, "preserved-access", *updated.AccessToken)
	assert.Equal(t, expiresAt, *updated.ExpiresAt)
	assert.Equal(t, 1, updated.SuccessCount)
	assert.Equal(t, 0, updated.FailureCount)
	assert.Equal(t, "http://proxy", *updated.ProxyURL)

	accounts, err := store.List(context.Background(), ListFilter{})
	require.NoError(t, err)
	require.Len(t, accounts, 2)
	created := accountByLabel(t, accounts, "acct2")
	assert.NotEmpty(t, created.ID)
	assert.Equal(t, "apikey", created.AuthMethod)
	assert.Equal(t, "ksk_new", *created.APIKey)
	assert.Equal(t, generateMachineID("acct2", created.ID), created.MachineID)
}

func TestWatcherLookupBySecretDeleteAndRejectInvalidShape(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	existing := &Account{Label: "old", AuthMethod: "apikey", APIKey: strPtr("ksk_same"), MachineID: "machine-old", Enabled: true}
	deleted := &Account{ID: "delete-me", Label: "gone", AuthMethod: "social", RefreshToken: strPtr("rt"), MachineID: "machine-gone", Enabled: true}
	require.NoError(t, store.Create(ctx, existing))
	require.NoError(t, store.Create(ctx, deleted))

	path := filepath.Join(t.TempDir(), "credentials.json")
	writeCredentials(t, path, `[
		{"label":"new-label","auth_method":"apikey","api_key":"ksk_same","enabled":false},
		{"id":"delete-me","_delete":true}
	]`)
	require.NoError(t, testWatcher(path, store).sync(ctx))

	accounts, err := store.List(ctx, ListFilter{})
	require.NoError(t, err)
	require.Len(t, accounts, 1)
	assert.Equal(t, existing.ID, accounts[0].ID)
	assert.Equal(t, "new-label", accounts[0].Label)
	assert.False(t, accounts[0].Enabled)
	_, err = store.Get(ctx, "delete-me")
	assert.ErrorIs(t, err, ErrNotFound)

	writeCredentials(t, path, `{"accounts":[]}`)
	require.NoError(t, testWatcher(path, store).sync(ctx))
	afterInvalid, err := store.List(ctx, ListFilter{})
	require.NoError(t, err)
	assert.Equal(t, accounts, afterInvalid)
}

func TestWatcherRunDebouncesReloads(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	path := filepath.Join(t.TempDir(), "credentials.json")
	writeCredentials(t, path, `[{"label":"initial","auth_method":"apikey","api_key":"ksk_debounce","enabled":true}]`)

	ctx, cancel := context.WithCancel(context.Background())
	w := testWatcher(path, store)
	w.debounce = 50 * time.Millisecond
	errCh := make(chan error, 1)
	go func() { errCh <- w.Run(ctx) }()

	waitForAccountLabel(t, store, "initial")
	writeCredentials(t, path, `[{"label":"intermediate","auth_method":"apikey","api_key":"ksk_debounce","enabled":true}]`)
	writeCredentials(t, path, `[{"label":"final","auth_method":"apikey","api_key":"ksk_debounce","enabled":true}]`)
	waitForAccountLabel(t, store, "final")

	cancel()
	select {
	case err := <-errCh:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not stop")
	}
}

func testWatcher(path string, store *Store) *Watcher {
	return &Watcher{
		path:     path,
		store:    store,
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		debounce: 10 * time.Millisecond,
	}
}

func writeCredentials(t *testing.T, path, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
}

func accountByLabel(t *testing.T, accounts []Account, label string) Account {
	t.Helper()
	for _, acc := range accounts {
		if acc.Label == label {
			return acc
		}
	}
	t.Fatalf("account with label %q not found", label)
	return Account{}
}

func waitForAccountLabel(t *testing.T, store *Store, label string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		accounts, err := store.List(context.Background(), ListFilter{})
		require.NoError(t, err)
		for _, acc := range accounts {
			if acc.Label == label {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for account label %q", label)
}

func strPtr(v string) *string { return &v }
