package errs

import (
	"context"
	"errors"
	"net"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	e := New(ClassRecoverable, "E001", "something went wrong")
	require.NotNil(t, e)
	assert.Equal(t, ClassRecoverable, e.Class)
	assert.Equal(t, "E001", e.Code)
	assert.Equal(t, "something went wrong", e.Message)
	assert.Nil(t, e.Cause)
}

func TestWrap(t *testing.T) {
	inner := errors.New("inner failure")
	e := Wrap(inner, ClassNetwork, "network hiccup")
	require.NotNil(t, e)
	assert.Equal(t, ClassNetwork, e.Class)
	assert.Equal(t, "network hiccup", e.Message)
	assert.Equal(t, inner, e.Cause)
	assert.ErrorIs(t, e, inner)
}

func TestIs(t *testing.T) {
	inner := New(ClassRecoverable, "R1", "recoverable")
	outer := Wrap(inner, ClassFatal, "fatal wrapper")

	assert.True(t, Is(outer, ClassFatal))
	assert.True(t, Is(outer, ClassRecoverable))
	assert.False(t, Is(outer, ClassAuthExpired))

	plain := errors.New("plain")
	assert.False(t, Is(plain, ClassFatal))
}

func TestClassOf(t *testing.T) {
	t.Run("classified error", func(t *testing.T) {
		e := New(ClassRateLimited, "RL1", "slow down")
		assert.Equal(t, ClassRateLimited, ClassOf(e))
	})

	t.Run("wrapped chain", func(t *testing.T) {
		inner := New(ClassQuotaExhausted, "Q1", "out of quota")
		outer := Wrap(inner, ClassFatal, "wrapped")
		assert.Equal(t, ClassFatal, ClassOf(outer))
	})

	t.Run("plain error defaults to fatal", func(t *testing.T) {
		assert.Equal(t, ClassFatal, ClassOf(errors.New("plain")))
	})
}

func TestFromKiroResponse(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantNil    bool
		wantClass  Class
		wantStatus int
	}{
		{
			name:    "200 is not an error",
			status:  200,
			body:    "ok",
			wantNil: true,
		},
		{
			name:       "402 with MONTHLY_REQUEST_COUNT is quota exhausted",
			status:     402,
			body:       `{"error": "MONTHLY_REQUEST_COUNT exceeded"}`,
			wantClass:  ClassQuotaExhausted,
			wantStatus: 402,
		},
		{
			name:       "401 is auth expired",
			status:     401,
			body:       `{"error": "unauthorized"}`,
			wantClass:  ClassAuthExpired,
			wantStatus: 401,
		},
		{
			name:       "403 is auth expired",
			status:     403,
			body:       `{"error": "forbidden"}`,
			wantClass:  ClassAuthExpired,
			wantStatus: 403,
		},
		{
			name:       "429 is rate limited",
			status:     429,
			body:       `{"error": "too many requests"}`,
			wantClass:  ClassRateLimited,
			wantStatus: 429,
		},
		{
			name:       "400 with CONTENT_LENGTH_EXCEEDS_THRESHOLD is content too long",
			status:     400,
			body:       `{"error": "CONTENT_LENGTH_EXCEEDS_THRESHOLD"}`,
			wantClass:  ClassContentTooLong,
			wantStatus: 400,
		},
		{
			name:       "400 other is fatal",
			status:     400,
			body:       `{"error": "bad request"}`,
			wantClass:  ClassFatal,
			wantStatus: 400,
		},
		{
			name:       "422 is fatal",
			status:     422,
			body:       `{"error": "unprocessable"}`,
			wantClass:  ClassFatal,
			wantStatus: 422,
		},
		{
			name:       "500 is recoverable",
			status:     500,
			body:       `{"error": "internal"}`,
			wantClass:  ClassRecoverable,
			wantStatus: 500,
		},
		{
			name:       "503 is recoverable",
			status:     503,
			body:       `{"error": "unavailable"}`,
			wantClass:  ClassRecoverable,
			wantStatus: 503,
		},
		{
			name:       "418 falls through to fatal",
			status:     418,
			body:       `{"error": "teapot"}`,
			wantClass:  ClassFatal,
			wantStatus: 418,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FromKiroResponse(tt.status, []byte(tt.body))
			if tt.wantNil {
				require.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, tt.wantClass, got.Class)
			assert.Equal(t, tt.wantStatus, got.HTTPStatus)
			assert.NotNil(t, got.Cause)
			assert.Contains(t, got.Cause.Error(), tt.body)
		})
	}
}

func TestFromNetwork(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantNil   bool
		wantClass Class
	}{
		{
			name:    "nil returns nil",
			err:     nil,
			wantNil: true,
		},
		{
			name:      "context.Canceled is client canceled",
			err:       context.Canceled,
			wantClass: ClassClientCanceled,
		},
		{
			name:      "timeout error is network",
			err:       &net.DNSError{Err: "lookup failed", IsTimeout: true},
			wantClass: ClassNetwork,
		},
		{
			name:      "connection refused via syscall",
			err:       &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED},
			wantClass: ClassNetwork,
		},
		{
			name:      "connection timed out via syscall",
			err:       &net.OpError{Op: "dial", Err: syscall.ETIMEDOUT},
			wantClass: ClassNetwork,
		},
		{
			name:      "host unreachable",
			err:       &net.OpError{Op: "dial", Err: syscall.EHOSTUNREACH},
			wantClass: ClassNetwork,
		},
		{
			name:      "dns no such host by string",
			err:       errors.New("lookup example.com: no such host"),
			wantClass: ClassNetwork,
		},
		{
			name:      "tls error by string",
			err:       errors.New("tls: handshake failure"),
			wantClass: ClassNetwork,
		},
		{
			name:      "connection refused by string",
			err:       errors.New("dial tcp: connection refused"),
			wantClass: ClassNetwork,
		},
		{
			name:      "timeout by string",
			err:       errors.New("i/o timeout"),
			wantClass: ClassNetwork,
		},
		{
			name:    "unrecognized error returns nil",
			err:     errors.New("something random"),
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FromNetwork(tt.err)
			if tt.wantNil {
				require.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, tt.wantClass, got.Class)
			assert.NotNil(t, got.Cause)
		})
	}
}
