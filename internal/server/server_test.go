package server

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/irisvn/kiro-let-go/internal/account"
	"github.com/irisvn/kiro-let-go/internal/config"
	"github.com/irisvn/kiro-let-go/internal/version"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func testDeps(t *testing.T) Deps {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := account.OpenDB(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	ctx := context.Background()
	require.NoError(t, account.Apply(ctx, db))

	store, err := account.NewStore(db)
	require.NoError(t, err)

	circuit := account.NewCircuitBreaker(account.CircuitConfig{
		BaseCooldown:             time.Duration(60) * time.Second,
		MaxBackoffMultiplier:     1440,
		ProbabilisticRetryChance: 0.10,
	}, nil)

	return Deps{
		Cfg: &config.Config{
			Server: config.ServerConfig{
				Host:        "127.0.0.1",
				Port:        0,
				ProxyAPIKey: "proxy-key",
				AdminAPIKey: "admin-key",
			},
			Quota: config.QuotaConfig{CacheTTLSeconds: 43200},
		},
		Logger:  testLogger(),
		Store:   store,
		Circuit: circuit,
	}
}

func TestNewHealth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	orig := version.Version
	version.Version = "abc123"
	defer func() { version.Version = orig }()

	srv := New(Deps{Cfg: &config.Config{}, Logger: slog.Default()})
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	srv.engine.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"status":"ok","version":"abc123"}`, rec.Body.String())
}

func TestHealthNoAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := New(testDeps(t))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/health", nil)
	srv.engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "ok")
}

func TestRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := New(testDeps(t))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/health", nil)
	srv.engine.ServeHTTP(w, req)

	assert.NotEmpty(t, w.Header().Get("X-Request-ID"))

	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest(http.MethodGet, "/health", nil)
	req2.Header.Set("X-Request-ID", "existing-id")
	srv.engine.ServeHTTP(w2, req2)

	assert.Equal(t, "existing-id", w2.Header().Get("X-Request-ID"))
}

func TestCORSHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := New(testDeps(t))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodOptions, "/health", nil)
	srv.engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Contains(t, w.Header().Get("Access-Control-Allow-Headers"), "Authorization")
	assert.Contains(t, w.Header().Get("Access-Control-Allow-Headers"), "x-api-key")
	assert.Contains(t, w.Header().Get("Access-Control-Allow-Headers"), "anthropic-version")
}

func TestRecoveryMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps := testDeps(t)
	srv := New(deps)

	srv.engine.GET("/panic", func(c *gin.Context) {
		panic("boom")
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/panic", nil)
	srv.engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "internal_error")
	assert.NotContains(t, w.Body.String(), "boom")
}

func TestProxyAuthMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := New(testDeps(t))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/v1/models", nil)
	srv.engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "authentication_error")
}

func TestProxyAuthBearer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := New(testDeps(t))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer proxy-key")
	srv.engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestProxyAuthHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := New(testDeps(t))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("x-api-key", "proxy-key")
	srv.engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestProxyAuthInvalid(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := New(testDeps(t))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer wrong-key")
	srv.engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAdminAuthMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := New(testDeps(t))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/admin/accounts", nil)
	srv.engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "authentication_error")
}

func TestAdminAuthBearer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := New(testDeps(t))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/admin/accounts", nil)
	req.Header.Set("Authorization", "Bearer admin-key")
	srv.engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAdminAuthInvalid(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := New(testDeps(t))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/admin/accounts", nil)
	req.Header.Set("Authorization", "Bearer wrong-key")
	srv.engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAdminAuthXAPIKeyNotAllowed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := New(testDeps(t))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/admin/accounts", nil)
	req.Header.Set("x-api-key", "admin-key")
	srv.engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRunAndShutdown(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps := testDeps(t)
	deps.Cfg.Server.Port = 0
	srv := New(deps)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Run(ctx)
	}()

	var addr string
	select {
	case addr = <-srv.boundAddr:
	case <-time.After(5 * time.Second):
		t.Fatal("server did not start")
	}

	resp, err := http.Get("http://" + addr + "/health")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	cancel()

	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown timed out")
	}
}

func TestLoggingMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	deps := testDeps(t)
	deps.Logger = logger
	srv := New(deps)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("User-Agent", "test-agent")
	srv.engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	logged := buf.String()
	assert.Contains(t, logged, "method=GET")
	assert.Contains(t, logged, "path=/health")
	assert.Contains(t, logged, "status=200")
	assert.Contains(t, logged, "user_agent=test-agent")
	assert.Contains(t, logged, "request_id=")
}
