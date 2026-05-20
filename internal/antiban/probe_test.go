package antiban_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/irisvn/kiro-let-go/internal/antiban"
)

func TestIsHealthProbe_KubeProbe(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("User-Agent", "kube-probe/1.28")

	assert.True(t, antiban.IsHealthProbe(req))
}

func TestIsHealthProbe_HealthzPath(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	assert.True(t, antiban.IsHealthProbe(req))
}

func TestIsHealthProbe_NormalRequest(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	assert.False(t, antiban.IsHealthProbe(req))
}

func TestHealthProbeMiddleware_ShortCircuits(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	called := false
	r.Use(antiban.HealthProbeMiddleware())
	r.GET("/any", func(c *gin.Context) {
		called = true
		c.String(http.StatusOK, "handler")
	})

	req := httptest.NewRequest(http.MethodGet, "/any", nil)
	req.Header.Set("User-Agent", "kube-probe/1.28")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"status":"ok"}`, rec.Body.String())
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.False(t, called, "downstream handler should not be called for probe requests")
}
