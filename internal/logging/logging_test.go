package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/irisvn/kiro-let-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testRecord struct {
	record slog.Record
	attrs  []slog.Attr
}

type testHandler struct {
	records *[]testRecord
	attrs   []slog.Attr
	groups  []string
	opts    slog.HandlerOptions
}

func newTestHandler() *testHandler {
	recs := make([]testRecord, 0)
	return &testHandler{records: &recs}
}

func (h *testHandler) Enabled(_ context.Context, level slog.Level) bool {
	minLevel := slog.LevelInfo
	if h.opts.Level != nil {
		minLevel = h.opts.Level.Level()
	}
	return level >= minLevel
}

func (h *testHandler) Handle(_ context.Context, r slog.Record) error {
	*h.records = append(*h.records, testRecord{record: r, attrs: h.attrs})
	return nil
}

func (h *testHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &testHandler{
		records: h.records,
		attrs:   append(h.attrs, attrs...),
		groups:  h.groups,
		opts:    h.opts,
	}
}

func (h *testHandler) WithGroup(name string) slog.Handler {
	return &testHandler{
		records: h.records,
		attrs:   h.attrs,
		groups:  append(h.groups, name),
		opts:    h.opts,
	}
}

func (h *testHandler) Logger() *slog.Logger {
	return slog.New(h)
}

func (h *testHandler) Records() []testRecord {
	return *h.records
}

func TestNew_JSONFormat(t *testing.T) {
	cfg := config.LoggingConfig{Format: "json", Level: "info"}
	logger := New(cfg)
	require.NotNil(t, logger)
}

func TestNew_TextFormat(t *testing.T) {
	cfg := config.LoggingConfig{Format: "text", Level: "debug"}
	logger := New(cfg)
	require.NotNil(t, logger)
}

func TestNew_DefaultsToText(t *testing.T) {
	cfg := config.LoggingConfig{Format: "unknown", Level: "info"}
	logger := New(cfg)
	require.NotNil(t, logger)
}

func TestMiddleware_SetsRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newTestHandler()
	logger := h.Logger()

	r := gin.New()
	r.Use(Middleware(logger))
	r.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	requestID := w.Header().Get("X-Request-ID")
	require.NotEmpty(t, requestID)
	require.Len(t, h.Records(), 1)

	record := h.Records()[0]
	assert.Equal(t, slog.LevelInfo, record.record.Level)
	assert.Equal(t, "request", record.record.Message)

	attrs := recordAttrs(record)
	assert.Equal(t, "GET", attrs["method"])
	assert.Equal(t, "/test", attrs["path"])
	assert.Equal(t, int64(http.StatusOK), attrs["status"])
	assert.NotEmpty(t, attrs["duration_ms"])
	assert.Equal(t, requestID, attrs["request_id"])
}

func TestMiddleware_RedactsAuthorization(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newTestHandler()
	logger := h.Logger()

	r := gin.New()
	r.Use(Middleware(logger))
	r.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	req.Header.Set("X-Custom", "visible")
	r.ServeHTTP(w, req)

	require.Len(t, h.Records(), 1)
	record := h.Records()[0]
	attrs := recordAttrs(record)

	headers, ok := attrs["headers"].(map[string]interface{})
	require.True(t, ok, "headers should be a map")

	authVals, ok := headers["Authorization"].([]interface{})
	require.True(t, ok, "Authorization header should be present")
	require.Len(t, authVals, 1)
	assert.Equal(t, "[REDACTED]", authVals[0])

	customVals, ok := headers["X-Custom"].([]interface{})
	require.True(t, ok, "X-Custom header should be present")
	require.Len(t, customVals, 1)
	assert.Equal(t, "visible", customVals[0])
}

func TestMiddleware_RedactsAuthorizationCaseInsensitive(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newTestHandler()
	logger := h.Logger()

	r := gin.New()
	r.Use(Middleware(logger))
	r.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("authorization", "Basic secret")
	r.ServeHTTP(w, req)

	require.Len(t, h.Records(), 1)
	record := h.Records()[0]
	attrs := recordAttrs(record)

	headers, ok := attrs["headers"].(map[string]interface{})
	require.True(t, ok)

	authVals, ok := headers["Authorization"].([]interface{})
	require.True(t, ok)
	require.Len(t, authVals, 1)
	assert.Equal(t, "[REDACTED]", authVals[0])
}

