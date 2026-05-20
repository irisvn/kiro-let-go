package kiro

import (
	"context"
	"strings"
	"time"

	"github.com/irisvn/kiro-let-go/internal/account"
	"github.com/irisvn/kiro-let-go/internal/errs"
)

// APIKeyAuth is a bootstrap placeholder.
type APIKeyAuth struct{}

// NewAPIKeyAuth creates an API-key auth refresher.
func NewAPIKeyAuth() *APIKeyAuth { return &APIKeyAuth{} }

// Refresh returns the API key as a long-lived token.
func (a *APIKeyAuth) Refresh(ctx context.Context, acc *account.Account) (token string, expiresAt time.Time, err error) {
	_ = ctx

	if acc == nil || acc.APIKey == nil || !strings.HasPrefix(*acc.APIKey, "ksk_") {
		return "", time.Time{}, errs.New(errs.ClassFatal, "INVALID_API_KEY", "invalid API key")
	}

	return *acc.APIKey, time.Now().Add(100 * 365 * 24 * time.Hour), nil
}
