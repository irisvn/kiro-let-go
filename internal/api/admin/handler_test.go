package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/irisvn/kiro-let-go/internal/account"
	"github.com/irisvn/kiro-let-go/internal/kiro"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const adminAPIKey = "admin-test-key"

type fakeManager struct {
	store        *account.Store
	refreshErr   error
	refreshFn    func(ctx context.Context, accountID string) error
	refreshCalls []string
}

func (m *fakeManager) Refresh(ctx context.Context, accountID string) error {
	m.refreshCalls = append(m.refreshCalls, accountID)
	if m.refreshFn != nil {
		return m.refreshFn(ctx, accountID)
	}
	if m.refreshErr != nil {
		return m.refreshErr
	}
	acc, err := m.store.Get(ctx, accountID)
	if err != nil {
		return err
	}
	accessToken := "access-" + accountID
	expiresAt := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	acc.AccessToken = &accessToken
	acc.ExpiresAt = &expiresAt
	acc.DisabledReason = nil
	return m.store.Update(ctx, acc)
}

type fakeQuotaFetcher struct {
	getFn        func(ctx context.Context, acc *account.Account, force bool) (*account.Quota, error)
	summaryFn    func(ctx context.Context) ([]*account.QuotaSummaryItem, error)
	getCalls     []quotaGetCall
	summaryCalls int
}

type quotaGetCall struct {
	accountID string
	force     bool
}

func (f *fakeQuotaFetcher) Get(ctx context.Context, acc *account.Account, force bool) (*account.Quota, error) {
	f.getCalls = append(f.getCalls, quotaGetCall{accountID: acc.ID, force: force})
	if f.getFn != nil {
		return f.getFn(ctx, acc, force)
	}
	return nil, errors.New("unexpected quota get call")
}

func (f *fakeQuotaFetcher) Summary(ctx context.Context) ([]*account.QuotaSummaryItem, error) {
	f.summaryCalls++
	if f.summaryFn != nil {
		return f.summaryFn(ctx)
	}
	return nil, errors.New("unexpected quota summary call")
}

func TestAdminAuthCreateListAndGetRedactsSecrets(t *testing.T) {
	store, cleanup := setupAdminTestStore(t)
	defer cleanup()

	manager := &fakeManager{store: store}
	quota := &fakeQuotaFetcher{}
	circuit := account.NewCircuitBreaker(account.CircuitConfig{BaseCooldown: time.Minute, MaxBackoffMultiplier: 8}, nil)
	router := newAdminTestRouter(store, manager, quota, circuit, 12*time.Hour)

	unauthorized := performAdminRequest(t, router, http.MethodGet, "/admin/accounts", "", nil)
	assert.Equal(t, http.StatusUnauthorized, unauthorized.Code)

	body := map[string]any{
		"label":          "social-account",
		"auth_method":    "social",
		"refresh_token":  "refresh-secret",
		"profile_arn":    "arn:aws:iam::123456789012:role/Kiro",
		"region":         "us-west-2",
		"proxy_url":      "http://proxy.internal",
		"proxy_username": "proxy-user",
		"proxy_password": "proxy-pass",
		"enabled":        true,
	}
	createdResp := performAdminRequest(t, router, http.MethodPost, "/admin/accounts", adminAPIKey, body)
	require.Equal(t, http.StatusCreated, createdResp.Code)

	var created accountResponse
	decodeJSONResponse(t, createdResp, &created)
	assert.Equal(t, "social-account", created.Label)
	assert.Equal(t, "social", created.AuthMethod)
	assert.Nil(t, created.RefreshToken)
	assert.Nil(t, created.APIKey)
	assert.Nil(t, created.ProxyPassword)
	require.NotNil(t, created.AccessToken)
	assert.NotEmpty(t, *created.AccessToken)
	assert.True(t, created.Enabled)
	require.NoError(t, kiro.Validate(created.MachineID))
	assert.Len(t, manager.refreshCalls, 1)

	listResp := performAdminRequest(t, router, http.MethodGet, "/admin/accounts?enabled_only=true&auth_method=social", adminAPIKey, nil)
	require.Equal(t, http.StatusOK, listResp.Code)
	var listed []accountResponse
	decodeJSONResponse(t, listResp, &listed)
	require.Len(t, listed, 1)
	assert.Equal(t, created.ID, listed[0].ID)
	assert.Nil(t, listed[0].RefreshToken)
	assert.Nil(t, listed[0].ProxyPassword)

	circuit.RecordFailure(created.ID, "quota exhausted")
	getResp := performAdminRequest(t, router, http.MethodGet, "/admin/accounts/"+created.ID, adminAPIKey, nil)
	require.Equal(t, http.StatusOK, getResp.Code)
	var detail getAccountResponse
	decodeJSONResponse(t, getResp, &detail)
	assert.Equal(t, created.ID, detail.Account.ID)
	assert.True(t, detail.CircuitBreaker.Open)
	assert.Equal(t, "open", detail.CircuitBreaker.State)
	assert.Equal(t, 1, detail.CircuitBreaker.Failures)
	assert.Equal(t, "quota exhausted", detail.CircuitBreaker.LastReason)
	assert.Nil(t, detail.Account.RefreshToken)
	assert.Nil(t, detail.Account.ProxyPassword)
}

