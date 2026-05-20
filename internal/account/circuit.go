package account

import (
	"math/rand"
	"sync"
	"time"
)

type clockFn func() time.Time

type CircuitConfig struct {
	BaseCooldown             time.Duration
	MaxBackoffMultiplier     int
	ProbabilisticRetryChance float64
}

type CircuitInfo struct {
	AccountID    string
	Failures     int
	LastFailure  time.Time
	LastReason   string
	Open         bool
	CooldownEnds time.Time
}

type state struct {
	failures    int
	lastFailure time.Time
	lastReason  string
}

type CircuitBreaker struct {
	mu     sync.RWMutex
	states map[string]*state
	cfg    CircuitConfig
	clock  clockFn
}

func NewCircuitBreaker(cfg CircuitConfig, clock clockFn) *CircuitBreaker {
	if clock == nil {
		clock = time.Now
	}
	return &CircuitBreaker{
		states: make(map[string]*state),
		cfg:    cfg,
		clock:  clock,
	}
}

func (cb *CircuitBreaker) Seed(accountID string, failureCount int) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.states[accountID] = &state{
		failures:    failureCount,
		lastFailure: cb.clock(),
	}
}

func (cb *CircuitBreaker) IsOpen(accountID string) bool {
	cb.mu.RLock()
	st, exists := cb.states[accountID]
	var failures int
	var lastFailure time.Time
	if exists {
		failures = st.failures
		lastFailure = st.lastFailure
	}
	cb.mu.RUnlock()

	if !exists || failures < 3 {
		return false
	}

	cooldown := cb.calculateCooldown(failures)
	if cb.clock().Sub(lastFailure) >= cooldown {
		return false
	}

	if cb.ShouldRetry() {
		return false
	}

	return true
}

func (cb *CircuitBreaker) RecordSuccess(accountID string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	st, exists := cb.states[accountID]
	if !exists {
		cb.states[accountID] = &state{failures: 0}
		return
	}
	st.failures = 0
	st.lastReason = ""
}

func (cb *CircuitBreaker) Reset(accountID string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	delete(cb.states, accountID)
}

func (cb *CircuitBreaker) RecordFailure(accountID string, reason string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	st, exists := cb.states[accountID]
	if !exists {
		cb.states[accountID] = &state{
			failures:    1,
			lastFailure: cb.clock(),
			lastReason:  reason,
		}
		return
	}
	st.failures++
	st.lastFailure = cb.clock()
	st.lastReason = reason
}

func (cb *CircuitBreaker) Reason(accountID string) string {
	if !cb.IsOpen(accountID) {
		return ""
	}
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.states[accountID].lastReason
}

func (cb *CircuitBreaker) Snapshot() map[string]CircuitInfo {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	snap := make(map[string]CircuitInfo, len(cb.states))
	now := cb.clock()
	for id, st := range cb.states {
		cooldown := cb.calculateCooldown(st.failures)
		open := st.failures >= 3 && now.Sub(st.lastFailure) < cooldown
		snap[id] = CircuitInfo{
			AccountID:    id,
			Failures:     st.failures,
			LastFailure:  st.lastFailure,
			LastReason:   st.lastReason,
			Open:         open,
			CooldownEnds: st.lastFailure.Add(cooldown),
		}
	}
	return snap
}

func (cb *CircuitBreaker) calculateCooldown(failures int) time.Duration {
	if failures < 3 {
		return 0
	}
	multiplier := 1 << (failures - 3)
	multiplier = min(multiplier, cb.cfg.MaxBackoffMultiplier)
	return cb.cfg.BaseCooldown * time.Duration(multiplier)
}

// ShouldRetry reports whether a circuit-open account should be retried.
func (cb *CircuitBreaker) ShouldRetry() bool {
	if cb.cfg.ProbabilisticRetryChance <= 0 {
		return false
	}
	if cb.cfg.ProbabilisticRetryChance >= 1 {
		return true
	}
	return rand.Float64() < cb.cfg.ProbabilisticRetryChance
}
