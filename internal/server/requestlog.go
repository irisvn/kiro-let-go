package server

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/irisvn/kiro-let-go/internal/api/admin"
)

type RequestLogEntry = admin.RequestLogEntry

type RequestLog struct {
	mu      sync.RWMutex
	entries []RequestLogEntry
	maxSize int
}

func NewRequestLog(maxSize int) *RequestLog {
	if maxSize <= 0 {
		maxSize = 100
	}
	return &RequestLog{maxSize: maxSize}
}

func (rl *RequestLog) Add(entry RequestLogEntry) {
	if rl == nil {
		return
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if entry.ID == "" {
		entry.ID = uuid.NewString()
	}
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}
	if rl.maxSize <= 0 {
		rl.maxSize = 100
	}
	if len(rl.entries) >= rl.maxSize {
		copy(rl.entries, rl.entries[1:])
		rl.entries[len(rl.entries)-1] = entry
		return
	}
	rl.entries = append(rl.entries, entry)
}

func (rl *RequestLog) Entries() []RequestLogEntry {
	if rl == nil {
		return nil
	}
	rl.mu.RLock()
	defer rl.mu.RUnlock()
	out := make([]RequestLogEntry, len(rl.entries))
	for i := range rl.entries {
		out[i] = rl.entries[len(rl.entries)-1-i]
	}
	return out
}

func (rl *RequestLog) Clear() {
	if rl == nil {
		return
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.entries = nil
}

func RequestLogMiddleware(rl *RequestLog) gin.HandlerFunc {
	return func(c *gin.Context) {
		if rl == nil || !strings.HasPrefix(c.Request.URL.Path, "/v1/") {
			c.Next()
			return
		}

		started := time.Now()
		c.Next()

		status := c.Writer.Status()
		if status == 0 {
			status = http.StatusOK
		}
		entry := RequestLogEntry{
			ID:         uuid.NewString(),
			Timestamp:  time.Now().UTC(),
			Method:     c.Request.Method,
			Path:       c.Request.URL.Path,
			Status:     status,
			DurationMs: time.Since(started).Milliseconds(),
			ClientIP:   c.ClientIP(),
			Format:     requestFormat(c.Request.URL.Path),
		}
		if len(c.Errors) > 0 {
			entry.Error = c.Errors.Last().Error()
		}
		rl.Add(entry)
	}
}

func requestFormat(path string) string {
	if strings.HasPrefix(path, "/v1/chat/completions") || strings.HasPrefix(path, "/v1/models") {
		return "openai"
	}
	if strings.HasPrefix(path, "/v1/messages") {
		return "anthropic"
	}
	return ""
}
