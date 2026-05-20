package account

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestCircuitBreaker(now time.Time) *CircuitBreaker {
	return NewCircuitBreaker(CircuitConfig{
		BaseCooldown:             time.Minute,
		MaxBackoffMultiplier:     8,
		ProbabilisticRetryChance: 0.0,
	}, func() time.Time { return now })
}

func TestCircuitBreakerIsOpenNoFailures(t *testing.T) {
	now := time.Now()
	cb := newTestCircuitBreaker(now)
	assert.False(t, cb.IsOpen("acc1"))
}

func TestCircuitBreakerIsOpenAfterFailure(t *testing.T) {
	now := time.Now()
	cb := newTestCircuitBreaker(now)
	cb.RecordFailure("acc1", "timeout")
	assert.True(t, cb.IsOpen("acc1"))
}

func TestCircuitBreakerIsOpenCooldownExpires(t *testing.T) {
	now := time.Now()
	cb := newTestCircuitBreaker(now)
	cb.RecordFailure("acc1", "timeout")
	assert.True(t, cb.IsOpen("acc1"))

	now = now.Add(time.Minute)
	cb.clock = func() time.Time { return now }
	assert.False(t, cb.IsOpen("acc1"))
}

func TestCircuitBreakerExponentialBackoff(t *testing.T) {
	now := time.Now()
	cb := newTestCircuitBreaker(now)

	cb.RecordFailure("acc1", "err1")
	assert.True(t, cb.IsOpen("acc1"))

	now = now.Add(30 * time.Second)
	cb.clock = func() time.Time { return now }
	assert.True(t, cb.IsOpen("acc1"))

	now = now.Add(31 * time.Second)
	cb.clock = func() time.Time { return now }
	assert.False(t, cb.IsOpen("acc1"))

	cb.RecordFailure("acc1", "err2")
	assert.True(t, cb.IsOpen("acc1"))

	now = now.Add(time.Minute)
	cb.clock = func() time.Time { return now }
	assert.True(t, cb.IsOpen("acc1"))

	now = now.Add(time.Minute)
	cb.clock = func() time.Time { return now }
	assert.False(t, cb.IsOpen("acc1"))
}

func TestCircuitBreakerMaxBackoffCap(t *testing.T) {
	now := time.Now()
	cb := newTestCircuitBreaker(now)

	for i := 0; i < 10; i++ {
		cb.RecordFailure("acc1", "err")
	}

	now = now.Add(7 * time.Minute)
	cb.clock = func() time.Time { return now }
	assert.True(t, cb.IsOpen("acc1"))

	now = now.Add(time.Minute)
	cb.clock = func() time.Time { return now }
	assert.False(t, cb.IsOpen("acc1"))
}

func TestCircuitBreakerRecordSuccess(t *testing.T) {
	now := time.Now()
	cb := newTestCircuitBreaker(now)
	cb.RecordFailure("acc1", "timeout")
	assert.True(t, cb.IsOpen("acc1"))

	cb.RecordSuccess("acc1")
	assert.False(t, cb.IsOpen("acc1"))
}

func TestCircuitBreakerRecordFailureIncrements(t *testing.T) {
	now := time.Now()
	cb := newTestCircuitBreaker(now)
	cb.RecordFailure("acc1", "timeout")
	cb.RecordFailure("acc1", "timeout")
	cb.RecordFailure("acc1", "timeout")

	st := cb.Snapshot()["acc1"]
	assert.Equal(t, 3, st.Failures)
}

func TestCircuitBreakerReason(t *testing.T) {
	now := time.Now()
	cb := newTestCircuitBreaker(now)
	assert.Equal(t, "", cb.Reason("acc1"))

	cb.RecordFailure("acc1", "connection refused")
	assert.Equal(t, "connection refused", cb.Reason("acc1"))

	now = now.Add(2 * time.Minute)
	cb.clock = func() time.Time { return now }
	assert.Equal(t, "", cb.Reason("acc1"))
}