func TestMiddleware_LogsOneLinePerRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newTestHandler()
	logger := h.Logger()

	r := gin.New()
	r.Use(Middleware(logger))
	r.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(w, req)

	assert.Len(t, h.Records(), 1)
}

func TestFromContext_ReturnsLoggerWithRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newTestHandler()
	logger := h.Logger()

	r := gin.New()
	r.Use(Middleware(logger))
	r.GET("/test", func(c *gin.Context) {
		l := FromContext(c)
		require.NotNil(t, l)
		l.Info("inside handler")
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(w, req)

	require.Len(t, h.Records(), 2)

	insideRecord := h.Records()[0]
	insideAttrs := recordAttrs(insideRecord)
	assert.NotEmpty(t, insideAttrs["request_id"])

	requestRecord := h.Records()[1]
	requestAttrs := recordAttrs(requestRecord)
	assert.Equal(t, insideAttrs["request_id"], requestAttrs["request_id"])
}

func TestFromContext_FallsBackToDefault(t *testing.T) {
	ctx := context.Background()
	l := FromContext(ctx)
	assert.NotNil(t, l)
	assert.Equal(t, slog.Default(), l)
}

func TestMiddleware_LogsRequiredFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newTestHandler()
	logger := h.Logger()

	r := gin.New()
	r.Use(Middleware(logger))
	r.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("User-Agent", "test-agent")
	req.RemoteAddr = "192.168.1.1:12345"
	r.ServeHTTP(w, req)

	require.Len(t, h.Records(), 1)
	record := h.Records()[0]
	attrs := recordAttrs(record)

	assert.Equal(t, "GET", attrs["method"])
	assert.Equal(t, "/test", attrs["path"])
	assert.Equal(t, int64(200), attrs["status"])
	assert.NotEmpty(t, attrs["duration_ms"])
	assert.Equal(t, "test-agent", attrs["user_agent"])
	assert.NotEmpty(t, attrs["client_ip"])
	assert.NotEmpty(t, attrs["request_id"])
}

func TestNew_LevelParsing(t *testing.T) {
	tests := []struct {
		level    string
		expected slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"INFO", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"WARN", slog.LevelWarn},
		{"error", slog.LevelError},
		{"ERROR", slog.LevelError},
		{"", slog.LevelInfo},
		{"unknown", slog.LevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			cfg := config.LoggingConfig{Format: "text", Level: tt.level}
			logger := New(cfg)
			require.NotNil(t, logger)

		})
	}
}

func TestNew_JSONOutput(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(handler)
	logger.Info("test", slog.String("key", "value"))

	output := buf.String()
	assert.True(t, strings.Contains(output, `"key":"value"`))
}

func recordAttrs(tr testRecord) map[string]interface{} {
	attrs := make(map[string]interface{})
	for _, a := range tr.attrs {
		attrs[a.Key] = attrValue(a.Value)
	}
	tr.record.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = attrValue(a.Value)
		return true
	})
	return attrs
}

func attrValue(v slog.Value) interface{} {
	switch v.Kind() {
	case slog.KindString:
		return v.String()
	case slog.KindInt64:
		return v.Int64()
	case slog.KindFloat64:
		return v.Float64()
	case slog.KindBool:
		return v.Bool()
	case slog.KindTime:
		return v.Time()
	case slog.KindGroup:
		group := make(map[string]interface{})
		for _, a := range v.Group() {
			group[a.Key] = attrValue(a.Value)
		}
		return group
	case slog.KindLogValuer:
		return attrValue(v.LogValuer().LogValue())
	case slog.KindAny:
		anyVal := v.Any()

		b, err := json.Marshal(anyVal)
		if err == nil {
			var result interface{}
			if err := json.Unmarshal(b, &result); err == nil {
				return result
			}
		}
		return anyVal
	default:
		return v.Any()
	}
}
