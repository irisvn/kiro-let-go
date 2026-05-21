package kiro

import (
	"context"
	"sync"
)

type AttemptLog struct {
	Attempt      int    `json:"attempt"`
	AccountID    string `json:"account_id,omitempty"`
	AccountLabel string `json:"account_label,omitempty"`
	Region       string `json:"region,omitempty"`
	Status       int    `json:"status,omitempty"`
	DurationMs   int64  `json:"duration_ms"`
	Error        string `json:"error,omitempty"`
}

type AttemptsCollector struct {
	mu       sync.Mutex
	Attempts []AttemptLog
}

func (c *AttemptsCollector) Add(log AttemptLog) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Attempts = append(c.Attempts, log)
}

type contextKey string

const AttemptsCollectorKey contextKey = "attempts_collector"

func WithAttemptsCollector(ctx context.Context) (context.Context, *AttemptsCollector) {
	coll := &AttemptsCollector{}
	return context.WithValue(ctx, AttemptsCollectorKey, coll), coll
}

func GetAttemptsCollector(ctx context.Context) *AttemptsCollector {
	if ctx == nil {
		return nil
	}
	if coll, ok := ctx.Value(AttemptsCollectorKey).(*AttemptsCollector); ok {
		return coll
	}
	return nil
}