func TestCreateAccountValidationAndRefreshFailureDisablesAccount(t *testing.T) {
	store, cleanup := setupAdminTestStore(t)
	defer cleanup()

	manager := &fakeManager{store: store, refreshErr: errors.New("upstream refresh failed")}
	router := newAdminTestRouter(store, manager, &fakeQuotaFetcher{}, account.NewCircuitBreaker(account.CircuitConfig{BaseCooldown: time.Minute, MaxBackoffMultiplier: 8}, nil), 12*time.Hour)

	invalid := performAdminRequest(t, router, http.MethodPost, "/admin/accounts", adminAPIKey, map[string]any{
		"label":       "bad-key",
		"auth_method": "apikey",
		"api_key":     "not-a-kiro-key",
	})
	require.Equal(t, http.StatusBadRequest, invalid.Code)
	assert.Contains(t, invalid.Body.String(), "api_key must start with ksk_")

	created := performAdminRequest(t, router, http.MethodPost, "/admin/accounts", adminAPIKey, map[string]any{
		"label":         "refresh-failure",
		"auth_method":   "social",
		"refresh_token": "refresh-secret",
		"enabled":       true,
	})
	require.Equal(t, http.StatusCreated, created.Code)

	var resp accountResponse
	decodeJSONResponse(t, created, &resp)
	assert.False(t, resp.Enabled)
	require.NotNil(t, resp.DisabledReason)
	assert.Contains(t, *resp.DisabledReason, "upstream refresh failed")
	assert.Nil(t, resp.RefreshToken)

	stored, err := store.Get(context.Background(), resp.ID)
	require.NoError(t, err)
	assert.False(t, stored.Enabled)
	require.NotNil(t, stored.DisabledReason)
	assert.Contains(t, *stored.DisabledReason, "upstream refresh failed")
}

func TestPatchRefreshAndDeleteAccount(t *testing.T) {
	store, cleanup := setupAdminTestStore(t)
	defer cleanup()

	seed := &account.Account{
		ID:           "acc-1",
		Label:        "seed",
		AuthMethod:   "oauth",
		RefreshToken: stringPtr("refresh-1"),
		Region:       "us-east-1",
		MachineID:    kiro.Generate("seed"),
		Enabled:      true,
	}
	require.NoError(t, store.Create(context.Background(), seed))

	manager := &fakeManager{store: store}
	router := newAdminTestRouter(store, manager, &fakeQuotaFetcher{}, account.NewCircuitBreaker(account.CircuitConfig{BaseCooldown: time.Minute, MaxBackoffMultiplier: 8}, nil), 12*time.Hour)

	forbidden := performAdminRequest(t, router, http.MethodPatch, "/admin/accounts/acc-1", adminAPIKey, map[string]any{
		"auth_method": "apikey",
	})
	require.Equal(t, http.StatusBadRequest, forbidden.Code)
	assert.Contains(t, forbidden.Body.String(), "field auth_method is not allowed")

	patched := performAdminRequest(t, router, http.MethodPatch, "/admin/accounts/acc-1", adminAPIKey, map[string]any{
		"label":          "updated-label",
		"enabled":        false,
		"proxy_url":      "http://proxy.updated",
		"proxy_username": "updated-user",
		"proxy_password": "updated-pass",
		"region":         "eu-west-1",
		"auth_region":    "us-east-2",
		"api_region":     "eu-west-2",
	})
	require.Equal(t, http.StatusOK, patched.Code)
	var patchedResp accountResponse
	decodeJSONResponse(t, patched, &patchedResp)
	assert.Equal(t, "updated-label", patchedResp.Label)
	assert.False(t, patchedResp.Enabled)
	assert.Equal(t, "social", patchedResp.AuthMethod)
	assert.Nil(t, patchedResp.ProxyPassword)

	stored, err := store.Get(context.Background(), "acc-1")
	require.NoError(t, err)
	assert.Equal(t, "updated-label", stored.Label)
	assert.False(t, stored.Enabled)
	require.NotNil(t, stored.ProxyPassword)
	assert.Equal(t, "updated-pass", *stored.ProxyPassword)
	assert.Equal(t, "eu-west-1", stored.Region)

	refreshed := performAdminRequest(t, router, http.MethodPost, "/admin/accounts/acc-1/refresh", adminAPIKey, nil)
	require.Equal(t, http.StatusOK, refreshed.Code)
	var refreshedResp accountResponse
	decodeJSONResponse(t, refreshed, &refreshedResp)
	require.NotNil(t, refreshedResp.AccessToken)
	assert.Equal(t, "access-acc-1", *refreshedResp.AccessToken)
	assert.Nil(t, refreshedResp.RefreshToken)
	assert.Len(t, manager.refreshCalls, 1)

	deleted := performAdminRequest(t, router, http.MethodDelete, "/admin/accounts/acc-1", adminAPIKey, nil)
	require.Equal(t, http.StatusNoContent, deleted.Code)
	_, err = store.Get(context.Background(), "acc-1")
	assert.ErrorIs(t, err, account.ErrNotFound)
}

