package kiro

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/irisvn/kiro-let-go/internal/account"
	"github.com/irisvn/kiro-let-go/internal/errs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type rewriteTransport struct{ target string }

func (rt rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rewritten := req.Clone(req.Context())
	rewritten.URL.Scheme = "http"
	rewritten.URL.Host = strings.TrimPrefix(rt.target, "http://")
	rewritten.Host = rewritten.URL.Host
	return http.DefaultTransport.RoundTrip(rewritten)
}

type fakeSocialRefresh struct{ calls atomic.Int32 }

func (f *fakeSocialRefresh) Refresh(ctx context.Context, acc *account.Account) (string, string, time.Time, error) {
	_ = ctx
	n := f.calls.Add(1)
	return "refreshed-token", "refresh-token", time.Now().UTC().Add(time.Hour + time.Duration(n)), nil
}

func TestDispatcherFailoverThirdAccountSucceeds(t *testing.T) {
	ctx := context.Background()
	store, cleanup := dispatcherTestStore(t)
	defer cleanup()
	createDispatcherAccount(t, store, "acc-1", "token-1")
	createDispatcherAccount(t, store, "acc-2", "token-2")
	createDispatcherAccount(t, store, "acc-3", "token-3")

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/generateAssistantResponse", r.URL.Path)
		require.Equal(t, http.MethodPost, r.Method)
		n := requests.Add(1)
		if n < 3 {
			http.Error(w, "MONTHLY_REQUEST_COUNT exceeded", http.StatusPaymentRequired)
			return
		}
		assert.Equal(t, "Bearer token-3", r.Header.Get("Authorization"))
		_, _ = w.Write(dispatcherStreamBytes(t, "winner"))
	}))
	defer server.Close()

	d := newDispatcherForTest(store, server.URL, DispatcherConfig{MaxAttempts: 3, BaseRetryMs: 1}, nil)
	events, _, err := d.Stream(ctx, dispatcherPayload(), account.SelectionHint{})
	require.NoError(t, err)
	text := collectText(t, events)
	assert.Equal(t, "winner", text)
	assert.Equal(t, int32(3), requests.Load())

	acc1, err := store.Get(ctx, "acc-1")
	require.NoError(t, err)
	acc2, err := store.Get(ctx, "acc-2")
	require.NoError(t, err)
	acc3, err := store.Get(ctx, "acc-3")
	require.NoError(t, err)
	assert.Equal(t, 1, acc1.FailureCount)
	assert.Equal(t, 1, acc2.FailureCount)
	assert.Equal(t, 1, acc3.SuccessCount)
}

func TestDispatcherRefreshesOnceOnAuthThenFailsOver(t *testing.T) {
	ctx := context.Background()
	store, cleanup := dispatcherTestStore(t)
	defer cleanup()
	createOAuthDispatcherAccount(t, store, "acc-1", "old-token")
	createDispatcherAccount(t, store, "acc-2", "token-2")
	fakeRefresh := &fakeSocialRefresh{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("Authorization") {
		case "Bearer old-token":
			http.Error(w, "expired", http.StatusUnauthorized)
		case "Bearer token-2":
			_, _ = w.Write(dispatcherStreamBytes(t, "fallback"))
		default:
			t.Fatalf("unexpected authorization header: %q", r.Header.Get("Authorization"))
		}
	}))
	defer server.Close()

	d := newDispatcherForTest(store, server.URL, DispatcherConfig{MaxAttempts: 2, BaseRetryMs: 1}, fakeRefresh)
	events, _, err := d.Stream(ctx, dispatcherPayload(), account.SelectionHint{})
	require.NoError(t, err)
	assert.Equal(t, "fallback", collectText(t, events))
	assert.Equal(t, int32(1), fakeRefresh.calls.Load())
}

func TestDispatcherOnceCollectsFullResponse(t *testing.T) {
	store, cleanup := dispatcherTestStore(t)
	defer cleanup()
	createDispatcherAccount(t, store, "acc-1", "token-1")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(dispatcherMixedStreamBytes(t))
	}))
	defer server.Close()

	d := newDispatcherForTest(store, server.URL, DispatcherConfig{MaxAttempts: 1, BaseRetryMs: 1}, nil)
	full, err := d.Once(context.Background(), dispatcherPayload(), account.SelectionHint{})
	require.NoError(t, err)
	assert.Equal(t, "hello world", full.Text)
	assert.Equal(t, "end_turn", full.StopReason)
	assert.Positive(t, full.Usage.InputTokens)
	assert.Positive(t, full.Usage.OutputTokens)
	require.Len(t, full.ToolUses, 1)
	assert.Equal(t, "tool-1", full.ToolUses[0].ToolUseID)
	assert.Equal(t, "shell", full.ToolUses[0].Name)
	assert.Equal(t, "x", full.ToolUses[0].Input)
}

