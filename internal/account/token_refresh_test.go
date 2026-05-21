package account

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockSocialAuthRefresher struct {
	access  string
	refresh string
	expires time.Time
	err     error
	calls   int
}

func (m *mockSocialAuthRefresher) Refresh(ctx context.Context, acc *Account) (string, string, time.Time, error) {
	_ = ctx
	_ = acc
	m.calls++
	return m.access, m.refresh, m.expires, m.err
}

func TestTokenRefresherRefreshesOnlyExpiringSocialAccounts(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now().UTC()

	expiringAccess := "old-access"
	expiringRefresh := "old-refresh"
	expiring := &Account{Label: "social-expiring", AuthMethod: "social", AccessToken: &expiringAccess, RefreshToken: &expiringRefresh, ExpiresAt: ptrTime(now.Add(time.Minute)), Region: "us-east-1", MachineID: "m1", Enabled: true}
	apiKey := "ksk-test"
	apikey := &Account{Label: "apikey", AuthMethod: "apikey", APIKey: &apiKey, ExpiresAt: ptrTime(now.Add(time.Minute)), Region: "us-east-1", MachineID: "m2", Enabled: true}
	farAccess := "far-access"
	farRefresh := "far-refresh"
	far := &Account{Label: "social-far", AuthMethod: "social", AccessToken: &farAccess, RefreshToken: &farRefresh, ExpiresAt: ptrTime(now.Add(time.Hour)), Region: "us-east-1", MachineID: "m3", Enabled: true}
	require.NoError(t, store.Create(ctx, expiring))
	require.NoError(t, store.Create(ctx, apikey))
	require.NoError(t, store.Create(ctx, far))

	newExpires := now.Add(time.Hour)
	auth := &mockSocialAuthRefresher{access: "new-access", refresh: "new-refresh", expires: newExpires}
	tr := NewTokenRefresher(store, auth, slog.New(slog.NewTextHandler(io.Discard, nil)))
	tr.refreshExpiring(ctx)

	assert.Equal(t, 1, auth.calls)
	got, err := store.Get(ctx, expiring.ID)
	require.NoError(t, err)
	assert.Equal(t, "new-access", *got.AccessToken)
	assert.Equal(t, "new-refresh", *got.RefreshToken)
	assert.Equal(t, newExpires.Unix(), got.ExpiresAt.Unix())

	gotFar, err := store.Get(ctx, far.ID)
	require.NoError(t, err)
	assert.Equal(t, "far-access", *gotFar.AccessToken)
}

func TestTokenRefresherDisablesExpiredAccountWhenRefreshFails(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	expires := time.Now().UTC().Add(-time.Minute)
	refresh := "refresh"
	acc := &Account{Label: "expired", AuthMethod: "social", RefreshToken: &refresh, ExpiresAt: &expires, Region: "us-east-1", MachineID: "m1", Enabled: true}
	require.NoError(t, store.Create(ctx, acc))

	auth := &mockSocialAuthRefresher{err: errors.New("boom")}
	tr := NewTokenRefresher(store, auth, slog.New(slog.NewTextHandler(io.Discard, nil)))
	tr.refreshExpiring(ctx)

	got, err := store.Get(ctx, acc.ID)
	require.NoError(t, err)
	assert.False(t, got.Enabled)
	require.NotNil(t, got.DisabledReason)
	assert.Contains(t, *got.DisabledReason, "token refresh failed: boom")
}

func ptrTime(t time.Time) *time.Time { return &t }
