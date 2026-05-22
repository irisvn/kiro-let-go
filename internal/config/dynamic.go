package config

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	settingsKeyStrategy                 = "strategy"
	settingsKeyStickySession            = "sticky_session"
	settingsKeyBaseCooldownSec          = "base_cooldown_sec"
	settingsKeyMaxBackoffMultiplier     = "max_backoff_multiplier"
	settingsKeyProbabilisticRetryChance = "probabilistic_retry_chance"
	settingsKeyMaxAttempts              = "max_attempts"
	settingsKeyCacheTTLSeconds          = "cache_ttl_seconds"
	settingsKeyModelMappings            = "model_mappings"
	settingsKeyRequestLogEnabled        = "request_log_enabled"

	// New Ported settings from kiro-gateway
	settingsKeyWebSearchEnabled          = "web_search_enabled"
	settingsKeyFirstTokenTimeoutSec      = "first_token_timeout_sec"
	settingsKeyFirstTokenMaxRetries      = "first_token_max_retries"
	settingsKeyStreamingReadTimeoutSec   = "streaming_read_timeout_sec"
	settingsKeyTruncationRecoveryEnabled = "truncation_recovery_enabled"
	settingsKeyFakeReasoningEnabled      = "fake_reasoning_enabled"
	settingsKeyFakeReasoningMaxTokens    = "fake_reasoning_max_tokens"
	settingsKeyFakeReasoningBudgetCap    = "fake_reasoning_budget_cap"
)

// DynamicConfig stores runtime-editable settings loaded from SQLite.
type DynamicConfig struct {
	mu sync.RWMutex
	db *sql.DB

	// Load Balancer
	Strategy      string
	StickySession bool

	// Failover
	BaseCooldownSec          int
	MaxBackoffMultiplier     int
	ProbabilisticRetryChance float64
	MaxAttempts              int

	// Quota
	CacheTTLSeconds int

	// Model Mappings
	ModelMappings []ModelMappingRule

	// New Ported settings from kiro-gateway
	WebSearchEnabled          bool
	FirstTokenTimeoutSec      int
	FirstTokenMaxRetries      int
	StreamingReadTimeoutSec   int
	TruncationRecoveryEnabled bool
	FakeReasoningEnabled      bool
	FakeReasoningMaxTokens    int
	FakeReasoningBudgetCap    int

	// Request Logging
	RequestLogEnabled bool

	// Listeners (notified on change)
	listeners []func()
}

// DynamicSettings is the JSON/API representation of runtime-editable settings.
type DynamicSettings struct {
	Strategy                 string             `json:"strategy"`
	StickySession            bool               `json:"sticky_session"`
	BaseCooldownSec          int                `json:"base_cooldown_sec"`
	MaxBackoffMultiplier     int                `json:"max_backoff_multiplier"`
	ProbabilisticRetryChance float64            `json:"probabilistic_retry_chance"`
	MaxAttempts              int                `json:"max_attempts"`
	CacheTTLSeconds          int                `json:"cache_ttl_seconds"`
	ModelMappings            []ModelMappingRule `json:"model_mappings"`

	// New Ported settings from kiro-gateway
	WebSearchEnabled          bool `json:"web_search_enabled"`
	FirstTokenTimeoutSec      int  `json:"first_token_timeout_sec"`
	FirstTokenMaxRetries      int  `json:"first_token_max_retries"`
	StreamingReadTimeoutSec   int  `json:"streaming_read_timeout_sec"`
	TruncationRecoveryEnabled bool `json:"truncation_recovery_enabled"`
	FakeReasoningEnabled      bool `json:"fake_reasoning_enabled"`
	FakeReasoningMaxTokens    int  `json:"fake_reasoning_max_tokens"`
	FakeReasoningBudgetCap    int  `json:"fake_reasoning_budget_cap"`

	// Request Logging
	RequestLogEnabled bool `json:"request_log_enabled"`
}

// NewDynamicConfig creates an empty dynamic config backed by db.
func NewDynamicConfig(db *sql.DB) *DynamicConfig {
	return &DynamicConfig{db: db}
}

// Load reads all dynamic settings from SQLite.
func (dc *DynamicConfig) Load() error {
	if dc == nil || dc.db == nil {
		return fmt.Errorf("dynamic config db is nil")
	}

	rows, err := dc.db.QueryContext(context.Background(), "SELECT key, value FROM settings")
	if err != nil {
		return fmt.Errorf("load settings: %w", err)
	}
	defer rows.Close()

	values := map[string]string{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return fmt.Errorf("scan setting: %w", err)
		}
		values[key] = value
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate settings: %w", err)
	}
	if len(values) == 0 {
		return nil
	}

	settings, err := settingsFromRows(values)
	if err != nil {
		return err
	}
	dc.apply(settings)
	return nil
}

