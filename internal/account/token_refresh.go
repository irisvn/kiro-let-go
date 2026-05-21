package account

import (
	"context"
	"log/slog"
	"time"
)

// SocialAuthRefresher refreshes OAuth/social-account credentials for the background token loop.
type SocialAuthRefresher interface {
	Refresh(ctx context.Context, acc *Account) (newAccessToken string, newRefreshToken string, expiresAt time.Time, err error)
}

// TokenRefresher proactively refreshes social-account tokens before expiry.
type TokenRefresher struct {
	store      *Store
	socialAuth SocialAuthRefresher
	logger     *slog.Logger
	interval   time.Duration
	threshold  time.Duration
}

// NewTokenRefresher creates a token refresher with production defaults.
func NewTokenRefresher(store *Store, socialAuth SocialAuthRefresher, logger *slog.Logger) *TokenRefresher {
	if store == nil || socialAuth == nil {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &TokenRefresher{
		store:      store,
		socialAuth: socialAuth,
		logger:     logger,
		interval:   5 * time.Minute,
		threshold:  10 * time.Minute,
	}
}

// Run refreshes once immediately, then repeats until ctx is cancelled.
func (tr *TokenRefresher) Run(ctx context.Context) error {
	if tr == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	tr.refreshExpiring(ctx)

	ticker := time.NewTicker(tr.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			tr.refreshExpiring(ctx)
		}
	}
}

func (tr *TokenRefresher) refreshExpiring(ctx context.Context) {
	accounts, err := tr.store.List(ctx, ListFilter{EnabledOnly: true})
	if err != nil {
		tr.logger.Warn("token refresh loop: failed to list accounts", "error", err)
		return
	}

	now := time.Now().UTC()
	for i := range accounts {
		acc := &accounts[i]
		if normalizeAuthMethod(acc.AuthMethod) != "social" {
			continue
		}
		if acc.ExpiresAt == nil {
			continue
		}
		if acc.ExpiresAt.Sub(now) > tr.threshold {
			continue
		}

		tr.logger.Info("token refresh loop: refreshing expiring token",
			"account_id", acc.ID,
			"label", acc.Label,
			"expires_at", acc.ExpiresAt,
			"expires_in", acc.ExpiresAt.Sub(now).Round(time.Second),
		)

		newToken, newRefresh, expiresAt, err := tr.socialAuth.Refresh(ctx, acc)
		if err != nil {
			tr.logger.Warn("token refresh loop: refresh failed",
				"account_id", acc.ID,
				"label", acc.Label,
				"error", err,
			)
			if acc.ExpiresAt.Before(now) {
				reason := "token refresh failed: " + err.Error()
				_ = tr.store.SetEnabled(ctx, acc.ID, false, &reason)
				tr.logger.Warn("token refresh loop: disabled account with expired token",
					"account_id", acc.ID,
					"label", acc.Label,
				)
			}
			continue
		}

		acc.AccessToken = &newToken
		if newRefresh != "" {
			acc.RefreshToken = &newRefresh
		}
		acc.ExpiresAt = &expiresAt
		if err := tr.store.Update(ctx, acc); err != nil {
			tr.logger.Warn("token refresh loop: failed to save refreshed token",
				"account_id", acc.ID,
				"error", err,
			)
			continue
		}

		tr.logger.Info("token refresh loop: token refreshed successfully",
			"account_id", acc.ID,
			"label", acc.Label,
			"new_expires_at", expiresAt,
		)
	}
}
