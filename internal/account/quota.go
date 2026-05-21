package account

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/irisvn/kiro-let-go/internal/config"
	"github.com/irisvn/kiro-let-go/internal/errs"
)

// Quota is the normalized Kiro quota snapshot cached per account.
type Quota struct {
	SubscriptionTitle string
	LimitTotal        int64
	LimitRemaining    int64
	CurrentUsage      int64
	OverageCap        int64
	OverageRate       float64
	Currency          string
	ResourceType      string
	ResetTime         time.Time
	Raw               json.RawMessage
	FetchedAt         time.Time
}

// Fetcher fetches quota snapshots on explicit demand and caches them in Store.
type Fetcher struct {
	httpClient *http.Client
	store      *Store
	ttl        time.Duration
	dc         *config.DynamicConfig
	logger     *slog.Logger
}

// QuotaSummaryItem is the cached quota summary for one account.
type QuotaSummaryItem struct {
	AccountID string
	Label     string
	Enabled   bool
	Quota     *Quota
}

func (f *Fetcher) SetDynamicConfig(dc *config.DynamicConfig) {
	if f != nil {
		f.dc = dc
	}
}

// NewFetcher creates a quota fetcher. It does not start background work.
func NewFetcher(httpClient *http.Client, store *Store, ttl time.Duration, logger *slog.Logger) *Fetcher {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Fetcher{httpClient: httpClient, store: store, ttl: ttl, logger: logger}
}

// Get returns a cached quota when fresh unless force asks for an upstream refresh.
func (f *Fetcher) Get(ctx context.Context, acc *Account, force bool) (*Quota, error) {
	if f == nil || f.store == nil {
		return nil, fmt.Errorf("quota fetcher store is nil")
	}
	if acc == nil {
		return nil, fmt.Errorf("quota account is nil")
	}

	if !force {
		quota, err := f.cached(ctx, acc.ID)
		if err == nil && f.isFresh(quota) {
			return quota, nil
		}
		if err != nil && !errors.Is(err, ErrNotFound) {
			return nil, err
		}
	}

	quota, err := f.fetch(ctx, acc)
	if err != nil {
		return nil, err
	}
	if err := f.store.UpsertQuota(ctx, &QuotaCache{AccountID: acc.ID, PayloadJSON: string(quota.Raw), FetchedAt: quota.FetchedAt}); err != nil {
		return nil, err
	}
	return quota, nil
}