func TestDispatcherAllAttemptsExhausted(t *testing.T) {
	store, cleanup := dispatcherTestStore(t)
	defer cleanup()
	createDispatcherAccount(t, store, "acc-1", "token-1")
	createDispatcherAccount(t, store, "acc-2", "token-2")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "MONTHLY_REQUEST_COUNT exceeded", http.StatusPaymentRequired)
	}))
	defer server.Close()

	d := newDispatcherForTest(store, server.URL, DispatcherConfig{MaxAttempts: 2, BaseRetryMs: 1}, nil)
	_, _, err := d.Stream(context.Background(), dispatcherPayload(), account.SelectionHint{})
	require.Error(t, err)
	var classified *errs.Error
	require.ErrorAs(t, err, &classified)
	assert.Equal(t, errs.ClassFatal, classified.Class)
	assert.Equal(t, "ALL_ACCOUNTS_FAILED", classified.Code)
}

func newDispatcherForTest(store *account.Store, target string, cfg DispatcherConfig, social account.SocialRefresher) *Dispatcher {
	client := NewClient(0, nil)
	client.baseClient.Transport = rewriteTransport{target: target}
	managerOpts := []account.ManagerOption{}
	if social != nil {
		managerOpts = append(managerOpts, account.WithSocialAuth(social))
	}
	mgr := account.NewManager(store, &account.RoundRobin{}, account.NewCircuitBreaker(account.CircuitConfig{BaseCooldown: time.Millisecond, MaxBackoffMultiplier: 1}, nil), account.ManagerConfig{DefaultRegion: "us-west-2"}, nil, managerOpts...)
	return NewDispatcher(client, mgr, cfg, nil)
}

func dispatcherTestStore(t *testing.T) (*account.Store, func()) {
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

func createDispatcherAccount(t *testing.T, store *account.Store, id, token string) {
	t.Helper()
	expires := time.Now().UTC().Add(time.Hour)
	acc := &account.Account{ID: id, Label: id, AuthMethod: "api_key", APIKey: &token, AccessToken: &token, ExpiresAt: &expires, Region: "us-west-2", MachineID: "machine-" + id, Enabled: true}
	require.NoError(t, store.Create(context.Background(), acc))
}

func createOAuthDispatcherAccount(t *testing.T, store *account.Store, id, token string) {
	t.Helper()
	refresh := "refresh-token"
	expires := time.Now().UTC().Add(time.Hour)
	acc := &account.Account{ID: id, Label: id, AuthMethod: "oauth", AccessToken: &token, RefreshToken: &refresh, ExpiresAt: &expires, Region: "us-west-2", MachineID: "machine-" + id, Enabled: true}
	require.NoError(t, store.Create(context.Background(), acc))
}

func dispatcherPayload() *KiroPayload {
	return &KiroPayload{ConversationState: ConversationState{ConversationID: "conv", CurrentMessage: CurrentMessage{UserInputMessage: UserInputMessage{Content: "hi", ModelID: ModelClaudeSonnet46}}}}
}

func dispatcherStreamBytes(t *testing.T, text string) []byte {
	t.Helper()
	return buildEventStreamFrame(t, map[string]string{":message-type": "event", ":event-type": "assistantResponseEvent"}, []byte(`{"content":"`+text+`"}`))
}

func dispatcherMixedStreamBytes(t *testing.T) []byte {
	t.Helper()
	out := dispatcherStreamBytes(t, "hello ")
	out = append(out, dispatcherStreamBytes(t, "world")...)
	out = append(out, buildEventStreamFrame(t, map[string]string{":message-type": "event", ":event-type": "toolUseEvent"}, []byte(`{"name":"shell","toolUseId":"tool-1"}`))...)
	out = append(out, buildEventStreamFrame(t, map[string]string{":message-type": "event", ":event-type": "toolUseEvent"}, []byte(`{"toolUseId":"tool-1","input":"x"}`))...)
	out = append(out, buildEventStreamFrame(t, map[string]string{":message-type": "event", ":event-type": "contextUsageEvent"}, []byte(`{"contextUsagePercentage":10}`))...)
	return out
}

func collectText(t *testing.T, events <-chan StreamEvent) string {
	t.Helper()
	var text string
	for event := range events {
		switch e := event.(type) {
		case TextDelta:
			text += e.Text
		case ErrorEvent:
			require.NoError(t, e.Err)
		}
	}
	return text
}
