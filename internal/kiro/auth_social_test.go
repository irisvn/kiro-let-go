package kiro

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/irisvn/kiro-let-go/internal/account"
	"github.com/irisvn/kiro-let-go/internal/errs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSocialAuth_Refresh_MissingRefreshToken(t *testing.T) {
	auth := NewSocialAuth(nil, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	acc := &account.Account{MachineID: "deadbeef"}

	_, _, _, err := auth.Refresh(context.Background(), acc)
	require.Error(t, err)
	assert.True(t, errs.Is(err, errs.ClassFatal))
}

func TestSocialAuth_Refresh_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/refreshToken", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "KiroIDE-1.0.34-m1", r.Header.Get("User-Agent"))
		assert.Equal(t, "prod.eu-west-1.auth.desktop.kiro.dev", r.Host)
		assert.Equal(t, "close", r.Header.Get("Connection"))

		var body map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "rt-123", body["refreshToken"])

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"accessToken":"at-new","refreshToken":"rt-new","expiresIn":7200}`))
	}))
	defer server.Close()

	auth := NewSocialAuth(server.Client(), slog.New(slog.NewTextHandler(os.Stderr, nil)))
	auth.baseURL = server.URL
	acc := testAccount("eu-west-1", nil, "rt-123")

	at, rt, exp, err := auth.Refresh(context.Background(), acc)
	require.NoError(t, err)
	assert.Equal(t, "at-new", at)
	assert.Equal(t, "rt-new", rt)
	assert.WithinDuration(t, time.Now().UTC().Add(7200*time.Second), exp, 5*time.Second)
}

func TestSocialAuth_Refresh_SuccessDefaults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"accessToken":"at-only"}`))
	}))
	defer server.Close()

	auth := NewSocialAuth(server.Client(), slog.New(slog.NewTextHandler(os.Stderr, nil)))
	auth.baseURL = server.URL
	acc := testAccount("us-east-1", nil, "rt-123")

	at, rt, exp, err := auth.Refresh(context.Background(), acc)
	require.NoError(t, err)
	assert.Equal(t, "at-only", at)
	assert.Empty(t, rt)
	assert.WithinDuration(t, time.Now().UTC().Add(3600*time.Second), exp, 5*time.Second)
}

func TestSocialAuth_Refresh_MissingAccessToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"refreshToken":"rt-new"}`))
	}))
	defer server.Close()

	auth := NewSocialAuth(server.Client(), slog.New(slog.NewTextHandler(os.Stderr, nil)))
	auth.baseURL = server.URL
	acc := testAccount("us-east-1", nil, "rt-123")

	_, _, _, err := auth.Refresh(context.Background(), acc)
	require.Error(t, err)
	assert.True(t, errs.Is(err, errs.ClassFatal))
}

func TestSocialAuth_Refresh_InvalidGrant(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"Invalid refresh token provided"}`))
	}))
	defer server.Close()

	auth := NewSocialAuth(server.Client(), slog.New(slog.NewTextHandler(os.Stderr, nil)))
	auth.baseURL = server.URL
	acc := testAccount("us-east-1", nil, "rt-bad")

	_, _, _, err := auth.Refresh(context.Background(), acc)
	require.Error(t, err)
	assert.True(t, errs.Is(err, errs.ClassFatal))
	var ke *errs.Error
	require.True(t, errs.ClassOf(err) == errs.ClassFatal)
	require.ErrorAs(t, err, &ke)
	assert.Equal(t, "INVALID_REFRESH_TOKEN", ke.Code)
}

func TestSocialAuth_Refresh_BadRequestOther(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_request"}`))
	}))
	defer server.Close()

	auth := NewSocialAuth(server.Client(), slog.New(slog.NewTextHandler(os.Stderr, nil)))
	auth.baseURL = server.URL
	acc := testAccount("us-east-1", nil, "rt-123")

	_, _, _, err := auth.Refresh(context.Background(), acc)
	require.Error(t, err)
	assert.True(t, errs.Is(err, errs.ClassFatal))
}