// Summary returns one item per account with cached quota only; it never refreshes upstream.
func (f *Fetcher) Summary(ctx context.Context) ([]*QuotaSummaryItem, error) {
	if f == nil || f.store == nil {
		return nil, fmt.Errorf("quota fetcher store is nil")
	}
	accounts, err := f.store.List(ctx, ListFilter{})
	if err != nil {
		return nil, err
	}
	items := make([]*QuotaSummaryItem, 0, len(accounts))
	for i := range accounts {
		acc := accounts[i]
		item := &QuotaSummaryItem{AccountID: acc.ID, Label: acc.Label, Enabled: acc.Enabled}
		quota, err := f.cached(ctx, acc.ID)
		if err == nil {
			item.Quota = quota
		} else if !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (f *Fetcher) cached(ctx context.Context, accountID string) (*Quota, error) {
	qc, err := f.store.GetQuota(ctx, accountID)
	if err != nil {
		return nil, err
	}
	return parseQuota([]byte(qc.PayloadJSON), qc.FetchedAt)
}

func (f *Fetcher) isFresh(quota *Quota) bool {
	ttl := f.ttl
	if f.dc != nil {
		if seconds := f.dc.Get().CacheTTLSeconds; seconds > 0 {
			ttl = time.Duration(seconds) * time.Second
		}
	}
	if quota == nil || ttl <= 0 {
		return false
	}
	return time.Since(quota.FetchedAt) < ttl
}

func (f *Fetcher) fetch(ctx context.Context, acc *Account) (*Quota, error) {
	region := acc.Region
	if acc.APIRegion != nil && strings.TrimSpace(*acc.APIRegion) != "" {
		region = strings.TrimSpace(*acc.APIRegion)
	}
	if region == "" {
		region = "us-east-1"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://q."+region+".amazonaws.com/getUsageLimits", nil)
	if err != nil {
		return nil, fmt.Errorf("build quota request: %w", err)
	}
	// Use minimal headers for quota endpoint. KiroIDE User-Agent triggers
	// profileArn requirement on this endpoint, which is unnecessary for quota queries.
	token := ""
	if acc.AccessToken != nil {
		token = *acc.AccessToken
	} else if acc.APIKey != nil {
		token = *acc.APIKey
	}
	req.Header = http.Header{
		"Authorization":               {"Bearer " + token},
		"Content-Type":                {"application/json"},
		"Connection":                  {"close"},
		"x-amzn-kiro-agent-mode":      {"vibe"},
		"x-amzn-codewhisperer-optout": {"true"},
	}

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, errs.Wrap(err, errs.ClassRecoverable, "fetch quota")
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read quota response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch quota: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return parseQuota(body, time.Now().UTC())
}

func parseQuota(body []byte, fetchedAt time.Time) (*Quota, error) {
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("parse quota json: %w", err)
	}

	q := &Quota{Raw: append(json.RawMessage(nil), body...), FetchedAt: fetchedAt.UTC()}
	q.SubscriptionTitle, _ = findString(decoded, "subscriptionTitle")

	if breakdown := findUsageBreakdown(decoded); breakdown != nil {
		if usage, ok := findFloat(breakdown, "currentUsage"); ok {
			q.CurrentUsage = int64(usage)
		}
		if limit, ok := findFloat(breakdown, "usageLimit"); ok {
			q.LimitTotal = int64(limit)
			q.LimitRemaining = max(q.LimitTotal-q.CurrentUsage, 0)
		}
		if cap, ok := findFloat(breakdown, "overageCap"); ok {
			q.OverageCap = int64(cap)
		}
		if rate, ok := findFloat(breakdown, "overageRate"); ok {
			q.OverageRate = rate
		}
		if currency, ok := findStringDirect(breakdown, "currency"); ok {
			q.Currency = currency
		}
		if resType, ok := findStringDirect(breakdown, "resourceType"); ok {
			q.ResourceType = resType
		}
		if reset, ok := findFloat(breakdown, "nextDateReset"); ok && reset > 0 {
			q.ResetTime = time.Unix(int64(reset), 0).UTC()
		}
	} else {
		count, hasCount := findInt(decoded, "monthlyRequestCount")
		limit, hasLimit := findInt(decoded, "monthlyRequestLimit")
		remaining, hasRemaining := findInt(decoded, "monthlyRequestRemaining")
		if hasLimit {
			q.LimitTotal = limit
		}
		if hasRemaining {
			q.LimitRemaining = remaining
		} else if hasLimit && hasCount {
			q.CurrentUsage = count
			q.LimitRemaining = max(limit-count, 0)
		}
		if reset, ok := findString(decoded, "resetTime"); ok {
			q.ResetTime = parseResetTime(reset)
		}
	}
	return q, nil
}

func findUsageBreakdown(v any) map[string]any {
	root, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	list, ok := root["usageBreakdownList"]
	if !ok {
		return nil
	}
	arr, ok := list.([]any)
	if !ok || len(arr) == 0 {
		return nil
	}
	first, ok := arr[0].(map[string]any)
	if !ok {
		return nil
	}
	return first
}

func findFloat(m map[string]any, key string) (float64, bool) {
	raw, ok := m[key]
	if !ok {
		return 0, false
	}
	n, ok := raw.(float64)
	return n, ok
}

func findStringDirect(m map[string]any, key string) (string, bool) {
	raw, ok := m[key]
	if !ok {
		return "", false
	}
	s, ok := raw.(string)
	return s, ok
}

func findString(v any, key string) (string, bool) {
	switch x := v.(type) {
	case map[string]any:
		if raw, ok := x[key]; ok {
			if s, ok := raw.(string); ok {
				return s, true
			}
		}
		for _, child := range x {
			if s, ok := findString(child, key); ok {
				return s, true
			}
		}
	case []any:
		for _, child := range x {
			if s, ok := findString(child, key); ok {
				return s, true
			}
		}
	}
	return "", false
}

func findInt(v any, key string) (int64, bool) {
	switch x := v.(type) {
	case map[string]any:
		if raw, ok := x[key]; ok {
			if n, ok := raw.(float64); ok {
				return int64(n), true
			}
		}
		for _, child := range x {
			if n, ok := findInt(child, key); ok {
				return n, true
			}
		}
	case []any:
		for _, child := range x {
			if n, ok := findInt(child, key); ok {
				return n, true
			}
		}
	}
	return 0, false
}

func parseResetTime(s string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}
