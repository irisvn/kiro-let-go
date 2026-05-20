package logging

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/irisvn/kiro-let-go/internal/config"
)

const RequestIDKey contextKey = "request_id"

const loggerCtxKey = "logger"

type loggerKey struct{}

type contextKey string

// New creates a slog logger based on the provided config.
func New(cfg config.LoggingConfig) *slog.Logger {
	var level slog.Level
	switch strings.ToLower(cfg.Level) {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	switch strings.ToLower(cfg.Format) {
	case "json":
		handler = slog.NewJSONHandler(os.Stdout, opts)
	default:
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	return slog.New(handler)
}

// Middleware returns a gin middleware that logs requests with a request ID.
func Middleware(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		requestID := uuid.New().String()

		c.Set(string(RequestIDKey), requestID)
		c.Writer.Header().Set("X-Request-ID", requestID)

		reqLogger := logger.With(slog.String("request_id", requestID))
		c.Set(loggerCtxKey, reqLogger)

		c.Next()

		duration := time.Since(start)
		status := c.Writer.Status()

		headers := sanitizeHeaders(c.Request.Header)

		reqLogger.Info("request",
			slog.String("method", c.Request.Method),
			slog.String("path", c.Request.URL.Path),
			slog.Int("status", status),
			slog.Float64("duration_ms", float64(duration.Nanoseconds())/1e6),
			slog.String("client_ip", c.ClientIP()),
			slog.String("user_agent", c.Request.UserAgent()),
			slog.Any("headers", headers),
		)
	}
}

// FromContext returns the logger with request_id baked in from the context.
func FromContext(ctx context.Context) *slog.Logger {
	if val := ctx.Value(loggerKey{}); val != nil {
		if l, ok := val.(*slog.Logger); ok {
			return l
		}
	}
	if gctx, ok := ctx.(*gin.Context); ok {
		if val, exists := gctx.Get(loggerCtxKey); exists {
			if l, ok := val.(*slog.Logger); ok {
				return l
			}
		}
	}
	return slog.Default()
}

func sanitizeHeaders(headers http.Header) http.Header {
	sanitized := make(http.Header, len(headers))
	for k, v := range headers {
		if strings.EqualFold(k, "Authorization") {
			sanitized[k] = []string{"[REDACTED]"}
		} else {
			sanitized[k] = v
		}
	}
	return sanitized
}
