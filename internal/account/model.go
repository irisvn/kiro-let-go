package account

import (
	"errors"
	"time"
)

// ErrNotFound is returned when an account does not exist in the store.
var ErrNotFound = errors.New("account not found")

// Account represents a Kiro service account.
type Account struct {
	ID             string
	Label          string
	AuthMethod     string
	AccessToken    *string
	RefreshToken   *string
	APIKey         *string
	ExpiresAt      *time.Time
	ProfileARN     *string
	Region         string
	AuthRegion     *string
	APIRegion      *string
	MachineID      string
	ProxyURL       *string
	ProxyUsername  *string
	ProxyPassword  *string
	Enabled        bool
	DisabledReason *string
	FailureCount   int
	LastFailureAt  *time.Time
	SuccessCount   int
	LastUsedAt     *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// QuotaCache represents cached quota information for an account.
type QuotaCache struct {
	AccountID   string
	PayloadJSON string
	FetchedAt   time.Time
}

// ListFilter provides filtering options for the List method.
type ListFilter struct {
	EnabledOnly bool
	AuthMethod  string
}