// IsEmpty reports whether no settings are currently loaded.
func (dc *DynamicConfig) IsEmpty() bool {
	if dc == nil {
		return true
	}
	dc.mu.RLock()
	defer dc.mu.RUnlock()
	return strings.TrimSpace(dc.Strategy) == "" && dc.BaseCooldownSec == 0 && dc.MaxAttempts == 0 && dc.CacheTTLSeconds == 0 && len(dc.ModelMappings) == 0
}

// SeedFromStatic persists and applies the initial dynamic settings from JSON config.
func (dc *DynamicConfig) SeedFromStatic(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("static config is nil")
	}
	settings := DynamicSettings{
		Strategy:                 cfg.LoadBalancer.Strategy,
		StickySession:            cfg.LoadBalancer.StickySession,
		BaseCooldownSec:          cfg.Failover.BaseCooldownSec,
		MaxBackoffMultiplier:     cfg.Failover.MaxBackoffMultiplier,
		ProbabilisticRetryChance: cfg.Failover.ProbabilisticRetryChance,
		MaxAttempts:              cfg.Failover.MaxAttempts,
		CacheTTLSeconds:          cfg.Quota.CacheTTLSeconds,
		ModelMappings:            copyModelMappings(cfg.ModelMappings),

		WebSearchEnabled:          false,
		FirstTokenTimeoutSec:      15,
		FirstTokenMaxRetries:      3,
		StreamingReadTimeoutSec:   300,
		TruncationRecoveryEnabled: true,
		FakeReasoningEnabled:      true,
		FakeReasoningMaxTokens:    1024,
		FakeReasoningBudgetCap:    0,
		RequestLogEnabled:         true,
	}
	return dc.Update(settings)
}

// Get returns a thread-safe copy of dynamic settings.
func (dc *DynamicConfig) Get() DynamicSettings {
	if dc == nil {
		return DynamicSettings{}
	}
	dc.mu.RLock()
	defer dc.mu.RUnlock()
	return DynamicSettings{
		Strategy:                 dc.Strategy,
		StickySession:            dc.StickySession,
		BaseCooldownSec:          dc.BaseCooldownSec,
		MaxBackoffMultiplier:     dc.MaxBackoffMultiplier,
		ProbabilisticRetryChance: dc.ProbabilisticRetryChance,
		MaxAttempts:              dc.MaxAttempts,
		CacheTTLSeconds:          dc.CacheTTLSeconds,
		ModelMappings:            copyModelMappings(dc.ModelMappings),

		WebSearchEnabled:          dc.WebSearchEnabled,
		FirstTokenTimeoutSec:      dc.FirstTokenTimeoutSec,
		FirstTokenMaxRetries:      dc.FirstTokenMaxRetries,
		StreamingReadTimeoutSec:   dc.StreamingReadTimeoutSec,
		TruncationRecoveryEnabled: dc.TruncationRecoveryEnabled,
		FakeReasoningEnabled:      dc.FakeReasoningEnabled,
		FakeReasoningMaxTokens:    dc.FakeReasoningMaxTokens,
		FakeReasoningBudgetCap:    dc.FakeReasoningBudgetCap,
		RequestLogEnabled:         dc.RequestLogEnabled,
	}
}

