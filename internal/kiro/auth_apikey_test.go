package kiro

import (
	"context"
	"testing"
	"time"

	"github.com/irisvn/kiro-let-go/internal/account"
	"github.com/irisvn/kiro-let-go/internal/errs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyAuthRefresh(t *testing.T) {
	auth := &APIKeyAuth{}
	now := time.Now()

	t.Run("valid api key", func(t *testing.T) {
		key := "ksk_test_123"
		acc := &account.Account{APIKey: &key}

		token, expiresAt, err := auth.Refresh(context.Background(), acc)
		require.NoError(t, err)
		assert.Equal(t, key, token)
		assert.WithinDuration(t, now.Add(100*365*24*time.Hour), expiresAt, time.Second)
	})

	t.Run("invalid api key shape", func(t *testing.T) {
		key := "bad_key"
		acc := &account.Account{APIKey: &key}

		token, expiresAt, err := auth.Refresh(context.Background(), acc)
		assert.Empty(t, token)
		assert.True(t, expiresAt.IsZero())
		require.Error(t, err)
		assert.True(t, errs.Is(err, errs.ClassFatal))
		assert.Equal(t, "INVALID_API_KEY", err.(*errs.Error).Code)
	})
}