func TestCircuitBreakerSnapshot(t *testing.T) {
	now := time.Now()
	cb := newTestCircuitBreaker(now)
	cb.RecordFailure("acc1", "err1")
	cb.RecordFailure("acc2", "err2")
	cb.RecordSuccess("acc3")

	snap := cb.Snapshot()
	require.Len(t, snap, 3)

	assert.Equal(t, "acc1", snap["acc1"].AccountID)
	assert.Equal(t, 1, snap["acc1"].Failures)
	assert.Equal(t, "err1", snap["acc1"].LastReason)
	assert.True(t, snap["acc1"].Open)
	assert.Equal(t, now.Add(time.Minute), snap["acc1"].CooldownEnds)

	assert.Equal(t, "acc2", snap["acc2"].AccountID)
	assert.Equal(t, 1, snap["acc2"].Failures)
	assert.Equal(t, "err2", snap["acc2"].LastReason)
	assert.True(t, snap["acc2"].Open)

	assert.Equal(t, "acc3", snap["acc3"].AccountID)
	assert.Equal(t, 0, snap["acc3"].Failures)
	assert.False(t, snap["acc3"].Open)
}

func TestCircuitBreakerSeed(t *testing.T) {
	now := time.Now()
	cb := newTestCircuitBreaker(now)
	cb.Seed("acc1", 3)
	assert.True(t, cb.IsOpen("acc1"))

	st := cb.Snapshot()["acc1"]
	assert.Equal(t, 3, st.Failures)
	assert.Equal(t, now.Add(4*time.Minute), st.CooldownEnds)
}

func TestCircuitBreakerProbabilisticRetryAlways(t *testing.T) {
	now := time.Now()
	cb := NewCircuitBreaker(CircuitConfig{
		BaseCooldown:             time.Minute,
		MaxBackoffMultiplier:     8,
		ProbabilisticRetryChance: 1.0,
	}, func() time.Time { return now })
	cb.RecordFailure("acc1", "err")
	assert.False(t, cb.IsOpen("acc1"))
}

func TestCircuitBreakerProbabilisticRetryNever(t *testing.T) {
	now := time.Now()
	cb := NewCircuitBreaker(CircuitConfig{
		BaseCooldown:             time.Minute,
		MaxBackoffMultiplier:     8,
		ProbabilisticRetryChance: 0.0,
	}, func() time.Time { return now })
	cb.RecordFailure("acc1", "err")
	assert.True(t, cb.IsOpen("acc1"))
}

func TestCircuitBreakerProbabilisticRetryDistribution(t *testing.T) {
	now := time.Now()
	cb := NewCircuitBreaker(CircuitConfig{
		BaseCooldown:             time.Minute,
		MaxBackoffMultiplier:     8,
		ProbabilisticRetryChance: 0.1,
	}, func() time.Time { return now })
	cb.RecordFailure("acc1", "err")

	openCount := 0
	iterations := 1000
	for i := 0; i < iterations; i++ {
		if cb.IsOpen("acc1") {
			openCount++
		}
	}

	retryRate := float64(iterations-openCount) / float64(iterations)
	assert.InDelta(t, 0.1, retryRate, 0.05)
}

func TestCircuitBreakerConcurrency(t *testing.T) {
	now := time.Now()
	cb := newTestCircuitBreaker(now)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			cb.IsOpen("acc1")
		}()
		go func() {
			defer wg.Done()
			cb.RecordFailure("acc1", "timeout")
		}()
		go func() {
			defer wg.Done()
			cb.RecordSuccess("acc1")
		}()
	}
	wg.Wait()
}

func TestCircuitBreakerUnknownAccount(t *testing.T) {
	now := time.Now()
	cb := newTestCircuitBreaker(now)
	assert.False(t, cb.IsOpen("unknown"))
	assert.Equal(t, "", cb.Reason("unknown"))
}

func TestCircuitBreakerRecordSuccessUnknownAccount(t *testing.T) {
	now := time.Now()
	cb := newTestCircuitBreaker(now)
	cb.RecordSuccess("new-acc")
	assert.False(t, cb.IsOpen("new-acc"))
}
