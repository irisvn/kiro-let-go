package account

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/irisvn/kiro-let-go/internal/config"
)

var ErrNoCandidates = fmt.Errorf("no available candidates")

// NoAccountsAvailable is a legacy alias for ErrNoCandidates.
var NoAccountsAvailable = ErrNoCandidates

type Balancer interface {
	Pick(ctx context.Context, candidates []*Account) (*Account, error)
}

// RoundRobin selects candidates in fixed order. The index is advanced externally
// via the Advance method so that rotation only happens on successful use.
type RoundRobin struct {
	idx atomic.Uint64
}

func (r *RoundRobin) Pick(_ context.Context, candidates []*Account) (*Account, error) {
	if len(candidates) == 0 {
		return nil, ErrNoCandidates
	}

	filtered := filterEnabled(candidates)
	if len(filtered) == 0 {
		return nil, ErrNoCandidates
	}

	i := int(r.idx.Load()) % len(filtered)
	return filtered[i], nil
}

func (r *RoundRobin) Advance() {
	r.idx.Add(1)
}

// Balanced selects the account with the lowest success_count,
// breaking ties by the oldest last_used_at.
type Balanced struct{}

func (b *Balanced) Pick(_ context.Context, candidates []*Account) (*Account, error) {
	if len(candidates) == 0 {
		return nil, ErrNoCandidates
	}

	filtered := filterEnabled(candidates)
	if len(filtered) == 0 {
		return nil, ErrNoCandidates
	}

	var best *Account
	for _, acc := range filtered {
		if best == nil {
			best = acc
			continue
		}
		if acc.SuccessCount < best.SuccessCount {
			best = acc
			continue
		}
		if acc.SuccessCount == best.SuccessCount {
			best = olderLastUsed(acc, best)
		}
	}
	return best, nil
}

// MostQuota selects the account with the highest cached LimitRemaining.
// If cache is missing or stale, the account is treated as 0 and refreshed
// opportunistically in the background without blocking Pick.
type MostQuota struct {
	fetcher  *Fetcher
	inFlight sync.Map
}

// DynamicBalancer reads the configured strategy on each pick.
type DynamicBalancer struct {
	dc        *config.DynamicConfig
	round     *RoundRobin
	balanced  *Balanced
	mostQuota *MostQuota
}

func (m *MostQuota) Pick(ctx context.Context, candidates []*Account) (*Account, error) {
	if len(candidates) == 0 {
		return nil, ErrNoCandidates
	}

	filtered := filterEnabled(candidates)
	if len(filtered) == 0 {
		return nil, ErrNoCandidates
	}

	var best *Account
	var bestQuota int64

	for _, acc := range filtered {
		remaining := m.remainingQuota(ctx, acc)

		if best == nil || remaining > bestQuota {
			best = acc
			bestQuota = remaining
			continue
		}
		if remaining == bestQuota {
			if acc.SuccessCount < best.SuccessCount {
				best = acc
				bestQuota = remaining
				continue
			}
			if acc.SuccessCount == best.SuccessCount {
				best = olderLastUsed(acc, best)
			}
		}
	}

	return best, nil
}

func (m *MostQuota) remainingQuota(ctx context.Context, acc *Account) int64 {
	if m.fetcher == nil || m.fetcher.store == nil {
		return 0
	}

	quota, err := m.fetcher.cached(ctx, acc.ID)
	if err == nil && m.fetcher.isFresh(quota) {
		return quota.LimitRemaining
	}

	m.triggerRefresh(acc)
	if quota != nil {
		return quota.LimitRemaining
	}
	return 0
}

func (m *MostQuota) triggerRefresh(acc *Account) {
	if m.fetcher == nil {
		return
	}

	_, loaded := m.inFlight.LoadOrStore(acc.ID, true)
	if loaded {
		return
	}

	go func() {
		defer m.inFlight.Delete(acc.ID)

		refreshCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		q, err := m.fetcher.fetch(refreshCtx, acc)
		if err != nil {
			return
		}
		_ = m.fetcher.store.UpsertQuota(refreshCtx, &QuotaCache{
			AccountID:   acc.ID,
			PayloadJSON: string(q.Raw),
			FetchedAt:   q.FetchedAt,
		})
	}()
}

func NewBalancer(strategy string, fetcher *Fetcher) (Balancer, error) {
	switch strategy {
	case "round_robin":
		return &RoundRobin{}, nil
	case "balanced":
		return &Balanced{}, nil
	case "most_quota":
		return &MostQuota{fetcher: fetcher}, nil
	default:
		return nil, fmt.Errorf("unknown balancer strategy: %s", strategy)
	}
}

func NewDynamicBalancer(dc *config.DynamicConfig, fetcher *Fetcher) Balancer {
	return &DynamicBalancer{dc: dc, round: &RoundRobin{}, balanced: &Balanced{}, mostQuota: &MostQuota{fetcher: fetcher}}
}

func (d *DynamicBalancer) Pick(ctx context.Context, candidates []*Account) (*Account, error) {
	strategy := "round_robin"
	if d != nil && d.dc != nil {
		strategy = d.dc.Get().Strategy
	}
	switch strategy {
	case "balanced":
		return d.balanced.Pick(ctx, candidates)
	case "most_quota":
		return d.mostQuota.Pick(ctx, candidates)
	case "round_robin", "":
		return d.round.Pick(ctx, candidates)
	default:
		return nil, fmt.Errorf("unknown balancer strategy: %s", strategy)
	}
}

func (d *DynamicBalancer) Advance() {
	if d != nil && d.round != nil {
		d.round.Advance()
	}
}

func filterEnabled(candidates []*Account) []*Account {
	filtered := make([]*Account, 0, len(candidates))
	for _, acc := range candidates {
		if acc.Enabled {
			filtered = append(filtered, acc)
		}
	}
	return filtered
}

func olderLastUsed(a, b *Account) *Account {
	var aTime, bTime time.Time
	if a.LastUsedAt != nil {
		aTime = *a.LastUsedAt
	}
	if b.LastUsedAt != nil {
		bTime = *b.LastUsedAt
	}
	if aTime.Before(bTime) || aTime.Equal(bTime) {
		return a
	}
	return b
}
