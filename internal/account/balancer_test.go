package account

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ptr(t time.Time) *time.Time { return &t }

func TestNewBalancer(t *testing.T) {
	b, err := NewBalancer("round_robin", nil)
	require.NoError(t, err)
	require.IsType(t, &RoundRobin{}, b)

	b, err = NewBalancer("balanced", nil)
	require.NoError(t, err)
	require.IsType(t, &Balanced{}, b)

	b, err = NewBalancer("most_quota", nil)
	require.NoError(t, err)
	require.IsType(t, &MostQuota{}, b)

	_, err = NewBalancer("unknown", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown")
}

func TestRoundRobinPick(t *testing.T) {
	ctx := context.Background()
	rr := &RoundRobin{}

	acc1 := &Account{ID: "a1", Enabled: true}
	acc2 := &Account{ID: "a2", Enabled: true}
	acc3 := &Account{ID: "a3", Enabled: true}
	candidates := []*Account{acc1, acc2, acc3}

	got, err := rr.Pick(ctx, candidates)
	require.NoError(t, err)
	assert.Equal(t, "a1", got.ID)

	got, err = rr.Pick(ctx, candidates)
	require.NoError(t, err)
	assert.Equal(t, "a1", got.ID)

	rr.Advance()
	got, err = rr.Pick(ctx, candidates)
	require.NoError(t, err)
	assert.Equal(t, "a2", got.ID)

	rr.Advance()
	got, err = rr.Pick(ctx, candidates)
	require.NoError(t, err)
	assert.Equal(t, "a3", got.ID)

	rr.Advance()
	got, err = rr.Pick(ctx, candidates)
	require.NoError(t, err)
	assert.Equal(t, "a1", got.ID)
}

func TestRoundRobinSkipsDisabled(t *testing.T) {
	ctx := context.Background()
	rr := &RoundRobin{}

	acc1 := &Account{ID: "a1", Enabled: false}
	acc2 := &Account{ID: "a2", Enabled: true}
	candidates := []*Account{acc1, acc2}

	got, err := rr.Pick(ctx, candidates)
	require.NoError(t, err)
	assert.Equal(t, "a2", got.ID)
}

func TestRoundRobinEmpty(t *testing.T) {
	ctx := context.Background()
	rr := &RoundRobin{}

	_, err := rr.Pick(ctx, nil)
	assert.ErrorIs(t, err, ErrNoCandidates)

	_, err = rr.Pick(ctx, []*Account{})
	assert.ErrorIs(t, err, ErrNoCandidates)
}

func TestRoundRobinAllDisabled(t *testing.T) {
	ctx := context.Background()
	rr := &RoundRobin{}

	candidates := []*Account{
		{ID: "a1", Enabled: false},
		{ID: "a2", Enabled: false},
	}

	_, err := rr.Pick(ctx, candidates)
	assert.ErrorIs(t, err, ErrNoCandidates)
}

func TestBalancedLowestSuccessCount(t *testing.T) {
	ctx := context.Background()
	b := &Balanced{}

	acc1 := &Account{ID: "a1", Enabled: true, SuccessCount: 10}
	acc2 := &Account{ID: "a2", Enabled: true, SuccessCount: 5}
	acc3 := &Account{ID: "a3", Enabled: true, SuccessCount: 20}
	candidates := []*Account{acc1, acc2, acc3}

	got, err := b.Pick(ctx, candidates)
	require.NoError(t, err)
	assert.Equal(t, "a2", got.ID)
}

func TestBalancedTieBreakOldestLastUsed(t *testing.T) {
	ctx := context.Background()
	b := &Balanced{}

	now := time.Now().UTC()
	acc1 := &Account{ID: "a1", Enabled: true, SuccessCount: 5, LastUsedAt: ptr(now.Add(-2 * time.Hour))}
	acc2 := &Account{ID: "a2", Enabled: true, SuccessCount: 5, LastUsedAt: ptr(now.Add(-1 * time.Hour))}
	acc3 := &Account{ID: "a3", Enabled: true, SuccessCount: 5, LastUsedAt: nil}
	candidates := []*Account{acc1, acc2, acc3}

	got, err := b.Pick(ctx, candidates)
	require.NoError(t, err)
	assert.Equal(t, "a3", got.ID)
}

func TestBalancedSkipsDisabled(t *testing.T) {
	ctx := context.Background()
	b := &Balanced{}

	acc1 := &Account{ID: "a1", Enabled: false, SuccessCount: 0}
	acc2 := &Account{ID: "a2", Enabled: true, SuccessCount: 10}
	candidates := []*Account{acc1, acc2}

	got, err := b.Pick(ctx, candidates)
	require.NoError(t, err)
	assert.Equal(t, "a2", got.ID)
}

func TestBalancedEmpty(t *testing.T) {
	ctx := context.Background()
	b := &Balanced{}

	_, err := b.Pick(ctx, nil)
	assert.ErrorIs(t, err, ErrNoCandidates)
}

func TestMostQuotaNilFetcher(t *testing.T) {
	ctx := context.Background()
	mq := &MostQuota{fetcher: nil}

	acc1 := &Account{ID: "a1", Enabled: true, SuccessCount: 0}
	acc2 := &Account{ID: "a2", Enabled: true, SuccessCount: 0}
	candidates := []*Account{acc1, acc2}

	got, err := mq.Pick(ctx, candidates)
	require.NoError(t, err)
	assert.Equal(t, "a2", got.ID)
}

func TestMostQuotaWithCache(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	acc1 := newTestAccount()
	acc1.Label = "acc1"
	require.NoError(t, store.Create(ctx, acc1))
	require.NoError(t, store.UpsertQuota(ctx, &QuotaCache{
		AccountID:   acc1.ID,
		PayloadJSON: `{"monthlyRequestRemaining":100}`,
		FetchedAt:   time.Now().UTC(),
	}))

	acc2 := newTestAccount()
	acc2.Label = "acc2"
	require.NoError(t, store.Create(ctx, acc2))
	require.NoError(t, store.UpsertQuota(ctx, &QuotaCache{
		AccountID:   acc2.ID,
		PayloadJSON: `{"monthlyRequestRemaining":50}`,
		FetchedAt:   time.Now().UTC(),
	}))

	fetcher := NewFetcher(nil, store, time.Hour, nil)
	mq := &MostQuota{fetcher: fetcher}

	candidates := []*Account{acc1, acc2}
	got, err := mq.Pick(ctx, candidates)
	require.NoError(t, err)
	assert.Equal(t, acc1.ID, got.ID)
}

func TestMostQuotaTieBreakSuccessCount(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	acc1 := newTestAccount()
	acc1.Label = "acc1"
	acc1.SuccessCount = 5
	require.NoError(t, store.Create(ctx, acc1))
	require.NoError(t, store.UpsertQuota(ctx, &QuotaCache{
		AccountID:   acc1.ID,
		PayloadJSON: `{"monthlyRequestRemaining":100}`,
		FetchedAt:   time.Now().UTC(),
	}))

	acc2 := newTestAccount()
	acc2.Label = "acc2"
	acc2.SuccessCount = 2
	require.NoError(t, store.Create(ctx, acc2))
	require.NoError(t, store.UpsertQuota(ctx, &QuotaCache{
		AccountID:   acc2.ID,
		PayloadJSON: `{"monthlyRequestRemaining":100}`,
		FetchedAt:   time.Now().UTC(),
	}))

	fetcher := NewFetcher(nil, store, time.Hour, nil)
	mq := &MostQuota{fetcher: fetcher}

	candidates := []*Account{acc1, acc2}
	got, err := mq.Pick(ctx, candidates)
	require.NoError(t, err)
	assert.Equal(t, acc2.ID, got.ID)
}

func TestMostQuotaEmptyCandidates(t *testing.T) {
	ctx := context.Background()
	mq := &MostQuota{fetcher: nil}

	_, err := mq.Pick(ctx, nil)
	assert.ErrorIs(t, err, ErrNoCandidates)
}

func TestMostQuotaSkipsDisabled(t *testing.T) {
	ctx := context.Background()
	mq := &MostQuota{fetcher: nil}

	acc1 := &Account{ID: "a1", Enabled: false}
	acc2 := &Account{ID: "a2", Enabled: true}
	candidates := []*Account{acc1, acc2}

	got, err := mq.Pick(ctx, candidates)
	require.NoError(t, err)
	assert.Equal(t, "a2", got.ID)
}

func TestFilterEnabled(t *testing.T) {
	all := []*Account{
		{ID: "a1", Enabled: true},
		{ID: "a2", Enabled: false},
		{ID: "a3", Enabled: true},
	}
	got := filterEnabled(all)
	require.Len(t, got, 2)
	assert.Equal(t, "a1", got[0].ID)
	assert.Equal(t, "a3", got[1].ID)
}

func TestOlderLastUsed(t *testing.T) {
	now := time.Now().UTC()
	a := &Account{ID: "a", LastUsedAt: ptr(now.Add(-2 * time.Hour))}
	b := &Account{ID: "b", LastUsedAt: ptr(now.Add(-1 * time.Hour))}
	assert.Equal(t, "a", olderLastUsed(a, b).ID)
	assert.Equal(t, "a", olderLastUsed(b, a).ID)

	c := &Account{ID: "c", LastUsedAt: nil}
	assert.Equal(t, "c", olderLastUsed(c, a).ID)
	assert.Equal(t, "c", olderLastUsed(a, c).ID)

	d := &Account{ID: "d", LastUsedAt: ptr(now)}
	e := &Account{ID: "e", LastUsedAt: ptr(now)}
	assert.Equal(t, "d", olderLastUsed(d, e).ID)
}
