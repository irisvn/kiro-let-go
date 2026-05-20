package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/irisvn/kiro-let-go/internal/api/admin"
)

type RequestLogEntry = admin.RequestLogEntry

const (
	CtxKeyModel        = "rl_model"
	CtxKeyInputTokens  = "rl_input_tokens"
	CtxKeyOutputTokens = "rl_output_tokens"
	CtxKeyStream       = "rl_stream"
	CtxKeyAccountID    = "rl_account_id"
	CtxKeyAccountLabel = "rl_account_label"
)

const requestLogSnippetLimit = 2048

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

		model, stream, requestBody := peekRequestLogBody(c.Request)
		bodyWriter := &requestLogResponseWriter{ResponseWriter: c.Writer, limit: requestLogSnippetLimit}
		c.Writer = bodyWriter

		started := time.Now()
		c.Next()

		status := c.Writer.Status()
		if status == 0 {
			status = http.StatusOK
		}
		entry := RequestLogEntry{
			ID:              uuid.NewString(),
			Timestamp:       time.Now().UTC(),
			Method:          c.Request.Method,
			Path:            c.Request.URL.Path,
			Status:          status,
			DurationMs:      time.Since(started).Milliseconds(),
			ClientIP:        c.ClientIP(),
			Model:           firstNonEmpty(getStringFromContext(c, CtxKeyModel), model),
			InputTokens:     getIntFromContext(c, CtxKeyInputTokens),
			OutputTokens:    getIntFromContext(c, CtxKeyOutputTokens),
			Stream:          getBoolFromContext(c, CtxKeyStream) || stream,
			AccountID:       getStringFromContext(c, CtxKeyAccountID),
			AccountLabel:    getStringFromContext(c, CtxKeyAccountLabel),
			RequestBody:     requestBody,
			ResponseSnippet: bodyWriter.snippet(),
			Format:          requestFormat(c.Request.URL.Path),
			UserAgent:       c.Request.UserAgent(),
		}
		if len(c.Errors) > 0 {
			entry.Error = c.Errors.Last().Error()
		}
		rl.Add(entry)
	}
}

func peekRequestLogBody(r *http.Request) (string, bool, string) {
	if r == nil || r.Body == nil || r.Method != http.MethodPost {
		return "", false, ""
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		r.Body = io.NopCloser(bytes.NewReader(nil))
		return "", false, ""
	}
	r.Body = io.NopCloser(bytes.NewReader(body))

	var partial struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	_ = json.Unmarshal(body, &partial)
	return partial.Model, partial.Stream, truncateLogString(string(body), requestLogSnippetLimit)
}

type requestLogResponseWriter struct {
	gin.ResponseWriter
	buf   bytes.Buffer
	limit int
}

func (w *requestLogResponseWriter) Write(data []byte) (int, error) {
	w.capture(data)
	return w.ResponseWriter.Write(data)
}

func (w *requestLogResponseWriter) WriteString(s string) (int, error) {
	w.capture([]byte(s))
	return w.ResponseWriter.WriteString(s)
}

func (w *requestLogResponseWriter) capture(data []byte) {
	if w == nil || w.limit <= 0 || len(data) == 0 || w.buf.Len() >= w.limit {
		return
	}
	remaining := w.limit - w.buf.Len()
	if len(data) > remaining {
		data = data[:remaining]
	}
	_, _ = w.buf.Write(data)
}

func (w *requestLogResponseWriter) snippet() string {
	if w == nil {
		return ""
	}
	return w.buf.String()
}

func getStringFromContext(c *gin.Context, key string) string {
	v, ok := c.Get(key)
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func getIntFromContext(c *gin.Context, key string) int {
	v, ok := c.Get(key)
	if !ok || v == nil {
		return 0
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

func getBoolFromContext(c *gin.Context, key string) bool {
	v, ok := c.Get(key)
	if !ok || v == nil {
		return false
	}
	b, ok := v.(bool)
	return ok && b
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func truncateLogString(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
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