func TestSocialAuth_Refresh_AuthFailures(t *testing.T) {
	for _, code := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(fmt.Sprintf("status-%d", code), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(code)
			}))
			defer server.Close()

			auth := NewSocialAuth(server.Client(), slog.New(slog.NewTextHandler(os.Stderr, nil)))
			auth.baseURL = server.URL
			acc := testAccount("us-east-1", nil, "rt-123")

			_, _, _, err := auth.Refresh(context.Background(), acc)
			require.Error(t, err)
			assert.True(t, errs.Is(err, errs.ClassFatal))
		})
	}
}

func TestSocialAuth_Refresh_RecoverableErrors(t *testing.T) {
	for _, code := range []int{http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway} {
		t.Run(fmt.Sprintf("status-%d", code), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(code)
			}))
			defer server.Close()

			auth := NewSocialAuth(server.Client(), slog.New(slog.NewTextHandler(os.Stderr, nil)))
			auth.baseURL = server.URL
			acc := testAccount("us-east-1", nil, "rt-123")

			_, _, _, err := auth.Refresh(context.Background(), acc)
			require.Error(t, err)
			assert.True(t, errs.Is(err, errs.ClassRecoverable))
		})
	}
}

func TestSocialAuth_Refresh_UnexpectedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	defer server.Close()

	auth := NewSocialAuth(server.Client(), slog.New(slog.NewTextHandler(os.Stderr, nil)))
	auth.baseURL = server.URL
	acc := testAccount("us-east-1", nil, "rt-123")

	_, _, _, err := auth.Refresh(context.Background(), acc)
	require.Error(t, err)
	assert.True(t, errs.Is(err, errs.ClassFatal))
}

func TestSocialAuth_Refresh_AuthRegionResolution(t *testing.T) {
	tests := []struct {
		name       string
		authRegion *string
		region     string
		wantHost   string
	}{
		{
			name:       "authRegion takes precedence",
			authRegion: strPtr("ap-south-1"),
			region:     "eu-west-1",
			wantHost:   "prod.ap-south-1.auth.desktop.kiro.dev",
		},
		{
			name:     "region fallback",
			region:   "eu-west-1",
			wantHost: "prod.eu-west-1.auth.desktop.kiro.dev",
		},
		{
			name:     "default us-east-1",
			wantHost: "prod.us-east-1.auth.desktop.kiro.dev",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedHost string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedHost = r.Host
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"accessToken":"at"}`))
			}))
			defer server.Close()

			auth := NewSocialAuth(server.Client(), slog.New(slog.NewTextHandler(os.Stderr, nil)))
			auth.baseURL = server.URL
			acc := testAccount(tt.region, tt.authRegion, "rt-123")

			_, _, _, err := auth.Refresh(context.Background(), acc)
			require.NoError(t, err)
			assert.Equal(t, tt.wantHost, capturedHost)
		})
	}
}

func TestSocialAuth_Refresh_ProxyClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"accessToken":"proxied"}`))
	}))
	defer server.Close()

	auth := NewSocialAuth(server.Client(), slog.New(slog.NewTextHandler(os.Stderr, nil)))
	auth.baseURL = server.URL
	acc := testAccount("us-east-1", nil, "rt-123")
	acc.ProxyURL = strPtr("http://invalid-proxy.local:9999")

	_, _, _, err := auth.Refresh(context.Background(), acc)
	require.Error(t, err)
	assert.True(t, errs.Is(err, errs.ClassNetwork) || errs.Is(err, errs.ClassRecoverable) || strings.Contains(err.Error(), "proxy"))
}

func TestSocialAuth_Refresh_InvalidProxyURL(t *testing.T) {
	auth := NewSocialAuth(nil, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	acc := testAccount("us-east-1", nil, "rt-123")
	acc.ProxyURL = strPtr("://not-a-url")

	_, _, _, err := auth.Refresh(context.Background(), acc)
	require.Error(t, err)
	assert.True(t, errs.Is(err, errs.ClassFatal))
}

func testAccount(region string, authRegion *string, refreshToken string) *account.Account {
	return &account.Account{
		Region:       region,
		AuthRegion:   authRegion,
		RefreshToken: &refreshToken,
		MachineID:    "m1",
	}
}

func strPtr(s string) *string {
	return &s
}
