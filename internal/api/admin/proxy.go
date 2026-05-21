package admin

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/irisvn/kiro-let-go/internal/account"
	"github.com/irisvn/kiro-let-go/internal/config"
	"github.com/irisvn/kiro-let-go/internal/converter"
	"github.com/irisvn/kiro-let-go/internal/kiro"
)

type RequestLogEntry struct {
	ID              string            `json:"id"`
	RequestID       string            `json:"request_id,omitempty"`
	Timestamp       time.Time         `json:"timestamp"`
	Method          string            `json:"method"`
	Path            string            `json:"path"`
	Status          int               `json:"status"`
	DurationMs      int64             `json:"duration_ms"`
	ClientIP        string            `json:"client_ip"`
	AccountID       string            `json:"account_id,omitempty"`
	AccountLabel    string            `json:"account_label,omitempty"`
	Model           string            `json:"model,omitempty"`
	InputTokens     int               `json:"input_tokens,omitempty"`
	OutputTokens    int               `json:"output_tokens,omitempty"`
	Stream          bool              `json:"stream"`
	RequestBody     string            `json:"request_body,omitempty"`
	KiroPayload     string            `json:"kiro_payload,omitempty"`
	ResponseSnippet string            `json:"response_snippet,omitempty"`
	Error           string            `json:"error,omitempty"`
	Format          string            `json:"format,omitempty"`
	UserAgent       string            `json:"user_agent,omitempty"`
	Attempts        []kiro.AttemptLog `json:"attempts,omitempty"`
}

type requestLogReader interface {
	Entries() []RequestLogEntry
}

type accountAcquirer interface {
	Acquire(ctx context.Context, hint account.SelectionHint) (*account.Acquisition, error)
}

type proxyConfigResponse struct {
	Host                     string         `json:"host"`
	Port                     int            `json:"port"`
	LoadBalancerStrategy     string         `json:"load_balancer_strategy"`
	StickySession            bool           `json:"sticky_session"`
	MaxAttempts              int            `json:"max_attempts"`
	BaseCooldownSec          int            `json:"base_cooldown_sec"`
	MaxBackoffMultiplier     int            `json:"max_backoff_multiplier"`
	ProbabilisticRetryChance float64        `json:"probabilistic_retry_chance"`
	QuotaCacheTTLSeconds     int            `json:"quota_cache_ttl_seconds"`
	CredentialsJSONPath      string         `json:"credentials_json_path"`
	TotalAccounts            int            `json:"total_accounts"`
	EnabledAccounts          int            `json:"enabled_accounts"`
	Endpoints                []endpointInfo `json:"endpoints"`
}

type endpointInfo struct {
	Format      string `json:"format"`
	Method      string `json:"method"`
	Path        string `json:"path"`
	Description string `json:"description"`
}

type roundRobinTestRequest struct {
	Count int    `json:"count"`
	Model string `json:"model"`
}

type roundRobinTestResponse struct {
	Results []roundRobinTestResult `json:"results"`
	Summary map[string]int         `json:"summary"`
}

type roundRobinTestResult struct {
	Attempt      int    `json:"attempt"`
	AccountID    string `json:"account_id"`
	AccountLabel string `json:"account_label"`
	Success      bool   `json:"success"`
	DurationMs   int64  `json:"duration_ms"`
	Error        string `json:"error,omitempty"`
}

func (h *Handler) SetProxyDependencies(cfg *config.Config, log requestLogReader, acquirer accountAcquirer) {
	h.cfg = cfg
	h.requestLog = log
	h.acquirer = acquirer
}

func (h *Handler) getProxyConfig(c *gin.Context) {
	if h.cfg == nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "proxy config is not configured")
		return
	}

	accounts, err := h.store.List(c.Request.Context(), account.ListFilter{})
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	enabled := 0
	for _, acc := range accounts {
		if acc.Enabled {
			enabled++
		}
	}

	dyn := config.DynamicSettings{}
	if h.dynamicCfg != nil {
		dyn = h.dynamicCfg.Get()
	}
	if dyn.Strategy == "" {
		dyn = config.DynamicSettings{
			Strategy:                 h.cfg.LoadBalancer.Strategy,
			StickySession:            h.cfg.LoadBalancer.StickySession,
			MaxAttempts:              h.cfg.Failover.MaxAttempts,
			BaseCooldownSec:          h.cfg.Failover.BaseCooldownSec,
			MaxBackoffMultiplier:     h.cfg.Failover.MaxBackoffMultiplier,
			ProbabilisticRetryChance: h.cfg.Failover.ProbabilisticRetryChance,
			CacheTTLSeconds:          h.cfg.Quota.CacheTTLSeconds,
		}
	}

	c.JSON(http.StatusOK, proxyConfigResponse{
		Host:                     h.cfg.Server.Host,
		Port:                     h.cfg.Server.Port,
		LoadBalancerStrategy:     dyn.Strategy,
		StickySession:            dyn.StickySession,
		MaxAttempts:              dyn.MaxAttempts,
		BaseCooldownSec:          dyn.BaseCooldownSec,
		MaxBackoffMultiplier:     dyn.MaxBackoffMultiplier,
		ProbabilisticRetryChance: dyn.ProbabilisticRetryChance,
		QuotaCacheTTLSeconds:     dyn.CacheTTLSeconds,
		CredentialsJSONPath:      h.cfg.Storage.CredentialsJSONPath,
		TotalAccounts:            len(accounts),
		EnabledAccounts:          enabled,
		Endpoints: []endpointInfo{
			{Format: "anthropic", Method: http.MethodPost, Path: "/v1/messages", Description: "Anthropic Messages API (streaming + non-streaming)"},
			{Format: "anthropic", Method: http.MethodPost, Path: "/v1/messages/count_tokens", Description: "Anthropic token counting"},
			{Format: "openai", Method: http.MethodPost, Path: "/v1/chat/completions", Description: "OpenAI Chat Completions (streaming + non-streaming)"},
			{Format: "openai", Method: http.MethodGet, Path: "/v1/models", Description: "OpenAI Models list"},
		},
	})
}

