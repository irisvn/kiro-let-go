package account

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/irisvn/kiro-let-go/internal/errs"
)

const refreshSkew = time.Minute

// ManagerConfig controls account selection behavior.
type ManagerConfig struct {
	StickySession bool
	DefaultRegion string
}

// SelectionHint provides request context used to choose an account.
type SelectionHint struct {
	ConversationID string
	Model          string
	ExcludeIDs     []string
}

// Acquisition contains an acquired account, token, region, and release callbacks.
type Acquisition struct {
	Account        *Account
	Token          string
	Region         string
	ReleaseSuccess func()
	ReleaseFailure func(reason string)
}

// SocialRefresher refreshes OAuth/social-account credentials.
type SocialRefresher interface {
	Refresh(ctx context.Context, acc *Account) (newAccessToken, newRefreshToken string, expiresAt time.Time, err error)
}

// APIKeyRefresher refreshes or validates API-key credentials.
type APIKeyRefresher interface {
	Refresh(ctx context.Context, acc *Account) (token string, expiresAt time.Time, err error)
}

// ManagerOption customizes a Manager during construction.
type ManagerOption func(*Manager)

// Manager acquires, refreshes, and records health for configured accounts.
type Manager struct {
	store      *Store
	balancer   Balancer
	circuit    *CircuitBreaker
	mu         sync.RWMutex
	tokenLocks sync.Map /* accountID → *sync.Mutex */
	cfg        ManagerConfig
	logger     *slog.Logger

	socialAuth       SocialRefresher
	apiKeyAuth       APIKeyRefresher
	lastSuccessfulID string
}

