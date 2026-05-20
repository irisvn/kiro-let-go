package errs

import (
	"context"
	"errors"
	"net"
	"strings"
	"syscall"
)

// Class categorizes errors by their recoverability and root cause.
type Class int

const (
	ClassRecoverable Class = iota
	ClassFatal
	ClassQuotaExhausted
	ClassAuthExpired
	ClassRateLimited
	ClassContentTooLong
	ClassNetwork
	ClassClientCanceled
)

// Error is a typed error with classification metadata.
type Error struct {
	Class      Class
	Code       string
	HTTPStatus int
	Message    string
	Cause      error
}

// String returns a stable wire-friendly class name.
func (c Class) String() string {
	switch c {
	case ClassRecoverable:
		return "recoverable"
	case ClassFatal:
		return "fatal"
	case ClassQuotaExhausted:
		return "quota_exhausted"
	case ClassAuthExpired:
		return "auth_expired"
	case ClassRateLimited:
		return "rate_limited"
	case ClassContentTooLong:
		return "content_too_long"
	case ClassNetwork:
		return "network"
	case ClassClientCanceled:
		return "client_canceled"
	default:
		return "unknown"
	}
}

// Error implements the error interface.
func (e *Error) Error() string {
	return e.Message
}

// Unwrap returns the underlying cause for errors.Is/As traversal.
func (e *Error) Unwrap() error {
	return e.Cause
}

// New creates a new classified error.
func New(class Class, code, msg string) *Error {
	return &Error{
		Class:   class,
		Code:    code,
		Message: msg,
	}
}

// Wrap wraps an existing error with a classification.
func Wrap(err error, class Class, msg string) *Error {
	return &Error{
		Class:   class,
		Message: msg,
		Cause:   err,
	}
}

// Is reports whether err or any error in its chain has the given class.
func Is(err error, class Class) bool {
	var e *Error
	for {
		if errors.As(err, &e) && e.Class == class {
			return true
		}
		if unwrapped := errors.Unwrap(err); unwrapped != nil {
			err = unwrapped
		} else {
			break
		}
	}
	return false
}

// ClassOf extracts the classification from err, or ClassFatal if none is found.
func ClassOf(err error) Class {
	var e *Error
	for {
		if errors.As(err, &e) {
			return e.Class
		}
		if unwrapped := errors.Unwrap(err); unwrapped != nil {
			err = unwrapped
		} else {
			break
		}
	}
	return ClassFatal
}

// FromKiroResponse maps an HTTP response from Kiro to a classified error.
// It returns nil for status 200 (not an error).
func FromKiroResponse(status int, body []byte) *Error {
	if status == 200 {
		return nil
	}

	bodyStr := string(body)

	switch {
	case status == 402 && strings.Contains(bodyStr, "MONTHLY_REQUEST_COUNT"):
		return &Error{
			Class:      ClassQuotaExhausted,
			HTTPStatus: status,
			Message:    "quota exhausted",
			Cause:      errors.New(bodyStr),
		}
	case status == 401 || status == 403:
		return &Error{
			Class:      ClassAuthExpired,
			HTTPStatus: status,
			Message:    "authentication expired",
			Cause:      errors.New(bodyStr),
		}
	case status == 429:
		return &Error{
			Class:      ClassRateLimited,
			HTTPStatus: status,
			Message:    "rate limited",
			Cause:      errors.New(bodyStr),
		}
	case status == 400 && strings.Contains(bodyStr, "CONTENT_LENGTH_EXCEEDS_THRESHOLD"):
		return &Error{
			Class:      ClassContentTooLong,
			HTTPStatus: status,
			Message:    "content too long",
			Cause:      errors.New(bodyStr),
		}
	case status == 400 || status == 422:
		return &Error{
			Class:      ClassFatal,
			HTTPStatus: status,
			Message:    "fatal client error",
			Cause:      errors.New(bodyStr),
		}
	case status >= 500:
		return &Error{
			Class:      ClassRecoverable,
			HTTPStatus: status,
			Message:    "server error",
			Cause:      errors.New(bodyStr),
		}
	default:
		return &Error{
			Class:      ClassFatal,
			HTTPStatus: status,
			Message:    "unknown error",
			Cause:      errors.New(bodyStr),
		}
	}
}

// FromNetwork maps network-level errors to classified errors.
// It returns nil for unrecognized errors.
func FromNetwork(err error) *Error {
	if err == nil {
		return nil
	}

	if errors.Is(err, context.Canceled) {
		return &Error{
			Class:   ClassClientCanceled,
			Message: "client canceled request",
			Cause:   err,
		}
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return &Error{
				Class:   ClassNetwork,
				Message: "network timeout",
				Cause:   err,
			}
		}
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) {
		switch {
		case errors.Is(opErr.Err, syscall.ECONNREFUSED):
			return &Error{
				Class:   ClassNetwork,
				Message: "connection refused",
				Cause:   err,
			}
		case errors.Is(opErr.Err, syscall.ETIMEDOUT):
			return &Error{
				Class:   ClassNetwork,
				Message: "connection timed out",
				Cause:   err,
			}
		case errors.Is(opErr.Err, syscall.EHOSTUNREACH), errors.Is(opErr.Err, syscall.ENETUNREACH):
			return &Error{
				Class:   ClassNetwork,
				Message: "network unreachable",
				Cause:   err,
			}
		}
	}

	errStr := err.Error()
	if strings.Contains(errStr, "no such host") ||
		strings.Contains(errStr, "DNS") ||
		strings.Contains(errStr, "lookup") && strings.Contains(errStr, "failed") {
		return &Error{
			Class:   ClassNetwork,
			Message: "dns resolution failed",
			Cause:   err,
		}
	}
	if strings.Contains(errStr, "tls:") ||
		strings.Contains(errStr, "certificate") ||
		strings.Contains(errStr, "handshake") {
		return &Error{
			Class:   ClassNetwork,
			Message: "tls error",
			Cause:   err,
		}
	}
	if strings.Contains(errStr, "connection refused") {
		return &Error{
			Class:   ClassNetwork,
			Message: "connection refused",
			Cause:   err,
		}
	}
	if strings.Contains(errStr, "timeout") || strings.Contains(errStr, "timed out") {
		return &Error{
			Class:   ClassNetwork,
			Message: "network timeout",
			Cause:   err,
		}
	}

	return nil
}