func (h *Handler) getProxyLog(c *gin.Context) {
	if h.requestLog == nil {
		c.JSON(http.StatusOK, []RequestLogEntry{})
		return
	}
	limit := 50
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			writeError(c, http.StatusBadRequest, "validation_error", "limit must be a positive integer")
			return
		}
		limit = parsed
	}
	if limit > 100 {
		limit = 100
	}
	entries := h.requestLog.Entries()
	if len(entries) > limit {
		entries = entries[:limit]
	}
	
	// Truncate large fields for API response to keep UI lightweight
	const apiResponseLimit = 2000
	for i := range entries {
		entries[i].RequestBody = truncateForAPI(entries[i].RequestBody, apiResponseLimit)
		entries[i].ResponseSnippet = truncateForAPI(entries[i].ResponseSnippet, apiResponseLimit)
		entries[i].KiroPayload = truncateForAPI(entries[i].KiroPayload, apiResponseLimit)
	}
	
	c.JSON(http.StatusOK, entries)
}

func truncateForAPI(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "…"
}

type apiTestRequest struct {
	Format  string `json:"format"` // "anthropic" or "openai"
	Model   string `json:"model"`
	Message string `json:"message"`
}

type apiTestResponse struct {
	Success      bool   `json:"success"`
	Format       string `json:"format"`
	Model        string `json:"model"`
	Response     string `json:"response"`
	DurationMs   int64  `json:"duration_ms"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	AccountID    string `json:"account_id,omitempty"`
	AccountLabel string `json:"account_label,omitempty"`
	Error        string `json:"error,omitempty"`
}

func (h *Handler) testProxyAPI(c *gin.Context) {
	if h.dispatch == nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "dispatcher is not configured")
		return
	}

	var req apiTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, http.StatusBadRequest, "validation_error", err.Error())
		return
	}

	format := strings.TrimSpace(strings.ToLower(req.Format))
	if format == "" {
		format = "anthropic"
	}
	if format != "anthropic" && format != "openai" {
		writeError(c, http.StatusBadRequest, "validation_error", "format must be 'anthropic' or 'openai'")
		return
	}

	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = "claude-haiku-4.5"
	}
	message := strings.TrimSpace(req.Message)
	if message == "" {
		message = "Hi"
	}

	normalized := &converter.NormalizedRequest{
		Model:           model,
		Messages:        []converter.NormalizedMessage{{Role: "user", Parts: []converter.NormalizedPart{converter.Text{Text: message}}}},
		MaxOutputTokens: 100,
		Stream:          false,
	}

	payload, err := converter.NormalizedToKiro(normalized, "")
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "build kiro payload: "+err.Error())
		return
	}
	setRequestLogKiroPayload(c, payload)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	started := time.Now()
	result, err := h.dispatch.Once(ctx, payload, account.SelectionHint{Model: model})
	durationMs := time.Since(started).Milliseconds()
	setRequestLogAccount(c, result.AccountID, result.AccountLabel)
	setRequestLogUsage(c, result.Usage)

	if err != nil {
		c.JSON(http.StatusOK, apiTestResponse{
			Success:    false,
			Format:     format,
			Model:      model,
			DurationMs: durationMs,
			Error:      err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, apiTestResponse{
		Success:      true,
		Format:       format,
		Model:        model,
		Response:     result.Text,
		DurationMs:   durationMs,
		InputTokens:  result.Usage.InputTokens,
		OutputTokens: result.Usage.OutputTokens,
		AccountID:    result.AccountID,
		AccountLabel: result.AccountLabel,
	})
}

func (h *Handler) testRoundRobin(c *gin.Context) {
	if h.acquirer == nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "account manager is not configured")
		return
	}

	req := roundRobinTestRequest{Count: 5, Model: "claude-haiku-4.5"}
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			writeError(c, http.StatusBadRequest, "validation_error", err.Error())
			return
		}
	}
	if req.Count <= 0 {
		req.Count = 5
	}
	if req.Count > 20 {
		req.Count = 20
	}
	if strings.TrimSpace(req.Model) == "" {
		req.Model = "claude-haiku-4.5"
	}

	results := make([]roundRobinTestResult, 0, req.Count)
	summary := make(map[string]int)
	for i := 1; i <= req.Count; i++ {
		started := time.Now()
		result := roundRobinTestResult{Attempt: i}
		acq, err := h.acquirer.Acquire(c.Request.Context(), account.SelectionHint{Model: req.Model})
		result.DurationMs = time.Since(started).Milliseconds()
		if err != nil {
			result.Error = err.Error()
			results = append(results, result)
			continue
		}
		if acq != nil && acq.Account != nil {
			result.AccountID = acq.Account.ID
			result.AccountLabel = acq.Account.Label
		}
		result.Success = true
		if acq != nil && acq.ReleaseSuccess != nil {
			acq.ReleaseSuccess()
		}
		label := result.AccountLabel
		if label == "" {
			label = result.AccountID
		}
		if label != "" {
			summary[label]++
		}
		results = append(results, result)
	}
	c.JSON(http.StatusOK, roundRobinTestResponse{Results: results, Summary: summary})
}