// NewManager creates an account manager with optional balancer, circuit breaker, and refreshers.
func NewManager(store *Store, balancer Balancer, circuit *CircuitBreaker, cfg ManagerConfig, logger *slog.Logger, opts ...ManagerOption) *Manager {
	if balancer == nil {
		balancer = &RoundRobin{}
	}
	if circuit == nil {
		circuit = NewCircuitBreaker(defaultCircuitConfig(), nil)
	}
	if logger == nil {
		logger = slog.Default()
	}
	m := &Manager{store: store, balancer: balancer, circuit: circuit, cfg: cfg, logger: logger, apiKeyAuth: localAPIKeyRefresher{}}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// WithSocialAuth configures the social-account refresher used by a Manager.
func WithSocialAuth(auth SocialRefresher) ManagerOption {
	return func(m *Manager) { m.socialAuth = auth }
}

// WithAPIKeyAuth configures the API-key refresher used by a Manager.
func WithAPIKeyAuth(auth APIKeyRefresher) ManagerOption {
	return func(m *Manager) { m.apiKeyAuth = auth }
}

// Acquire selects and prepares an account for a request.
func (m *Manager) Acquire(ctx context.Context, hint SelectionHint) (*Acquisition, error) {
	if m == nil || m.store == nil {
		return nil, fmt.Errorf("account manager store is nil")
	}

	accounts, err := m.store.List(ctx, ListFilter{EnabledOnly: true})
	if err != nil {
		return nil, err
	}
	candidates := m.filterCandidates(ctx, accounts, hint)
	if len(candidates) == 0 {
		return nil, ErrNoCandidates
	}

	if sticky := m.stickyCandidate(candidates, hint); sticky != nil {
		return m.acquireAccount(ctx, sticky)
	}

	chosen, err := m.balancer.Pick(ctx, candidates)
	if err != nil {
		return nil, err
	}
	return m.acquireAccount(ctx, chosen)
}

// AcquireAccount prepares a specific account for a request without consulting
// the balancer. Callers remain responsible for checking account eligibility.
func (m *Manager) AcquireAccount(ctx context.Context, acc *Account) (*Acquisition, error) {
	if m == nil || m.store == nil {
		return nil, fmt.Errorf("account manager store is nil")
	}
	if acc == nil {
		return nil, fmt.Errorf("account is nil")
	}
	return m.acquireAccount(ctx, acc)
}

// Refresh forces a credential refresh for the specified account.
func (m *Manager) Refresh(ctx context.Context, accountID string) error {
	if m == nil || m.store == nil {
		return fmt.Errorf("account manager store is nil")
	}
	acc, err := m.store.Get(ctx, accountID)
	if err != nil {
		return err
	}
	_, _, err = m.refreshLocked(ctx, acc, true)
	return err
}

// List returns all accounts with circuit-breaker disabled reasons applied.
func (m *Manager) List(ctx context.Context) ([]*Account, error) {
	if m == nil || m.store == nil {
		return nil, fmt.Errorf("account manager store is nil")
	}
	accounts, err := m.store.List(ctx, ListFilter{})
	if err != nil {
		return nil, err
	}
	out := make([]*Account, 0, len(accounts))
	for i := range accounts {
		acc := accounts[i]
		if m.circuit != nil && m.circuit.IsOpen(acc.ID) {
			reason := m.circuit.Reason(acc.ID)
			acc.DisabledReason = &reason
		}
		out = append(out, &acc)
	}
	return out, nil
}

func (m *Manager) filterCandidates(ctx context.Context, accounts []Account, hint SelectionHint) []*Account {
	excluded := make(map[string]struct{}, len(hint.ExcludeIDs))
	for _, id := range hint.ExcludeIDs {
		excluded[id] = struct{}{}
	}
	candidates := make([]*Account, 0, len(accounts))
	for i := range accounts {
		acc := accounts[i]
		if !acc.Enabled {
			continue
		}
		if _, ok := excluded[acc.ID]; ok {
			continue
		}
		if m.circuit != nil && m.circuit.IsOpen(acc.ID) && !m.circuit.ShouldRetry() {
			continue
		}
		if isOpusModel(hint.Model) && m.hasFreeSubscription(ctx, acc.ID) {
			continue
		}
		candidates = append(candidates, &acc)
	}
	return candidates
}

func (m *Manager) stickyCandidate(candidates []*Account, hint SelectionHint) *Account {
	if !m.cfg.StickySession || len(hint.ExcludeIDs) > 0 {
		return nil
	}
	m.mu.RLock()
	last := m.lastSuccessfulID
	m.mu.RUnlock()
	if last == "" {
		return nil
	}
	for _, acc := range candidates {
		if acc.ID == last {
			return acc
		}
	}
	return nil
}

func (m *Manager) acquireAccount(ctx context.Context, acc *Account) (*Acquisition, error) {
	refreshed, token, err := m.refreshLocked(ctx, acc, false)
	if err != nil {
		return nil, err
	}
	region := strings.TrimSpace(refreshed.Region)
	if region == "" {
		region = strings.TrimSpace(m.cfg.DefaultRegion)
	}
	if region == "" {
		region = "us-east-1"
	}
	id := refreshed.ID
	return &Acquisition{
		Account: refreshed,
		Token:   token,
		Region:  region,
		ReleaseSuccess: func() {
			if err := m.store.RecordSuccess(context.Background(), id); err != nil {
				m.logger.Warn("failed to record account success", "account_id", id, "error", err)
			}
			m.circuit.RecordSuccess(id)
			if advancer, ok := m.balancer.(interface{ Advance() }); ok {
				advancer.Advance()
			}
			if m.cfg.StickySession {
				m.mu.Lock()
				m.lastSuccessfulID = id
				m.mu.Unlock()
			}
		},
		ReleaseFailure: func(reason string) {
			if err := m.store.RecordFailure(context.Background(), id, reason); err != nil {
				m.logger.Warn("failed to record account failure", "account_id", id, "reason", reason, "error", err)
			}
			m.circuit.RecordFailure(id, reason)
		},
	}, nil
}

func (m *Manager) refreshLocked(ctx context.Context, acc *Account, force bool) (*Account, string, error) {
	lockAny, _ := m.tokenLocks.LoadOrStore(acc.ID, &sync.Mutex{})
	lock := lockAny.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	fresh, err := m.store.Get(ctx, acc.ID)
	if err != nil {
		return nil, "", err
	}
	if !force && !needsRefresh(fresh) {
		return fresh, currentToken(fresh), nil
	}

	switch normalizeAuthMethod(fresh.AuthMethod) {
	case "api_key":
		if m.apiKeyAuth == nil {
			return nil, "", fmt.Errorf("api key refresher is nil")
		}
		token, expiresAt, err := m.apiKeyAuth.Refresh(ctx, fresh)
		if err != nil {
			return nil, "", err
		}
		fresh.AccessToken = &token
		fresh.ExpiresAt = &expiresAt
		if err := m.store.Update(ctx, fresh); err != nil {
			return nil, "", err
		}
		return fresh, token, nil
	default:
		if m.socialAuth == nil {
			return nil, "", fmt.Errorf("social auth refresher is nil")
		}
		access, refresh, expiresAt, err := m.socialAuth.Refresh(ctx, fresh)
		if err != nil {
			return nil, "", err
		}
		fresh.AccessToken = &access
		if refresh != "" {
			fresh.RefreshToken = &refresh
		}
		fresh.ExpiresAt = &expiresAt
		if err := m.store.Update(ctx, fresh); err != nil {
			return nil, "", err
		}
		return fresh, access, nil
	}
}

func defaultCircuitConfig() CircuitConfig {
	return CircuitConfig{BaseCooldown: time.Minute, MaxBackoffMultiplier: 32, ProbabilisticRetryChance: 0.10}
}

func (m *Manager) hasFreeSubscription(ctx context.Context, accountID string) bool {
	qc, err := m.store.GetQuota(ctx, accountID)
	if errors.Is(err, ErrNotFound) {
		return false
	}
	if err != nil {
		m.logger.Warn("quota lookup failed during opus selection", "account_id", accountID, "error", err)
		return false
	}
	quota, err := parseQuota([]byte(qc.PayloadJSON), qc.FetchedAt)
	if err == nil {
		return strings.Contains(strings.ToUpper(quota.SubscriptionTitle), "FREE")
	}
	var raw map[string]any
	if json.Unmarshal([]byte(qc.PayloadJSON), &raw) == nil {
		if title, ok := findString(raw, "subscriptionTitle"); ok {
			return strings.Contains(strings.ToUpper(title), "FREE")
		}
	}
	return false
}

func needsRefresh(acc *Account) bool {
	if currentToken(acc) == "" {
		return true
	}
	if acc.ExpiresAt == nil {
		return false
	}
	return time.Now().UTC().Add(refreshSkew).After(acc.ExpiresAt.UTC())
}

func currentToken(acc *Account) string {
	if acc.AccessToken != nil && *acc.AccessToken != "" {
		return *acc.AccessToken
	}
	if isAPIKeyMethod(acc.AuthMethod) && acc.APIKey != nil {
		return *acc.APIKey
	}
	return ""
}

func normalizeAuthMethod(method string) string {
	method = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(method), "-", "_"))
	if method == "apikey" {
		return "api_key"
	}
	return method
}

func isAPIKeyMethod(method string) bool { return normalizeAuthMethod(method) == "api_key" }
func isOpusModel(model string) bool     { return strings.Contains(strings.ToLower(model), "opus") }

type localAPIKeyRefresher struct{}

func (localAPIKeyRefresher) Refresh(ctx context.Context, acc *Account) (string, time.Time, error) {
	_ = ctx
	if acc == nil || acc.APIKey == nil || strings.TrimSpace(*acc.APIKey) == "" {
		return "", time.Time{}, errs.New(errs.ClassFatal, "INVALID_API_KEY", "invalid API key")
	}
	return *acc.APIKey, time.Now().UTC().Add(100 * 365 * 24 * time.Hour), nil
}
