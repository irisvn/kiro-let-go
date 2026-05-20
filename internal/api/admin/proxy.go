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
)

type RequestLogEntry struct {
	ID           string    `json:"id"`
	Timestamp    time.Time `json:"timestamp"`
	Method       string    `json:"method"`
	Path         string    `json:"path"`
	Status       int       `json:"status"`
	DurationMs   int64     `json:"duration_ms"`
	ClientIP     string    `json:"client_ip"`
	AccountID    string    `json:"account_id,omitempty"`
	AccountLabel string    `json:"account_label,omitempty"`
	Model        string    `json:"model,omitempty"`
	Error        string    `json:"error,omitempty"`
	Format       string    `json:"format,omitempty"`
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

	c.JSON(http.StatusOK, proxyConfigResponse{
		Host:                     h.cfg.Server.Host,
		Port:                     h.cfg.Server.Port,
		LoadBalancerStrategy:     h.cfg.LoadBalancer.Strategy,
		StickySession:            h.cfg.LoadBalancer.StickySession,
		MaxAttempts:              h.cfg.Failover.MaxAttempts,
		BaseCooldownSec:          h.cfg.Failover.BaseCooldownSec,
		MaxBackoffMultiplier:     h.cfg.Failover.MaxBackoffMultiplier,
		ProbabilisticRetryChance: h.cfg.Failover.ProbabilisticRetryChance,
		QuotaCacheTTLSeconds:     h.cfg.Quota.CacheTTLSeconds,
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
	c.JSON(http.StatusOK, entries)
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