func TestQuotaEndpointsRespectForceAndSummaryTTL(t *testing.T) {
	store, cleanup := setupAdminTestStore(t)
	defer cleanup()

	acc1 := &account.Account{ID: "acc-1", Label: "first", AuthMethod: "api_key", APIKey: stringPtr("ksk_first"), Region: "us-east-1", MachineID: kiro.Generate("first"), Enabled: true}
	acc2 := &account.Account{ID: "acc-2", Label: "second", AuthMethod: "api_key", APIKey: stringPtr("ksk_second"), Region: "us-east-1", MachineID: kiro.Generate("second"), Enabled: true}
	require.NoError(t, store.Create(context.Background(), acc1))
	require.NoError(t, store.Create(context.Background(), acc2))

	freshFetchedAt := time.Now().UTC().Add(-30 * time.Minute).Truncate(time.Second)
	staleFetchedAt := time.Now().UTC().Add(-13 * time.Hour).Truncate(time.Second)
	quota := &fakeQuotaFetcher{
		getFn: func(ctx context.Context, acc *account.Account, force bool) (*account.Quota, error) {
			return &account.Quota{
				SubscriptionTitle: "Kiro Pro",
				LimitTotal:        100,
				LimitRemaining:    80,
				ResetTime:         time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
				Raw:               json.RawMessage(`{"subscriptionTitle":"Kiro Pro"}`),
				FetchedAt:         freshFetchedAt,
			}, nil
		},
		summaryFn: func(ctx context.Context) ([]*account.QuotaSummaryItem, error) {
			return []*account.QuotaSummaryItem{
				{AccountID: acc1.ID, Label: acc1.Label, Quota: &account.Quota{SubscriptionTitle: "Kiro Pro", LimitTotal: 100, LimitRemaining: 80, FetchedAt: freshFetchedAt}},
				{AccountID: acc2.ID, Label: acc2.Label, Quota: &account.Quota{SubscriptionTitle: "Kiro Free", LimitTotal: 50, LimitRemaining: 5, FetchedAt: staleFetchedAt}},
			}, nil
		},
	}
	router := newAdminTestRouter(store, &fakeManager{store: store}, quota, account.NewCircuitBreaker(account.CircuitConfig{BaseCooldown: time.Minute, MaxBackoffMultiplier: 8}, nil), 12*time.Hour)

	quotaResp := performAdminRequest(t, router, http.MethodGet, "/admin/accounts/acc-1/quota?force=true", adminAPIKey, nil)
	require.Equal(t, http.StatusOK, quotaResp.Code)
	var single quotaResponse
	decodeJSONResponse(t, quotaResp, &single)
	assert.Equal(t, "Kiro Pro", single.SubscriptionTitle)
	assert.Equal(t, int64(100), single.LimitTotal)
	assert.Equal(t, int64(80), single.LimitRemaining)
	require.Len(t, quota.getCalls, 1)
	assert.True(t, quota.getCalls[0].force)

	summaryResp := performAdminRequest(t, router, http.MethodGet, "/admin/quota", adminAPIKey, nil)
	require.Equal(t, http.StatusOK, summaryResp.Code)
	var summary []quotaSummaryResponse
	decodeJSONResponse(t, summaryResp, &summary)
	require.Len(t, summary, 2)
	assert.Equal(t, "acc-1", summary[0].AccountID)
	assert.False(t, summary[0].Stale)
	require.NotNil(t, summary[0].SubscriptionTitle)
	assert.Equal(t, "Kiro Pro", *summary[0].SubscriptionTitle)
	assert.Equal(t, "acc-2", summary[1].AccountID)
	assert.True(t, summary[1].Stale)
	require.NotNil(t, summary[1].SubscriptionTitle)
	assert.Equal(t, "Kiro Free", *summary[1].SubscriptionTitle)
	assert.Equal(t, 1, quota.summaryCalls)
	require.Len(t, quota.getCalls, 1, "summary should not trigger upstream quota refresh")
}

func setupAdminTestStore(t *testing.T) (*account.Store, func()) {
	t.Helper()
	db, err := account.OpenDB(":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	require.NoError(t, account.Apply(context.Background(), db))
	store, err := account.NewStore(db)
	require.NoError(t, err)
	return store, func() {
		require.NoError(t, store.Close())
		require.NoError(t, db.Close())
	}
}

func newAdminTestRouter(store *account.Store, manager refreshManager, quota quotaFetcher, circuit circuitSnapshotter, quotaTTL time.Duration) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterRoutes(router, adminAPIKey, NewHandler(store, manager, quota, circuit, quotaTTL))
	return router
}

func performAdminRequest(t *testing.T, router http.Handler, method, target, apiKey string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		require.NoError(t, err)
	}
	req := httptest.NewRequest(method, target, bytes.NewReader(payload))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	return resp
}

func decodeJSONResponse(t *testing.T, resp *httptest.ResponseRecorder, out any) {
	t.Helper()
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), out), fmt.Sprintf("response body: %s", resp.Body.String()))
}
