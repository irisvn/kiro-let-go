package account

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeSocialAuth struct {
	calls atomic.Int32
	sleep time.Duration
}

func (f *fakeSocialAuth) Refresh(ctx context.Context, acc *Account) (string, string, time.Time, error) {
	if f.sleep > 0 {
		select {
		case <-time.After(f.sleep):
		case <-ctx.Done():
			return "", "", time.Time{}, ctx.Err()
		}
	}
	n := f.calls.Add(1)
	return fmt.Sprintf("access-%d", n), fmt.Sprintf("refresh-%d", n), time.Now().UTC().Add(time.Hour), nil
}

type sequenceBalancer struct {
	calls atomic.Int32
}

func (b *sequenceBalancer) Pick(ctx context.Context, candidates []*Account) (*Account, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	if len(candidates) == 0 {
		return nil, ErrNoCandidates
	}
	n := int(b.calls.Add(1)) - 1
	return candidates[n%len(candidates)], nil
}

func managerTestStore(t *testing.T) (*Store, func()) {
	t.Helper()
	store, cleanup := setupTestStore(t)
	store.db.SetMaxOpenConns(1)
	return store, cleanup
}

func TestManagerRefreshDCL(t *testing.T) {
	store, cleanup := managerTestStore(t)
	defer cleanup()
	ctx := context.Background()

	oldAccess := "old-access"
	oldRefresh := "old-refresh"
	expired := time.Now().UTC().Add(-time.Hour)
	acc := newTestAccount()
	acc.AuthMethod = "oauth"
	acc.APIKey = nil
	acc.AccessToken = &oldAccess
	acc.RefreshToken = &oldRefresh
	acc.ExpiresAt = &expired
	require.NoError(t, store.Create(ctx, acc))

	fakeAuth := &fakeSocialAuth{sleep: 5 * time.Millisecond}
	mgr := NewManager(store, &RoundRobin{}, newManagerTestCircuit(), ManagerConfig{DefaultRegion: "us-east-1"}, nil, WithSocialAuth(fakeAuth))

	const goroutines = 50
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)
	tokens := make(chan string, goroutines)
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			acq, err := mgr.Acquire(ctx, SelectionHint{})
			if err != nil {
				errs <- err
				return
			}
			tokens <- acq.Token
		}()
	}
	wg.Wait()
	close(errs)
	close(tokens)
	for err := range errs {
		require.NoError(t, err)
	}
	for token := range tokens {
		assert.Equal(t, "access-1", token)
	}
	assert.Equal(t, int32(1), fakeAuth.calls.Load())
}

func TestManagerStickySessionAfterSuccess(t *testing.T) {
	store, cleanup := managerTestStore(t)
	defer cleanup()
	ctx := context.Background()

	acc1 := newTestAccount()
	acc1.ID = "acc-1"
	acc1.Label = "first"
	acc1.AccessToken = managerStrPtr("token-1")
	require.NoError(t, store.Create(ctx, acc1))
	acc2 := newTestAccount()
	acc2.ID = "acc-2"
	acc2.Label = "second"
	acc2.AccessToken = managerStrPtr("token-2")
	require.NoError(t, store.Create(ctx, acc2))

	bal := &sequenceBalancer{}
	mgr := NewManager(store, bal, newManagerTestCircuit(), ManagerConfig{StickySession: true}, nil)
	first, err := mgr.Acquire(ctx, SelectionHint{})
	require.NoError(t, err)
	assert.Equal(t, "acc-1", first.Account.ID)
	first.ReleaseSuccess()

	second, err := mgr.Acquire(ctx, SelectionHint{})
	require.NoError(t, err)
	assert.Equal(t, "acc-1", second.Account.ID)
	assert.Equal(t, int32(1), bal.calls.Load(), "sticky path should bypass balancer")
}

func TestManagerExcludesAccounts(t *testing.T) {
	store, cleanup := managerTestStore(t)
	defer cleanup()
	ctx := context.Background()

	acc1 := newTestAccount()
	acc1.ID = "acc-1"
	acc1.AccessToken = managerStrPtr("token-1")
	require.NoError(t, store.Create(ctx, acc1))
	acc2 := newTestAccount()
	acc2.ID = "acc-2"
	acc2.AccessToken = managerStrPtr("token-2")
	require.NoError(t, store.Create(ctx, acc2))

	mgr := NewManager(store, &RoundRobin{}, newManagerTestCircuit(), ManagerConfig{}, nil)
	acq, err := mgr.Acquire(ctx, SelectionHint{ExcludeIDs: []string{"acc-1"}})
	require.NoError(t, err)
	assert.Equal(t, "acc-2", acq.Account.ID)
}

func TestManagerOpusSkipsFreeQuota(t *testing.T) {
	store, cleanup := managerTestStore(t)
	defer cleanup()
	ctx := context.Background()

	free := newTestAccount()
	free.ID = "free"
	free.AccessToken = managerStrPtr("free-token")
	require.NoError(t, store.Create(ctx, free))
	require.NoError(t, store.UpsertQuota(ctx, &QuotaCache{AccountID: free.ID, PayloadJSON: `{"subscriptionTitle":"Kiro Free"}`, FetchedAt: time.Now().UTC()}))
	pro := newTestAccount()
	pro.ID = "pro"
	pro.AccessToken = managerStrPtr("pro-token")
	require.NoError(t, store.Create(ctx, pro))
	require.NoError(t, store.UpsertQuota(ctx, &QuotaCache{AccountID: pro.ID, PayloadJSON: `{"subscriptionTitle":"Kiro Pro"}`, FetchedAt: time.Now().UTC()}))

	mgr := NewManager(store, &RoundRobin{}, newManagerTestCircuit(), ManagerConfig{}, nil)
	acq, err := mgr.Acquire(ctx, SelectionHint{Model: "claude-3-opus"})
	require.NoError(t, err)
	assert.Equal(t, "pro", acq.Account.ID)
}

func TestManagerReleaseClosuresRecordUsage(t *testing.T) {
	store, cleanup := managerTestStore(t)
	defer cleanup()
	ctx := context.Background()

	acc := newTestAccount()
	acc.ID = "acc-1"
	acc.AccessToken = managerStrPtr("token-1")
	require.NoError(t, store.Create(ctx, acc))
	circuit := newManagerTestCircuit()

	mgr := NewManager(store, &RoundRobin{}, circuit, ManagerConfig{StickySession: true}, nil)
	acq, err := mgr.Acquire(ctx, SelectionHint{})
	require.NoError(t, err)
	acq.ReleaseSuccess()
	got, err := store.Get(ctx, acc.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, got.SuccessCount)
	assert.False(t, circuit.IsOpen(acc.ID))

	acq.ReleaseFailure("boom")
	got, err = store.Get(ctx, acc.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, got.FailureCount)
}

func managerStrPtr(s string) *string { return &s }

func newManagerTestCircuit() *CircuitBreaker { return NewCircuitBreaker(defaultCircuitConfig(), nil) }