// Update validates, writes settings to SQLite, applies them, and notifies listeners.
func (dc *DynamicConfig) Update(settings DynamicSettings) error {
	if dc == nil || dc.db == nil {
		return fmt.Errorf("dynamic config db is nil")
	}
	settings = normalizeDynamicSettings(settings)
	if err := ValidateDynamicSettings(settings); err != nil {
		return err
	}
	rows, err := rowsFromSettings(settings)
	if err != nil {
		return err
	}

	tx, err := dc.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("begin settings update: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for key, value := range rows {
		if _, err := tx.ExecContext(context.Background(), "INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at", key, value, now); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("write setting %s: %w", key, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit settings update: %w", err)
	}

	dc.apply(settings)
	dc.notify()
	return nil
}

// OnChange registers a listener called after successful updates.
func (dc *DynamicConfig) OnChange(fn func()) {
	if dc == nil || fn == nil {
		return
	}
	dc.mu.Lock()
	defer dc.mu.Unlock()
	dc.listeners = append(dc.listeners, fn)
}

// ValidateDynamicSettings validates runtime-editable settings.
func ValidateDynamicSettings(settings DynamicSettings) error {
	switch settings.Strategy {
	case "round_robin", "balanced", "most_quota":
	default:
		return fmt.Errorf("strategy must be one of round_robin, balanced, most_quota")
	}
	if settings.BaseCooldownSec <= 0 {
		return fmt.Errorf("base_cooldown_sec must be positive")
	}
	if settings.MaxBackoffMultiplier <= 0 {
		return fmt.Errorf("max_backoff_multiplier must be positive")
	}
	if settings.ProbabilisticRetryChance < 0 || settings.ProbabilisticRetryChance > 1 {
		return fmt.Errorf("probabilistic_retry_chance must be between 0 and 1")
	}
	if settings.MaxAttempts <= 0 {
		return fmt.Errorf("max_attempts must be positive")
	}
	if settings.CacheTTLSeconds <= 0 {
		return fmt.Errorf("cache_ttl_seconds must be positive")
	}
	for i, rule := range settings.ModelMappings {
		if strings.TrimSpace(rule.RuleType) == "" {
			return fmt.Errorf("model_mappings[%d].rule_type is required", i)
		}
		if len(rule.TargetModels) == 0 {
			return fmt.Errorf("model_mappings[%d].target_models is required", i)
		}
		if strings.EqualFold(strings.TrimSpace(rule.RuleType), "loadbalance") && len(rule.Weights) > 0 && len(rule.Weights) != len(rule.TargetModels) {
			return fmt.Errorf("model_mappings[%d].weights must match target_models length", i)
		}
	}
	return nil
}

func (dc *DynamicConfig) apply(settings DynamicSettings) {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	dc.Strategy = settings.Strategy
	dc.StickySession = settings.StickySession
	dc.BaseCooldownSec = settings.BaseCooldownSec
	dc.MaxBackoffMultiplier = settings.MaxBackoffMultiplier
	dc.ProbabilisticRetryChance = settings.ProbabilisticRetryChance
	dc.MaxAttempts = settings.MaxAttempts
	dc.CacheTTLSeconds = settings.CacheTTLSeconds
	dc.ModelMappings = copyModelMappings(settings.ModelMappings)

	dc.WebSearchEnabled = settings.WebSearchEnabled
	dc.FirstTokenTimeoutSec = settings.FirstTokenTimeoutSec
	dc.FirstTokenMaxRetries = settings.FirstTokenMaxRetries
	dc.StreamingReadTimeoutSec = settings.StreamingReadTimeoutSec
	dc.TruncationRecoveryEnabled = settings.TruncationRecoveryEnabled
	dc.FakeReasoningEnabled = settings.FakeReasoningEnabled
	dc.FakeReasoningMaxTokens = settings.FakeReasoningMaxTokens
	dc.FakeReasoningBudgetCap = settings.FakeReasoningBudgetCap
	dc.RequestLogEnabled = settings.RequestLogEnabled
}

func (dc *DynamicConfig) notify() {
	dc.mu.RLock()
	listeners := append([]func(){}, dc.listeners...)
	dc.mu.RUnlock()
	for _, fn := range listeners {
		fn()
	}
}

func settingsFromRows(values map[string]string) (DynamicSettings, error) {
	settings := DynamicSettings{Strategy: strings.TrimSpace(values[settingsKeyStrategy])}
	
	settings.StickySession = parseBoolWithDefault(values[settingsKeyStickySession], true)
	settings.BaseCooldownSec = parseIntWithDefault(values[settingsKeyBaseCooldownSec], 60)
	settings.MaxBackoffMultiplier = parseIntWithDefault(values[settingsKeyMaxBackoffMultiplier], 1440)
	settings.ProbabilisticRetryChance = parseFloatWithDefault(values[settingsKeyProbabilisticRetryChance], 0.1)
	settings.MaxAttempts = parseIntWithDefault(values[settingsKeyMaxAttempts], 9)
	settings.CacheTTLSeconds = parseIntWithDefault(values[settingsKeyCacheTTLSeconds], 43200)

	settings.WebSearchEnabled = parseBoolWithDefault(values[settingsKeyWebSearchEnabled], false)
	settings.FirstTokenTimeoutSec = parseIntWithDefault(values[settingsKeyFirstTokenTimeoutSec], 15)
	settings.FirstTokenMaxRetries = parseIntWithDefault(values[settingsKeyFirstTokenMaxRetries], 3)
	settings.StreamingReadTimeoutSec = parseIntWithDefault(values[settingsKeyStreamingReadTimeoutSec], 300)
	settings.TruncationRecoveryEnabled = parseBoolWithDefault(values[settingsKeyTruncationRecoveryEnabled], true)
	settings.FakeReasoningEnabled = parseBoolWithDefault(values[settingsKeyFakeReasoningEnabled], true)
	settings.FakeReasoningMaxTokens = parseIntWithDefault(values[settingsKeyFakeReasoningMaxTokens], 1024)
	settings.FakeReasoningBudgetCap = parseIntWithDefault(values[settingsKeyFakeReasoningBudgetCap], 0)
	settings.RequestLogEnabled = parseBoolWithDefault(values[settingsKeyRequestLogEnabled], true)

	if raw := strings.TrimSpace(values[settingsKeyModelMappings]); raw != "" {
		if err := json.Unmarshal([]byte(raw), &settings.ModelMappings); err != nil {
			return settings, fmt.Errorf("parse model_mappings: %w", err)
		}
	}
	settings = normalizeDynamicSettings(settings)
	return settings, ValidateDynamicSettings(settings)
}

func rowsFromSettings(settings DynamicSettings) (map[string]string, error) {
	mappings, err := json.Marshal(settings.ModelMappings)
	if err != nil {
		return nil, fmt.Errorf("marshal model_mappings: %w", err)
	}
	return map[string]string{
		settingsKeyStrategy:                 settings.Strategy,
		settingsKeyStickySession:            strconv.FormatBool(settings.StickySession),
		settingsKeyBaseCooldownSec:          strconv.Itoa(settings.BaseCooldownSec),
		settingsKeyMaxBackoffMultiplier:     strconv.Itoa(settings.MaxBackoffMultiplier),
		settingsKeyProbabilisticRetryChance: strconv.FormatFloat(settings.ProbabilisticRetryChance, 'f', -1, 64),
		settingsKeyMaxAttempts:              strconv.Itoa(settings.MaxAttempts),
		settingsKeyCacheTTLSeconds:          strconv.Itoa(settings.CacheTTLSeconds),
		settingsKeyModelMappings:            string(mappings),

		settingsKeyWebSearchEnabled:          strconv.FormatBool(settings.WebSearchEnabled),
		settingsKeyFirstTokenTimeoutSec:      strconv.Itoa(settings.FirstTokenTimeoutSec),
		settingsKeyFirstTokenMaxRetries:      strconv.Itoa(settings.FirstTokenMaxRetries),
		settingsKeyStreamingReadTimeoutSec:   strconv.Itoa(settings.StreamingReadTimeoutSec),
		settingsKeyTruncationRecoveryEnabled: strconv.FormatBool(settings.TruncationRecoveryEnabled),
		settingsKeyFakeReasoningEnabled:      strconv.FormatBool(settings.FakeReasoningEnabled),
		settingsKeyFakeReasoningMaxTokens:    strconv.Itoa(settings.FakeReasoningMaxTokens),
		settingsKeyFakeReasoningBudgetCap:    strconv.Itoa(settings.FakeReasoningBudgetCap),
		settingsKeyRequestLogEnabled:         strconv.FormatBool(settings.RequestLogEnabled),
	}, nil
}

func normalizeDynamicSettings(settings DynamicSettings) DynamicSettings {
	settings.Strategy = strings.TrimSpace(strings.ToLower(settings.Strategy))
	settings.ModelMappings = copyModelMappings(settings.ModelMappings)
	return settings
}

func copyModelMappings(in []ModelMappingRule) []ModelMappingRule {
	if len(in) == 0 {
		return nil
	}
	out := make([]ModelMappingRule, len(in))
	for i, rule := range in {
		out[i] = rule
		out[i].TargetModels = append([]string(nil), rule.TargetModels...)
		out[i].Weights = append([]int(nil), rule.Weights...)
	}
	return out
}

func parseBoolWithDefault(val string, def bool) bool {
	if val == "" {
		return def
	}
	b, err := strconv.ParseBool(val)
	if err != nil {
		return def
	}
	return b
}

func parseIntWithDefault(val string, def int) int {
	if val == "" {
		return def
	}
	i, err := strconv.Atoi(val)
	if err != nil {
		return def
	}
	return i
}

func parseFloatWithDefault(val string, def float64) float64 {
	if val == "" {
		return def
	}
	f, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return def
	}
	return f
}
