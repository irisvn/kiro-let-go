package account

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestStore(t *testing.T) (*Store, func()) {
	t.Helper()
	db, err := OpenDB(":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)

	ctx := context.Background()
	require.NoError(t, Apply(ctx, db))

	store, err := NewStore(db)
	require.NoError(t, err)

	cleanup := func() {
		require.NoError(t, store.Close())
		require.NoError(t, db.Close())
	}

	return store, cleanup
}

func newTestAccount() *Account {
	label := "test-label"
	authMethod := "api_key"
	apiKey := "secret-key"
	region := "us-west-2"
	machineID := "machine-1"
	return &Account{
		Label:      label,
		AuthMethod: authMethod,
		APIKey:     &apiKey,
		Region:     region,
		MachineID:  machineID,
		Enabled:    true,
	}
}

func TestStoreCreate(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	acc := newTestAccount()
	require.NoError(t, store.Create(ctx, acc))

	assert.NotEmpty(t, acc.ID)
	assert.False(t, acc.CreatedAt.IsZero())
	assert.False(t, acc.UpdatedAt.IsZero())
	assert.Equal(t, acc.CreatedAt, acc.UpdatedAt)

	got, err := store.Get(ctx, acc.ID)
	require.NoError(t, err)
	assert.Equal(t, acc.ID, got.ID)
	assert.Equal(t, acc.Label, got.Label)
	assert.Equal(t, acc.AuthMethod, got.AuthMethod)
	assert.Equal(t, *acc.APIKey, *got.APIKey)
	assert.Equal(t, acc.Region, got.Region)
	assert.Equal(t, acc.MachineID, got.MachineID)
	assert.True(t, got.Enabled)
}

func TestStoreCreateWithExplicitID(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	acc := newTestAccount()
	acc.ID = "custom-id-123"
	require.NoError(t, store.Create(ctx, acc))

	assert.Equal(t, "custom-id-123", acc.ID)

	got, err := store.Get(ctx, acc.ID)
	require.NoError(t, err)
	assert.Equal(t, "custom-id-123", got.ID)
}

func TestStoreGetNotFound(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	_, err := store.Get(ctx, "nonexistent")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestStoreList(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	acc1 := newTestAccount()
	acc1.Label = "acc1"
	acc1.AuthMethod = "api_key"
	acc1.Enabled = true
	require.NoError(t, store.Create(ctx, acc1))

	acc2 := newTestAccount()
	acc2.Label = "acc2"
	acc2.AuthMethod = "oauth"
	acc2.Enabled = false
	require.NoError(t, store.Create(ctx, acc2))

	acc3 := newTestAccount()
	acc3.Label = "acc3"
	acc3.AuthMethod = "api_key"
	acc3.Enabled = true
	require.NoError(t, store.Create(ctx, acc3))

	all, err := store.List(ctx, ListFilter{})
	require.NoError(t, err)
	assert.Len(t, all, 3)

	enabled, err := store.List(ctx, ListFilter{EnabledOnly: true})
	require.NoError(t, err)
	assert.Len(t, enabled, 2)
	for _, acc := range enabled {
		assert.True(t, acc.Enabled)
	}

	apiKeyAccounts, err := store.List(ctx, ListFilter{AuthMethod: "api_key"})
	require.NoError(t, err)
	assert.Len(t, apiKeyAccounts, 2)
	for _, acc := range apiKeyAccounts {
		assert.Equal(t, "api_key", acc.AuthMethod)
	}

	filtered, err := store.List(ctx, ListFilter{EnabledOnly: true, AuthMethod: "api_key"})
	require.NoError(t, err)
	assert.Len(t, filtered, 2)
	for _, acc := range filtered {
		assert.True(t, acc.Enabled)
		assert.Equal(t, "api_key", acc.AuthMethod)
	}

	empty, err := store.List(ctx, ListFilter{AuthMethod: "none"})
	require.NoError(t, err)
	assert.Len(t, empty, 0)
}

func TestStoreUpdate(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	acc := newTestAccount()
	require.NoError(t, store.Create(ctx, acc))

	createdAt := acc.CreatedAt
	time.Sleep(10 * time.Millisecond)

	acc.Label = "updated-label"
	acc.Region = "eu-west-1"
	acc.Enabled = false
	reason := "manual disable"
	acc.DisabledReason = &reason
	require.NoError(t, store.Update(ctx, acc))

	assert.True(t, acc.UpdatedAt.After(createdAt), "updated_at should be newer than created_at")

	got, err := store.Get(ctx, acc.ID)
	require.NoError(t, err)
	assert.Equal(t, "updated-label", got.Label)
	assert.Equal(t, "eu-west-1", got.Region)
	assert.False(t, got.Enabled)
	assert.Equal(t, "manual disable", *got.DisabledReason)
	assert.Equal(t, createdAt.Truncate(time.Second), got.CreatedAt.Truncate(time.Second))
}

func TestStoreUpdateNotFound(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	acc := newTestAccount()
	acc.ID = "nonexistent"
	err := store.Update(ctx, acc)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestStoreDelete(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	acc := newTestAccount()
	require.NoError(t, store.Create(ctx, acc))

	require.NoError(t, store.Delete(ctx, acc.ID))

	_, err := store.Get(ctx, acc.ID)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestStoreDeleteNotFound(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	err := store.Delete(ctx, "nonexistent")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestStoreRecordSuccess(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	acc := newTestAccount()
	require.NoError(t, store.Create(ctx, acc))

	require.NoError(t, store.RecordFailure(ctx, acc.ID, "test failure 1"))
	require.NoError(t, store.RecordFailure(ctx, acc.ID, "test failure 2"))

	require.NoError(t, store.RecordSuccess(ctx, acc.ID))

	got, err := store.Get(ctx, acc.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, got.SuccessCount)
	assert.NotNil(t, got.LastUsedAt)
	assert.Equal(t, 0, got.FailureCount)

	require.NoError(t, store.RecordSuccess(ctx, acc.ID))

	got, err = store.Get(ctx, acc.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, got.SuccessCount)
}

func TestStoreRecordSuccessNotFound(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	err := store.RecordSuccess(ctx, "nonexistent")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestStoreRecordFailure(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	acc := newTestAccount()
	require.NoError(t, store.Create(ctx, acc))

	require.NoError(t, store.RecordFailure(ctx, acc.ID, "test failure 1"))

	got, err := store.Get(ctx, acc.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, got.FailureCount)
	assert.NotNil(t, got.LastFailureAt)

	require.NoError(t, store.RecordFailure(ctx, acc.ID, "test failure 2"))

	got, err = store.Get(ctx, acc.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, got.FailureCount)
}

func TestStoreRecordFailureNotFound(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	err := store.RecordFailure(ctx, "nonexistent", "missing account")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestStoreSetEnabled(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	acc := newTestAccount()
	require.NoError(t, store.Create(ctx, acc))

	reason := "quota exceeded"
	require.NoError(t, store.SetEnabled(ctx, acc.ID, false, &reason))

	got, err := store.Get(ctx, acc.ID)
	require.NoError(t, err)
	assert.False(t, got.Enabled)
	assert.Equal(t, "quota exceeded", *got.DisabledReason)

	require.NoError(t, store.SetEnabled(ctx, acc.ID, true, nil))

	got, err = store.Get(ctx, acc.ID)
	require.NoError(t, err)
	assert.True(t, got.Enabled)
	assert.Nil(t, got.DisabledReason)
}

func TestStoreSetEnabledNotFound(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	err := store.SetEnabled(ctx, "nonexistent", false, nil)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestStoreUpsertQuota(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	acc := newTestAccount()
	require.NoError(t, store.Create(ctx, acc))

	qc := &QuotaCache{
		AccountID:   acc.ID,
		PayloadJSON: `{"quota": 100}`,
		FetchedAt:   time.Now().UTC(),
	}
	require.NoError(t, store.UpsertQuota(ctx, qc))

	got, err := store.GetQuota(ctx, acc.ID)
	require.NoError(t, err)
	assert.Equal(t, `{"quota": 100}`, got.PayloadJSON)

	qc.PayloadJSON = `{"quota": 200}`
	qc.FetchedAt = time.Now().UTC().Add(time.Hour)
	require.NoError(t, store.UpsertQuota(ctx, qc))

	got, err = store.GetQuota(ctx, acc.ID)
	require.NoError(t, err)
	assert.Equal(t, `{"quota": 200}`, got.PayloadJSON)
}

func TestStoreGetQuotaNotFound(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	_, err := store.GetQuota(ctx, "nonexistent")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestStoreQuotaCascadeDelete(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	acc := newTestAccount()
	require.NoError(t, store.Create(ctx, acc))

	qc := &QuotaCache{
		AccountID:   acc.ID,
		PayloadJSON: `{"quota": 100}`,
		FetchedAt:   time.Now().UTC(),
	}
	require.NoError(t, store.UpsertQuota(ctx, qc))

	require.NoError(t, store.Delete(ctx, acc.ID))

	_, err := store.GetQuota(ctx, acc.ID)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestStoreNullableFields(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	acc := newTestAccount()
	acc.AccessToken = nil
	acc.RefreshToken = nil
	acc.APIKey = nil
	acc.ExpiresAt = nil
	acc.ProfileARN = nil
	acc.AuthRegion = nil
	acc.APIRegion = nil
	acc.ProxyURL = nil
	acc.ProxyUsername = nil
	acc.ProxyPassword = nil
	acc.DisabledReason = nil
	acc.LastFailureAt = nil
	acc.LastUsedAt = nil

	require.NoError(t, store.Create(ctx, acc))

	got, err := store.Get(ctx, acc.ID)
	require.NoError(t, err)
	assert.Nil(t, got.AccessToken)
	assert.Nil(t, got.RefreshToken)
	assert.Nil(t, got.APIKey)
	assert.Nil(t, got.ExpiresAt)
	assert.Nil(t, got.ProfileARN)
	assert.Nil(t, got.AuthRegion)
	assert.Nil(t, got.APIRegion)
	assert.Nil(t, got.ProxyURL)
	assert.Nil(t, got.ProxyUsername)
	assert.Nil(t, got.ProxyPassword)
	assert.Nil(t, got.DisabledReason)
	assert.Nil(t, got.LastFailureAt)
	assert.Nil(t, got.LastUsedAt)
}

func TestStoreAllFieldsRoundTrip(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	accessToken := "access-token"
	refreshToken := "refresh-token"
	apiKey := "api-key"
	expiresAt := time.Now().UTC().Add(time.Hour)
	profileARN := "arn:aws:iam::123456789012:role/MyRole"
	authRegion := "us-east-1"
	apiRegion := "us-west-2"
	proxyURL := "http://proxy.example.com:8080"
	proxyUsername := "proxy-user"
	proxyPassword := "proxy-pass"
	disabledReason := "test reason"
	lastFailureAt := time.Now().UTC().Add(-time.Hour)
	lastUsedAt := time.Now().UTC().Add(-30 * time.Minute)

	acc := &Account{
		ID:             "full-test-id",
		Label:          "full-test",
		AuthMethod:     "oauth",
		AccessToken:    &accessToken,
		RefreshToken:   &refreshToken,
		APIKey:         &apiKey,
		ExpiresAt:      &expiresAt,
		ProfileARN:     &profileARN,
		Region:         "ap-southeast-1",
		AuthRegion:     &authRegion,
		APIRegion:      &apiRegion,
		MachineID:      "machine-full",
		ProxyURL:       &proxyURL,
		ProxyUsername:  &proxyUsername,
		ProxyPassword:  &proxyPassword,
		Enabled:        false,
		DisabledReason: &disabledReason,
		FailureCount:   3,
		LastFailureAt:  &lastFailureAt,
		SuccessCount:   5,
		LastUsedAt:     &lastUsedAt,
	}

	require.NoError(t, store.Create(ctx, acc))

	got, err := store.Get(ctx, acc.ID)
	require.NoError(t, err)

	assert.Equal(t, acc.ID, got.ID)
	assert.Equal(t, acc.Label, got.Label)
	assert.Equal(t, acc.AuthMethod, got.AuthMethod)
	assert.Equal(t, *acc.AccessToken, *got.AccessToken)
	assert.Equal(t, *acc.RefreshToken, *got.RefreshToken)
	assert.Equal(t, *acc.APIKey, *got.APIKey)
	assert.Equal(t, acc.ExpiresAt.Format(time.RFC3339), got.ExpiresAt.Format(time.RFC3339))
	assert.Equal(t, *acc.ProfileARN, *got.ProfileARN)
	assert.Equal(t, acc.Region, got.Region)
	assert.Equal(t, *acc.AuthRegion, *got.AuthRegion)
	assert.Equal(t, *acc.APIRegion, *got.APIRegion)
	assert.Equal(t, acc.MachineID, got.MachineID)
	assert.Equal(t, *acc.ProxyURL, *got.ProxyURL)
	assert.Equal(t, *acc.ProxyUsername, *got.ProxyUsername)
	assert.Equal(t, *acc.ProxyPassword, *got.ProxyPassword)
	assert.Equal(t, acc.Enabled, got.Enabled)
	assert.Equal(t, *acc.DisabledReason, *got.DisabledReason)
	assert.Equal(t, acc.FailureCount, got.FailureCount)
	assert.Equal(t, acc.LastFailureAt.Format(time.RFC3339), got.LastFailureAt.Format(time.RFC3339))
	assert.Equal(t, acc.SuccessCount, got.SuccessCount)
	assert.Equal(t, acc.LastUsedAt.Format(time.RFC3339), got.LastUsedAt.Format(time.RFC3339))
}
