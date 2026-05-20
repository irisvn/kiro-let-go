package kiro

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/irisvn/kiro-let-go/internal/account"
	"github.com/irisvn/kiro-let-go/internal/errs"
	"golang.org/x/net/proxy"
)

const maxAttempts = 3

var retryBackoffs = []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}

// Client sends requests to Kiro with per-account proxy isolation.
type Client struct {
	baseClient *http.Client
	logger     *slog.Logger
	clients    sync.Map
}

// NewClient creates a Kiro HTTP client. The http.Client timeout is left at zero
// so streaming responses are not interrupted by the client itself.
func NewClient(timeoutSec int, logger *slog.Logger) *Client {
	_ = timeoutSec
	if logger == nil {
		logger = slog.Default()
	}

	return &Client{
		baseClient: &http.Client{
			Timeout:   0,
			Transport: newTransport(0, nil),
		},
		logger: logger,
	}
}

func newTransport(timeout time.Duration, proxyURL func(*http.Request) (*url.URL, error)) *http.Transport {
	return &http.Transport{
		Proxy:                 proxyURL,
		DialContext:           (&net.Dialer{Timeout: timeout, KeepAlive: 0}).DialContext,
		ForceAttemptHTTP2:     true,
		DisableKeepAlives:     true,
		MaxIdleConns:          0,
		IdleConnTimeout:       0,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
}

func (c *Client) clientForAccount(acc *account.Account) *http.Client {
	if acc == nil || acc.ProxyURL == nil || strings.TrimSpace(*acc.ProxyURL) == "" {
		return c.baseClient
	}

	key := acc.ID
	if key == "" {
		key = *acc.ProxyURL
	}
	if cached, ok := c.clients.Load(key); ok {
		return cached.(*http.Client)
	}

	client := &http.Client{Timeout: 0}
	transport, err := c.transportForProxy(acc)
	if err != nil {
		transport = newTransport(0, nil)
		if c.logger != nil {
			c.logger.Warn("invalid account proxy; using direct transport", "account_id", acc.ID, "error", err)
		}
	}
	client.Transport = transport

	actual, _ := c.clients.LoadOrStore(key, client)
	return actual.(*http.Client)
}

func (c *Client) transportForProxy(acc *account.Account) (http.RoundTripper, error) {
	parsed, err := accountProxyURL(acc)
	if err != nil {
		return nil, err
	}

	switch parsed.Scheme {
	case "http", "https":
		return newTransport(0, http.ProxyURL(parsed)), nil
	case "socks5", "socks5h":
		var auth *proxy.Auth
		if parsed.User != nil {
			password, _ := parsed.User.Password()
			auth = &proxy.Auth{User: parsed.User.Username(), Password: password}
		}
		dialer, err := proxy.SOCKS5("tcp", parsed.Host, auth, proxy.Direct)
		if err != nil {
			return nil, err
		}
		return &http.Transport{
			DialContext:           dialContext(dialer),
			ForceAttemptHTTP2:     true,
			DisableKeepAlives:     true,
			MaxIdleConns:          0,
			IdleConnTimeout:       0,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: time.Second,
		}, nil
	default:
		return nil, errors.New("unsupported proxy scheme")
	}
}

func accountProxyURL(acc *account.Account) (*url.URL, error) {
	if acc == nil || acc.ProxyURL == nil {
		return nil, errors.New("missing proxy URL")
	}
	parsed, err := url.Parse(strings.TrimSpace(*acc.ProxyURL))
	if err != nil {
		return nil, err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, errors.New("invalid proxy URL")
	}
	if acc.ProxyUsername != nil {
		password := ""
		if acc.ProxyPassword != nil {
			password = *acc.ProxyPassword
		}
		parsed.User = url.UserPassword(*acc.ProxyUsername, password)
	}
	return parsed, nil
}

func dialContext(dialer proxy.Dialer) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		type contextDialer interface {
			DialContext(context.Context, string, string) (net.Conn, error)
		}
		if d, ok := dialer.(contextDialer); ok {
			return d.DialContext(ctx, network, address)
		}

		connCh := make(chan net.Conn, 1)
		errCh := make(chan error, 1)
		go func() {
			conn, err := dialer.Dial(network, address)
			if err != nil {
				errCh <- err
				return
			}
			connCh <- conn
		}()

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case err := <-errCh:
			return nil, err
		case conn := <-connCh:
			return conn, nil
		}
	}
}

// Do sends req with the account-specific client and retries recoverable Kiro and
// network failures. It never consumes a response body returned to the caller.
func (c *Client) Do(ctx context.Context, acc *account.Account, req *http.Request) (*http.Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	client := c.clientForAccount(acc)

	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		attemptReq, err := requestForAttempt(ctx, req, attempt)
		if err != nil {
			return nil, err
		}

		resp, err := client.Do(attemptReq)
		if err != nil {
			classified := errs.FromNetwork(err)
			if classified == nil {
				classified = errs.Wrap(err, errs.ClassNetwork, "network error")
			}
			lastErr = classified
			if !shouldRetryError(classified) || attempt == maxAttempts-1 {
				return nil, classified
			}
			if err := sleepBackoff(ctx, attempt); err != nil {
				return nil, err
			}
			continue
		}

		if !shouldRetryStatus(resp.StatusCode) {
			return resp, kiroResponseError(resp.StatusCode)
		}

		classified := kiroResponseError(resp.StatusCode)
		lastErr = classified
		if attempt == maxAttempts-1 {
			return resp, classified
		}
		_ = resp.Body.Close()
		if err := sleepBackoff(ctx, attempt); err != nil {
			return nil, err
		}
	}

	return nil, lastErr
}

// Stream sends req and returns the response body without buffering it.
func (c *Client) Stream(ctx context.Context, acc *account.Account, req *http.Request) (io.ReadCloser, *http.Response, error) {
	resp, err := c.Do(ctx, acc, req)
	if err != nil {
		return nil, resp, err
	}
	if resp == nil {
		return nil, nil, errors.New("nil response")
	}
	return resp.Body, resp, nil
}

func requestForAttempt(ctx context.Context, req *http.Request, attempt int) (*http.Request, error) {
	if req == nil {
		return nil, errors.New("nil request")
	}
	if attempt == 0 {
		return req.WithContext(ctx), nil
	}
	clone := req.Clone(ctx)
	if req.Body != nil {
		if req.GetBody == nil {
			return nil, errors.New("request body is not replayable")
		}
		body, err := req.GetBody()
		if err != nil {
			return nil, err
		}
		clone.Body = body
	}
	return clone, nil
}

func shouldRetryStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

func kiroResponseError(status int) error {
	classified := errs.FromKiroResponse(status, nil)
	if classified == nil {
		return nil
	}
	return classified
}

func shouldRetryError(err error) bool {
	return errs.Is(err, errs.ClassNetwork)
}

func sleepBackoff(ctx context.Context, attempt int) error {
	d := retryBackoffs[attempt]
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		classified := errs.FromNetwork(ctx.Err())
		if classified != nil {
			return classified
		}
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
