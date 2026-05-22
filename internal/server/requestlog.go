package server

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/irisvn/kiro-let-go/internal/api/admin"
	"github.com/irisvn/kiro-let-go/internal/config"
	"github.com/irisvn/kiro-let-go/internal/kiro"
)

type RequestLogEntry = admin.RequestLogEntry

const (
	CtxKeyModel        = "rl_model"
	CtxKeyInputTokens  = "rl_input_tokens"
	CtxKeyOutputTokens = "rl_output_tokens"
	CtxKeyStream       = "rl_stream"
	CtxKeyAccountID    = "rl_account_id"
	CtxKeyAccountLabel = "rl_account_label"
	CtxKeyKiroPayload  = "rl_kiro_payload"
)

const requestLogSnippetLimit = 100 * 1024 // 100KB cho file log

type RequestLog struct {
	mu       sync.RWMutex
	entries  []RequestLogEntry
	maxSize  int
	filePath string
	fileMu   sync.Mutex
}

func NewRequestLog(maxSize int) *RequestLog {
	if maxSize <= 0 {
		maxSize = 100
	}
	return &RequestLog{maxSize: maxSize}
}

func NewRequestLogWithFile(maxSize int, filePath string) *RequestLog {
	rl := NewRequestLog(maxSize)
	if filePath != "" {
		rl.filePath = filePath
		// Ensure parent directory exists
		if dir := filepath.Dir(filePath); dir != "" {
			_ = os.MkdirAll(dir, 0755)
		}
	}
	return rl
}

func (rl *RequestLog) Add(entry RequestLogEntry) {
	if rl == nil {
		return
	}
	if entry.ID == "" {
		entry.ID = uuid.NewString()
	}
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}

	// Write to file first (before locking, since fileMu is separate)
	if rl.filePath != "" {
		rl.appendToFile(entry)
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()
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

func (rl *RequestLog) appendToFile(entry RequestLogEntry) {
	rl.fileMu.Lock()
	defer rl.fileMu.Unlock()

	// Check size and rotate if necessary (>= 30MB)
	if info, err := os.Stat(rl.filePath); err == nil && info.Size() >= 30*1024*1024 {
		rl.rotateFiles()
	}

	f, err := os.OpenFile(rl.filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	line, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_, _ = f.Write(line)
	_, _ = f.Write([]byte("\n"))
}

func (rl *RequestLog) rotateFiles() {
	maxBackups := 9
	// 1. Remove oldest backup
	oldestFile := fmt.Sprintf("%s.%d", rl.filePath, maxBackups)
	_ = os.Remove(oldestFile)

	// 2. Shift backups (8 down to 1)
	for i := maxBackups - 1; i >= 1; i-- {
		src := fmt.Sprintf("%s.%d", rl.filePath, i)
		dst := fmt.Sprintf("%s.%d", rl.filePath, i+1)
		if _, err := os.Stat(src); err == nil {
			_ = os.Rename(src, dst)
		}
	}

	// 3. Rename current file to .1
	_ = os.Rename(rl.filePath, fmt.Sprintf("%s.1", rl.filePath))
}

func (rl *RequestLog) LoadFromFile() error {
	if rl == nil || rl.filePath == "" {
		return nil
	}

	f, err := os.Open(rl.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	rl.mu.Lock()
	defer rl.mu.Unlock()

	scanner := bufio.NewScanner(f)
	// Increase scanner buffer for long lines
	const maxCapacity = 1024 * 1024 // 1MB
	buf := make([]byte, maxCapacity)
	scanner.Buffer(buf, maxCapacity)

	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var entry RequestLogEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		rl.entries = append(rl.entries, entry)
	}

	// Trim to maxSize
	if len(rl.entries) > rl.maxSize {
		rl.entries = rl.entries[len(rl.entries)-rl.maxSize:]
	}
	return scanner.Err()
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
	if rl.filePath != "" {
		rl.fileMu.Lock()
		defer rl.fileMu.Unlock()
		_ = os.Remove(rl.filePath)
	}
}

func RequestLogMiddleware(rl *RequestLog, dc *config.DynamicConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if dc != nil && !dc.Get().RequestLogEnabled {
			c.Next()
			return
		}

		path := c.Request.URL.Path
		shouldLog := strings.HasPrefix(path, "/v1/") || strings.Contains(path, "/chat-test") || strings.Contains(path, "/test")
		if rl == nil || !shouldLog {
			c.Next()
			return
		}

		// Inject AttemptsCollector in request context
		reqCtx, collector := kiro.WithAttemptsCollector(c.Request.Context())
		c.Request = c.Request.WithContext(reqCtx)

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
			RequestID:       getStringFromContext(c, "request_id"),
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
			KiroPayload:     getStringFromContext(c, CtxKeyKiroPayload),
			ResponseSnippet: bodyWriter.snippet(),
			Format:          requestFormat(c.Request.URL.Path),
			UserAgent:       c.Request.UserAgent(),
		}
		if collector != nil {
			entry.Attempts = collector.Attempts
		}
		if len(c.Errors) > 0 {
			var errMsgs []string
			for _, e := range c.Errors {
				errMsgs = append(errMsgs, e.Error())
			}
			entry.Error = strings.Join(errMsgs, "; ")
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
