package account

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type rewriteTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func (t rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = t.target.Scheme
	req.URL.Host = t.target.Host
	req.Host = t.target.Host
	return t.base.RoundTrip(req)
}

func TestQuotaFetcherCacheTTLAndForce(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	apiRegion := "us-east-1"
	acc := newTestAccount()
	acc.AuthMethod = "apikey"
	acc.APIRegion = &apiRegion
	require.NoError(t, store.Create(ctx, acc))

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/getUsageLimits", r.URL.Path)
		assert.Equal(t, "Bearer "+*acc.APIKey, r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"subscriptionTitle":"Kiro Pro",
			"usageBreakdownList":[{"currentUsage":25,"usageLimit":100,"overageCap":500,"overageRate":0.04,"currency":"USD","resourceType":"CREDIT","nextDateReset":1780272000.0}],
			"resetTime":"2026-06-01T00:00:00Z"
		}`))
	}))
	defer server.Close()
	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)

	client := &http.Client{Transport: rewriteTransport{target: serverURL, base: http.DefaultTransport}}
	fetcher := NewFetcher(client, store, 12*time.Hour, nil)

	for range 5 {
		quota, err := fetcher.Get(ctx, acc, false)
		require.NoError(t, err)
		assert.Equal(t, "Kiro Pro", quota.SubscriptionTitle)
		assert.Equal(t, int64(100), quota.LimitTotal)
		assert.Equal(t, int64(75), quota.LimitRemaining)
	}
	assert.Equal(t, int32(1), calls.Load())

	_, err = fetcher.Get(ctx, acc, true)
	require.NoError(t, err)
	assert.Equal(t, int32(2), calls.Load())
}

func TestQuotaFetcherStaleCacheRefreshes(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	apiRegion := "us-east-1"
	acc := newTestAccount()
	acc.APIRegion = &apiRegion
	require.NoError(t, store.Create(ctx, acc))
	require.NoError(t, store.UpsertQuota(ctx, &QuotaCache{
		AccountID:   acc.ID,
		PayloadJSON: `{"subscriptionTitle":"old","monthlyRequestCount":1,"monthlyRequestLimit":2}`,
		FetchedAt:   time.Now().UTC().Add(-2 * time.Hour),
	}))

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"subscriptionTitle":"new","monthlyRequestCount":4,"monthlyRequestLimit":10}`))
	}))
	defer server.Close()
	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)

	fetcher := NewFetcher(&http.Client{Transport: rewriteTransport{target: serverURL, base: http.DefaultTransport}}, store, time.Hour, nil)
	quota, err := fetcher.Get(ctx, acc, false)
	require.NoError(t, err)
	assert.Equal(t, "new", quota.SubscriptionTitle)
	assert.Equal(t, int32(1), calls.Load())
}

func TestQuotaSummaryDoesNotFetch(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	acc1 := newTestAccount()
	acc1.Label = "with-cache"
	require.NoError(t, store.Create(ctx, acc1))
	require.NoError(t, store.UpsertQuota(ctx, &QuotaCache{
		AccountID:   acc1.ID,
		PayloadJSON: `{"subscriptionTitle":"cached","monthlyRequestCount":3,"monthlyRequestLimit":8}`,
		FetchedAt:   time.Now().UTC(),
	}))

	acc2 := newTestAccount()
	acc2.Label = "without-cache"
	require.NoError(t, store.Create(ctx, acc2))

	var calls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, assert.AnError
	})}
	fetcher := NewFetcher(client, store, time.Hour, nil)

	items, err := fetcher.Summary(ctx)
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, int32(0), calls.Load())
	assert.NotNil(t, items[0].Quota)
	assert.Nil(t, items[1].Quota)
}

func TestQuotaFetcherStartsNoBackgroundGoroutine(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	before := runtime.NumGoroutine()
	_ = NewFetcher(http.DefaultClient, store, time.Hour, nil)
	time.Sleep(10 * time.Millisecond)
	after := runtime.NumGoroutine()
	assert.LessOrEqual(t, after, before+1)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
