package kiro

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/irisvn/kiro-let-go/internal/account"
	"github.com/irisvn/kiro-let-go/internal/errs"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func fastRetries(t *testing.T) {
	t.Helper()
	original := retryBackoffs
	retryBackoffs = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	t.Cleanup(func() { retryBackoffs = original })
}

func TestNewClientBaseTransportIsStreamingFriendlyAndConnectionClose(t *testing.T) {
	client := NewClient(15, slog.Default())

	require.NotNil(t, client.baseClient)
	require.Zero(t, client.baseClient.Timeout)

	transport, ok := client.baseClient.Transport.(*http.Transport)
	require.True(t, ok)
	require.True(t, transport.DisableKeepAlives)
}

func TestClientForAccountUsesBaseWithoutProxyAndCachesPerAccountProxyClients(t *testing.T) {
	client := NewClient(0, slog.Default())
	proxyURL := "http://127.0.0.1:18080"
	acc := &account.Account{ID: "acc-1", ProxyURL: &proxyURL}
	other := &account.Account{ID: "acc-2", ProxyURL: &proxyURL}

	require.Same(t, client.baseClient, client.clientForAccount(nil))
	require.Same(t, client.baseClient, client.clientForAccount(&account.Account{ID: "direct"}))
	require.Same(t, client.clientForAccount(acc), client.clientForAccount(acc))
	require.NotSame(t, client.clientForAccount(acc), client.clientForAccount(other))
}

func TestDoUsesHTTPProxyWithAccountCredentials(t *testing.T) {
	fastRetries(t)

	var gotAuth atomic.Value
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth.Store(r.Header.Get("Proxy-Authorization"))
		require.Equal(t, "http://kiro.example.test/path", r.URL.String())
		_, _ = w.Write([]byte("proxied"))
	}))
	t.Cleanup(proxyServer.Close)

	proxyURL := proxyServer.URL
	username := "user"
	password := "pass"
	acc := &account.Account{ID: "proxied", ProxyURL: &proxyURL, ProxyUsername: &username, ProxyPassword: &password}
	client := NewClient(0, slog.Default())
	req, err := http.NewRequest(http.MethodGet, "http://kiro.example.test/path", nil)
	require.NoError(t, err)

	resp, err := client.Do(context.Background(), acc, req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "proxied", string(body))
	require.Equal(t, "Basic dXNlcjpwYXNz", gotAuth.Load())
}

func TestDoRetries429And5xxThenReturnsSuccess(t *testing.T) {
	fastRetries(t)

	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch atomic.AddInt32(&calls, 1) {
		case 1:
			http.Error(w, "rate limited", http.StatusTooManyRequests)
		case 2:
			http.Error(w, "bad gateway", http.StatusBadGateway)
		default:
			_, _ = w.Write([]byte("ok"))
		}
	}))
	t.Cleanup(server.Close)

	client := NewClient(0, slog.Default())
	req, err := http.NewRequest(http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	resp, err := client.Do(context.Background(), nil, req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	require.Equal(t, int32(3), calls)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "ok", string(body))
}

func TestDoReturnsClassifiedKiroErrorAfterRetryExhaustion(t *testing.T) {
	fastRetries(t)

	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)

	client := NewClient(0, slog.Default())
	req, err := http.NewRequest(http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	resp, err := client.Do(context.Background(), nil, req)
	require.Error(t, err)
	require.Equal(t, int32(3), calls)
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	require.True(t, errs.Is(err, errs.ClassRecoverable))
	_ = resp.Body.Close()
}

func TestDoRetriesClassifiedNetworkErrors(t *testing.T) {
	fastRetries(t)

	var calls int32
	client := NewClient(0, slog.Default())
	client.baseClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if atomic.AddInt32(&calls, 1) < 3 {
			return nil, &net.DNSError{Err: "no such host", Name: "kiro.example.test"}
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("ok")),
			Request:    req,
		}, nil
	})
	req, err := http.NewRequest(http.MethodGet, "http://kiro.example.test", nil)
	require.NoError(t, err)

	resp, err := client.Do(context.Background(), nil, req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, int32(3), calls)
}

func TestDoReturnsNonRetryable4xxImmediatelyWithClassifiedError(t *testing.T) {
	fastRetries(t)

	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	t.Cleanup(server.Close)

	client := NewClient(0, slog.Default())
	req, err := http.NewRequest(http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	resp, err := client.Do(context.Background(), nil, req)
	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Equal(t, int32(1), calls)
	require.True(t, errs.Is(err, errs.ClassFatal))
	_ = resp.Body.Close()
}

func TestStreamReturnsUnbufferedBody(t *testing.T) {
	fastRetries(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.(http.Flusher).Flush()
		_, _ = w.Write([]byte("chunk"))
	}))
	t.Cleanup(server.Close)

	client := NewClient(0, slog.Default())
	req, err := http.NewRequest(http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	body, resp, err := client.Stream(context.Background(), nil, req)
	require.NoError(t, err)
	defer body.Close()
	data, err := io.ReadAll(body)
	require.NoError(t, err)

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "chunk", string(data))
}

func TestDoDoesNotRetryClientCanceledContext(t *testing.T) {
	fastRetries(t)

	client := NewClient(0, slog.Default())
	client.baseClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, context.Canceled
	})
	req, err := http.NewRequest(http.MethodGet, "http://kiro.example.test", nil)
	require.NoError(t, err)

	resp, err := client.Do(context.Background(), nil, req)
	require.Nil(t, resp)
	require.Error(t, err)
	require.True(t, errors.Is(err, context.Canceled))
	require.True(t, errs.Is(err, errs.ClassClientCanceled))
}
