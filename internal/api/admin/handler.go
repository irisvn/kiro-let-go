package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/irisvn/kiro-let-go/internal/account"
	"github.com/irisvn/kiro-let-go/internal/antiban"
	"github.com/irisvn/kiro-let-go/internal/kiro"
	"github.com/irisvn/kiro-let-go/internal/server/middleware"
)

type accountStore interface {
	Create(ctx context.Context, acc *account.Account) error
	Get(ctx context.Context, id string) (*account.Account, error)
	List(ctx context.Context, filter account.ListFilter) ([]account.Account, error)
	Update(ctx context.Context, acc *account.Account) error
	Delete(ctx context.Context, id string) error
	SetEnabled(ctx context.Context, id string, enabled bool, reason *string) error
}

type refreshManager interface {
	Refresh(ctx context.Context, accountID string) error
}

type quotaFetcher interface {
	Get(ctx context.Context, acc *account.Account, force bool) (*account.Quota, error)
	Summary(ctx context.Context) ([]*account.QuotaSummaryItem, error)
}

type circuitSnapshotter interface {
	Snapshot() map[string]account.CircuitInfo
}

type Handler struct {
	store    accountStore
	manager  refreshManager
	quota    quotaFetcher
	circuit  circuitSnapshotter
	dispatch *kiro.Dispatcher
	quotaTTL time.Duration

	modelsMu    sync.Mutex
	modelsCache map[string]cachedModels
}

type createAccountRequest struct {
	Label         string  `json:"label"`
	AuthMethod    string  `json:"auth_method"`
	RefreshToken  *string `json:"refresh_token"`
	APIKey        *string `json:"api_key"`
	ProfileARN    *string `json:"profile_arn"`
	Region        string  `json:"region"`
	AuthRegion    *string `json:"auth_region"`
	APIRegion     *string `json:"api_region"`
	ProxyURL      *string `json:"proxy_url"`
	ProxyUsername *string `json:"proxy_username"`
	ProxyPassword *string `json:"proxy_password"`
	Enabled       *bool   `json:"enabled"`
}

type accountResponse struct {
	ID             string     `json:"id"`
	Label          string     `json:"label"`
	AuthMethod     string     `json:"auth_method"`
	AccessToken    *string    `json:"access_token"`
	RefreshToken   *string    `json:"refresh_token"`
	APIKey         *string    `json:"api_key"`
	ExpiresAt      *time.Time `json:"expires_at"`
	ProfileARN     *string    `json:"profile_arn"`
	Region         string     `json:"region"`
	AuthRegion     *string    `json:"auth_region"`
	APIRegion      *string    `json:"api_region"`
	MachineID      string     `json:"machine_id"`
	ProxyURL       *string    `json:"proxy_url"`
	ProxyUsername  *string    `json:"proxy_username"`
	ProxyPassword  *string    `json:"proxy_password"`
	Enabled        bool       `json:"enabled"`
	DisabledReason *string    `json:"disabled_reason"`
	FailureCount   int        `json:"failure_count"`
	LastFailureAt  *time.Time `json:"last_failure_at"`
	SuccessCount   int        `json:"success_count"`
	LastUsedAt     *time.Time `json:"last_used_at"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type circuitBreakerResponse struct {
	Open       bool   `json:"open"`
	State      string `json:"state"`
	Failures   int    `json:"failures"`
	LastReason string `json:"last_reason"`
}

type getAccountResponse struct {
	Account        accountResponse        `json:"account"`
	CircuitBreaker circuitBreakerResponse `json:"circuit_breaker"`
}

type createAccountResponse struct {
	Account           accountResponse `json:"account"`
	Verified          bool            `json:"verified"`
	VerificationError string          `json:"verification_error,omitempty"`
}

type AvailableModel struct {
	ModelID             string       `json:"model_id"`
	ModelName           string       `json:"model_name"`
	Description         string       `json:"description"`
	RateMultiplier      float64      `json:"rate_multiplier"`
	RateUnit            string       `json:"rate_unit"`
	SupportedInputTypes []string     `json:"supported_input_types"`
	IsDefault           bool         `json:"is_default"`
	ContextWindow       *int64       `json:"context_window,omitempty"`
	TokenLimits         *TokenLimits `json:"token_limits,omitempty"`
}

type TokenLimits struct {
	MaxInputTokens  int64 `json:"max_input_tokens"`
	MaxOutputTokens int64 `json:"max_output_tokens"`
}

type ListModelsResponse struct {
	Models       []AvailableModel `json:"models"`
	DefaultModel *AvailableModel  `json:"default_model,omitempty"`
	Cached       bool             `json:"cached,omitempty"`
}

type TestAccountResponse struct {
	Status            string `json:"status"`
	Message           string `json:"message"`
	SubscriptionTitle string `json:"subscription_title,omitempty"`
	UserID            string `json:"user_id,omitempty"`
	DurationMs        int64  `json:"duration_ms"`
}

type chatTestRequest struct {
	Model   string `json:"model"`
	Message string `json:"message"`
}

type chatTestResponse struct {
	Success    bool   `json:"success"`
	Model      string `json:"model"`
	Message    string `json:"message"`
	Response   string `json:"response"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

type cachedModels struct {
	response  ListModelsResponse
	fetchedAt time.Time
}

type quotaResponse struct {
	SubscriptionTitle string          `json:"subscription_title"`
	LimitTotal        int64           `json:"limit_total"`
	LimitRemaining    int64           `json:"limit_remaining"`
	CurrentUsage      int64           `json:"current_usage"`
	OverageCap        int64           `json:"overage_cap"`
	OverageRate       float64         `json:"overage_rate"`
	Currency          string          `json:"currency"`
	ResourceType      string          `json:"resource_type"`
	ResetTime         *time.Time      `json:"reset_time"`
	Raw               json.RawMessage `json:"raw"`
	FetchedAt         *time.Time      `json:"fetched_at"`
}

type quotaSummaryResponse struct {
	AccountID         string     `json:"account_id"`
	Label             string     `json:"label"`
	SubscriptionTitle *string    `json:"subscription_title"`
	LimitTotal        *int64     `json:"limit_total"`
	LimitRemaining    *int64     `json:"limit_remaining"`
	CurrentUsage      *int64     `json:"current_usage"`
	OverageCap        *int64     `json:"overage_cap"`
	OverageRate       *float64   `json:"overage_rate"`
	Currency          *string    `json:"currency"`
	ResetTime         *time.Time `json:"reset_time"`
	FetchedAt         *time.Time `json:"fetched_at"`
	Stale             bool       `json:"stale"`
}

type errorResponse struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

var (
	createAccountFields = allowedFields(
		"label", "auth_method", "refresh_token", "api_key", "profile_arn", "region",
		"auth_region", "api_region", "proxy_url", "proxy_username", "proxy_password", "enabled",
	)
	patchAccountFields = allowedFields(
		"label", "enabled", "disabled_reason", "proxy_url", "proxy_username", "proxy_password", "region", "auth_region", "api_region",
	)
	chatTestFields = allowedFields("model", "message")
)

func NewHandler(store accountStore, manager refreshManager, quota quotaFetcher, circuit circuitSnapshotter, quotaTTL time.Duration, dispatcher ...*kiro.Dispatcher) *Handler {
	var dispatch *kiro.Dispatcher
	if len(dispatcher) > 0 {
		dispatch = dispatcher[0]
	}
	return &Handler{store: store, manager: manager, quota: quota, circuit: circuit, dispatch: dispatch, quotaTTL: quotaTTL, modelsCache: make(map[string]cachedModels)}
}

func RegisterRoutes(r gin.IRouter, adminAPIKey string, h *Handler) {
	adminGroup := r.Group("/admin", middleware.AdminAuthMiddleware(adminAPIKey))
	adminGroup.POST("/accounts", h.createAccount)
	adminGroup.GET("/accounts", h.listAccounts)
	adminGroup.GET("/accounts/:id", h.getAccount)
	adminGroup.PATCH("/accounts/:id", h.patchAccount)
	adminGroup.DELETE("/accounts/:id", h.deleteAccount)
	adminGroup.POST("/accounts/:id/refresh", h.refreshAccount)
	adminGroup.GET("/accounts/:id/quota", h.getAccountQuota)
	adminGroup.GET("/accounts/:id/models", h.getAccountModels)
	adminGroup.POST("/accounts/:id/test", h.testAccount)
	adminGroup.POST("/accounts/:id/chat-test", h.chatTestAccount)
	adminGroup.GET("/quota", h.getQuotaSummary)
}

func (h *Handler) createAccount(c *gin.Context) {
	if h.store == nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "account store is not configured")
		return
	}

	var req createAccountRequest
	_, err := decodeJSONObject(c, createAccountFields, &req)
	if err != nil {
		writeError(c, http.StatusBadRequest, "validation_error", err.Error())
		return
	}

	label := strings.TrimSpace(req.Label)
	if label == "" {
		writeError(c, http.StatusBadRequest, "validation_error", "label is required")
		return
	}

	method, externalMethod, err := normalizeAuthMethod(req.AuthMethod)
	if err != nil {
		writeError(c, http.StatusBadRequest, "validation_error", err.Error())
		return
	}

	var refreshToken *string
	var apiKey *string
	if method == "social" {
		refreshToken = normalizeNullableString(req.RefreshToken)
		if refreshToken == nil {
			writeError(c, http.StatusBadRequest, "validation_error", "refresh_token is required for social auth")
			return
		}
	} else {
		apiKey = normalizeNullableString(req.APIKey)
		if apiKey == nil {
			writeError(c, http.StatusBadRequest, "validation_error", "api_key is required for apikey auth")
			return
		}
		if !strings.HasPrefix(*apiKey, "ksk_") {
			writeError(c, http.StatusBadRequest, "validation_error", "api_key must start with ksk_")
			return
		}
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	id := uuid.NewString()
	seed := label
	if seed == "" {
		seed = id
	}
	acc := &account.Account{
		ID:            id,
		Label:         label,
		AuthMethod:    method,
		RefreshToken:  refreshToken,
		APIKey:        apiKey,
		ProfileARN:    normalizeNullableString(req.ProfileARN),
		Region:        strings.TrimSpace(req.Region),
		AuthRegion:    normalizeNullableString(req.AuthRegion),
		APIRegion:     normalizeNullableString(req.APIRegion),
		MachineID:     kiro.Generate(seed),
		ProxyURL:      normalizeNullableString(req.ProxyURL),
		ProxyUsername: normalizeNullableString(req.ProxyUsername),
		ProxyPassword: normalizeNullableString(req.ProxyPassword),
		Enabled:       enabled,
	}

	if err := h.store.Create(c.Request.Context(), acc); err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", fmt.Sprintf("create account: %v", err))
		return
	}

	if method == "social" {
		if h.manager == nil {
			writeError(c, http.StatusInternalServerError, "internal_error", "account manager is not configured")
			return
		}
		if err := h.manager.Refresh(c.Request.Context(), acc.ID); err != nil {
			reason := fmt.Sprintf("refresh failed: %v", err)
			_ = h.store.SetEnabled(c.Request.Context(), acc.ID, false, &reason)
			created, getErr := h.store.Get(c.Request.Context(), acc.ID)
			if getErr != nil {
				writeError(c, http.StatusInternalServerError, "internal_error", fmt.Sprintf("load created account: %v", getErr))
				return
			}
			created.AuthMethod = method
			c.JSON(http.StatusCreated, createAccountResponse{
				Account:           toAccountResponse(created, externalMethod),
				Verified:          false,
				VerificationError: reason,
			})
			return
		}
	}

	created, err := h.store.Get(c.Request.Context(), acc.ID)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", fmt.Sprintf("load created account: %v", err))
		return
	}
	created.AuthMethod = method
	c.JSON(http.StatusCreated, createAccountResponse{
		Account:  toAccountResponse(created, externalMethod),
		Verified: true,
	})
}

func (h *Handler) listAccounts(c *gin.Context) {
	if h.store == nil {
		c.JSON(http.StatusOK, []accountResponse{})
		return
	}

	enabledOnly, err := parseOptionalBool(c.Query("enabled_only"), false)
	if err != nil {
		writeError(c, http.StatusBadRequest, "validation_error", "enabled_only must be true or false")
		return
	}

	filterMethod := ""
	if rawMethod := c.Query("auth_method"); rawMethod != "" {
		_, externalMethod, err := normalizeAuthMethod(rawMethod)
		if err != nil {
			writeError(c, http.StatusBadRequest, "validation_error", err.Error())
			return
		}
		filterMethod = externalMethod
	}

	accounts, err := h.store.List(c.Request.Context(), account.ListFilter{EnabledOnly: enabledOnly})
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", fmt.Sprintf("list accounts: %v", err))
		return
	}

	resp := make([]accountResponse, 0, len(accounts))
	for i := range accounts {
		externalMethod := externalAuthMethod(accounts[i].AuthMethod)
		if filterMethod != "" && externalMethod != filterMethod {
			continue
		}
		resp = append(resp, toAccountResponse(&accounts[i], externalMethod))
	}
	c.JSON(http.StatusOK, resp)
}

func (h *Handler) getAccount(c *gin.Context) {
	acc, ok := h.loadAccount(c)
	if !ok {
		return
	}

	info := circuitBreakerResponse{Open: false, State: "closed", Failures: acc.FailureCount}
	if h.circuit != nil {
		if snapshot, found := h.circuit.Snapshot()[acc.ID]; found {
			info.Open = snapshot.Open
			info.State = "closed"
			if snapshot.Open {
				info.State = "open"
			}
			info.Failures = snapshot.Failures
			info.LastReason = snapshot.LastReason
		}
	}

	c.JSON(http.StatusOK, getAccountResponse{
		Account:        toAccountResponse(acc, externalAuthMethod(acc.AuthMethod)),
		CircuitBreaker: info,
	})
}

func (h *Handler) patchAccount(c *gin.Context) {
	acc, ok := h.loadAccount(c)
	if !ok {
		return
	}

	raw, err := decodeJSONObject(c, patchAccountFields, nil)
	if err != nil {
		writeError(c, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	if len(raw) == 0 {
		writeError(c, http.StatusBadRequest, "validation_error", "at least one field must be provided")
		return
	}

	if value, present, err := requiredStringField(raw, "label"); err != nil {
		writeError(c, http.StatusBadRequest, "validation_error", err.Error())
		return
	} else if present {
		acc.Label = strings.TrimSpace(value)
		if acc.Label == "" {
			writeError(c, http.StatusBadRequest, "validation_error", "label cannot be empty")
			return
		}
	}

	if value, present, err := requiredBoolField(raw, "enabled"); err != nil {
		writeError(c, http.StatusBadRequest, "validation_error", err.Error())
		return
	} else if present {
		acc.Enabled = value
	}

	if value, present, err := nullableStringField(raw, "disabled_reason"); err != nil {
		writeError(c, http.StatusBadRequest, "validation_error", err.Error())
		return
	} else if present {
		acc.DisabledReason = value
	}

	if value, present, err := nullableStringField(raw, "proxy_url"); err != nil {
		writeError(c, http.StatusBadRequest, "validation_error", err.Error())
		return
	} else if present {
		acc.ProxyURL = value
	}

	if value, present, err := nullableStringField(raw, "proxy_username"); err != nil {
		writeError(c, http.StatusBadRequest, "validation_error", err.Error())
		return
	} else if present {
		acc.ProxyUsername = value
	}

	if value, present, err := nullableStringField(raw, "proxy_password"); err != nil {
		writeError(c, http.StatusBadRequest, "validation_error", err.Error())
		return
	} else if present {
		acc.ProxyPassword = value
	}

	if value, present, err := nullableStringField(raw, "region"); err != nil {
		writeError(c, http.StatusBadRequest, "validation_error", err.Error())
		return
	} else if present {
		acc.Region = nullableStringValue(value)
	}

	if value, present, err := nullableStringField(raw, "auth_region"); err != nil {
		writeError(c, http.StatusBadRequest, "validation_error", err.Error())
		return
	} else if present {
		acc.AuthRegion = value
	}

	if value, present, err := nullableStringField(raw, "api_region"); err != nil {
		writeError(c, http.StatusBadRequest, "validation_error", err.Error())
		return
	} else if present {
		acc.APIRegion = value
	}

	if err := h.store.Update(c.Request.Context(), acc); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, account.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeError(c, status, errorTypeForStatus(status), fmt.Sprintf("update account: %v", err))
		return
	}

	updated, err := h.store.Get(c.Request.Context(), acc.ID)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", fmt.Sprintf("load updated account: %v", err))
		return
	}
	c.JSON(http.StatusOK, toAccountResponse(updated, externalAuthMethod(updated.AuthMethod)))
}

func (h *Handler) deleteAccount(c *gin.Context) {
	if h.store == nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "account store is not configured")
		return
	}

	if err := h.store.Delete(c.Request.Context(), c.Param("id")); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, account.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeError(c, status, errorTypeForStatus(status), fmt.Sprintf("delete account: %v", err))
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) refreshAccount(c *gin.Context) {
	if h.manager == nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "account manager is not configured")
		return
	}
	if h.store == nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "account store is not configured")
		return
	}

	id := c.Param("id")
	if err := h.manager.Refresh(c.Request.Context(), id); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, account.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeError(c, status, errorTypeForStatus(status), fmt.Sprintf("refresh account: %v", err))
		return
	}

	acc, err := h.store.Get(c.Request.Context(), id)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, account.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeError(c, status, errorTypeForStatus(status), fmt.Sprintf("load refreshed account: %v", err))
		return
	}
	c.JSON(http.StatusOK, toAccountResponse(acc, externalAuthMethod(acc.AuthMethod)))
}

func (h *Handler) getAccountQuota(c *gin.Context) {
	if h.quota == nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "quota fetcher is not configured")
		return
	}

	acc, ok := h.loadAccount(c)
	if !ok {
		return
	}

	force, err := parseOptionalBool(c.Query("force"), false)
	if err != nil {
		writeError(c, http.StatusBadRequest, "validation_error", "force must be true or false")
		return
	}

	quota, err := h.quota.Get(c.Request.Context(), acc, force)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", fmt.Sprintf("get quota: %v", err))
		return
	}
	c.JSON(http.StatusOK, toQuotaResponse(quota))
}

func (h *Handler) getAccountModels(c *gin.Context) {
	acc, ok := h.loadAccount(c)
	if !ok {
		return
	}

	if cached, ok := h.getCachedModels(acc.ID); ok {
		cached.Cached = true
		c.JSON(http.StatusOK, cached)
		return
	}

	acc, ok = h.ensureFreshToken(c, acc)
	if !ok {
		return
	}

	models, status, kind, err := h.fetchAccountModels(c.Request.Context(), acc)
	if err != nil {
		writeError(c, status, kind, err.Error())
		return
	}
	h.setCachedModels(acc.ID, *models)
	c.JSON(http.StatusOK, models)
}

func (h *Handler) testAccount(c *gin.Context) {
	acc, ok := h.loadAccount(c)
	if !ok {
		return
	}
	if !acc.Enabled {
		writeError(c, http.StatusBadRequest, "validation_error", "account is disabled")
		return
	}
	acc, ok = h.ensureFreshToken(c, acc)
	if !ok {
		return
	}

	started := time.Now()
	resp := h.fetchUsageLimits(c.Request.Context(), acc, started)
	durationMs := time.Since(started).Milliseconds()
	resp.DurationMs = durationMs
	c.JSON(http.StatusOK, resp)
}

func (h *Handler) chatTestAccount(c *gin.Context) {
	acc, ok := h.loadAccount(c)
	if !ok {
		return
	}
	if !acc.Enabled {
		writeError(c, http.StatusBadRequest, "validation_error", "account is disabled")
		return
	}

	var req chatTestRequest
	if _, err := decodeJSONObject(c, chatTestFields, &req); err != nil {
		writeError(c, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	model := strings.TrimSpace(req.Model)
	if model == "" {
		writeError(c, http.StatusBadRequest, "validation_error", "model is required")
		return
	}
	model = kiro.MapModel(model)
	message := strings.TrimSpace(req.Message)
	if message == "" {
		message = "Hi"
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	acc, ok = h.ensureFreshTokenWithContext(c, ctx, acc)
	if !ok {
		return
	}

	started := time.Now()
	text, status, kind, err := h.sendChatTest(ctx, acc, model, message)
	durationMs := time.Since(started).Milliseconds()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			writeError(c, http.StatusGatewayTimeout, "timeout_error", "chat test timed out after 30 seconds")
			return
		}
		writeError(c, status, kind, err.Error())
		return
	}
	c.JSON(http.StatusOK, chatTestResponse{Success: true, Model: model, Message: message, Response: text, DurationMs: durationMs})
}

func (h *Handler) getCachedModels(accountID string) (ListModelsResponse, bool) {
	if h == nil {
		return ListModelsResponse{}, false
	}
	h.modelsMu.Lock()
	defer h.modelsMu.Unlock()
	if h.modelsCache == nil {
		h.modelsCache = make(map[string]cachedModels)
		return ListModelsResponse{}, false
	}
	entry, ok := h.modelsCache[accountID]
	if !ok || time.Since(entry.fetchedAt) >= 30*time.Minute {
		if ok {
			delete(h.modelsCache, accountID)
		}
		return ListModelsResponse{}, false
	}
	return entry.response, true
}

func (h *Handler) setCachedModels(accountID string, resp ListModelsResponse) {
	if h == nil {
		return
	}
	h.modelsMu.Lock()
	defer h.modelsMu.Unlock()
	if h.modelsCache == nil {
		h.modelsCache = make(map[string]cachedModels)
	}
	resp.Cached = false
	h.modelsCache[accountID] = cachedModels{response: resp, fetchedAt: time.Now().UTC()}
}

func (h *Handler) ensureFreshToken(c *gin.Context, acc *account.Account) (*account.Account, bool) {
	return h.ensureFreshTokenWithContext(c, c.Request.Context(), acc)
}

func (h *Handler) ensureFreshTokenWithContext(c *gin.Context, ctx context.Context, acc *account.Account) (*account.Account, bool) {
	if acc == nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "account is nil")
		return nil, false
	}
	if !accountNeedsRefresh(acc) {
		return acc, true
	}
	if h.manager == nil {
		writeError(c, http.StatusUnauthorized, "authentication_error", "account token is missing or expired and refresh manager is not configured")
		return nil, false
	}
	if err := h.manager.Refresh(ctx, acc.ID); err != nil {
		status := http.StatusUnauthorized
		if errors.Is(err, account.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeError(c, status, errorTypeForStatus(status), fmt.Sprintf("refresh account: %v", err))
		return nil, false
	}
	refreshed, err := h.store.Get(ctx, acc.ID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, account.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeError(c, status, errorTypeForStatus(status), fmt.Sprintf("load refreshed account: %v", err))
		return nil, false
	}
	return refreshed, true
}

func (h *Handler) sendChatTest(ctx context.Context, acc *account.Account, model, message string) (string, int, string, error) {
	payload := &kiro.KiroPayload{
		ConversationState: kiro.ConversationState{
			ConversationID:      uuid.NewString(),
			AgentContinuationID: uuid.NewString(),
			AgentTaskType:       "vibe",
			ChatTriggerType:     "MANUAL",
			CurrentMessage: kiro.CurrentMessage{
				UserInputMessage: kiro.UserInputMessage{
					Content: message,
					ModelID: model,
					Origin:  "AI_EDITOR",
					UserInputMessageContext: &kiro.UserInputMessageContext{
						Tools: []kiro.Tool{},
					},
				},
			},
			History: []kiro.HistoryItem{},
		},
	}
	if acc.ProfileARN != nil && strings.TrimSpace(*acc.ProfileARN) != "" {
		payload.ProfileArn = strings.TrimSpace(*acc.ProfileARN)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", http.StatusInternalServerError, "internal_error", fmt.Errorf("marshal chat test payload: %w", err)
	}
	region := accountAPIRegion(acc)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://q."+region+".amazonaws.com/generateAssistantResponse", bytes.NewReader(body))
	if err != nil {
		return "", http.StatusInternalServerError, "internal_error", fmt.Errorf("build chat test request: %w", err)
	}
	shadow := *acc
	token := accountBearerToken(acc)
	if strings.EqualFold(acc.AuthMethod, "apikey") || strings.EqualFold(acc.AuthMethod, "api_key") {
		shadow.APIKey = &token
	} else {
		shadow.AccessToken = &token
	}
	req.Header = antiban.BuildKiroRequestHeaders(&shadow, region)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", http.StatusGatewayTimeout, "timeout_error", errors.New("chat test timed out after 30 seconds")
		}
		return "", http.StatusBadGateway, "upstream_error", fmt.Errorf("generate assistant response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		respBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return "", http.StatusBadGateway, "upstream_error", fmt.Errorf("read Kiro error response: %w", readErr)
		}
		status, kind, msg := classifyKiroHTTPError(resp.StatusCode, respBody)
		return "", status, kind, fmt.Errorf("generate assistant response: %s", msg)
	}

	decoder := kiro.NewStreamDecoder(nil)
	events := decoder.Decode(ctx, resp.Body, body)
	var text strings.Builder
	for event := range events {
		switch e := event.(type) {
		case kiro.TextDelta:
			text.WriteString(e.Text)
		case kiro.ErrorEvent:
			return "", http.StatusBadGateway, "upstream_error", fmt.Errorf("decode Kiro stream: %w", e.Err)
		}
	}
	if err := ctx.Err(); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return "", http.StatusGatewayTimeout, "timeout_error", errors.New("chat test timed out after 30 seconds")
		}
		return "", http.StatusBadGateway, "upstream_error", fmt.Errorf("chat test canceled: %w", err)
	}
	return text.String(), http.StatusOK, "", nil
}

func accountNeedsRefresh(acc *account.Account) bool {
	if acc.AuthMethod == "api_key" {
		return acc.APIKey == nil || strings.TrimSpace(*acc.APIKey) == ""
	}
	if acc.AccessToken == nil || strings.TrimSpace(*acc.AccessToken) == "" {
		return true
	}
	return acc.ExpiresAt != nil && !acc.ExpiresAt.After(time.Now().Add(time.Minute))
}

func (h *Handler) fetchAccountModels(ctx context.Context, acc *account.Account) (*ListModelsResponse, int, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://q."+accountAPIRegion(acc)+".amazonaws.com/ListAvailableModels?origin=AI_EDITOR&maxResults=50", nil)
	if err != nil {
		return nil, http.StatusInternalServerError, "internal_error", fmt.Errorf("build models request: %w", err)
	}
	setMinimalKiroHeaders(req, accountBearerToken(acc))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, http.StatusBadGateway, "upstream_error", fmt.Errorf("list available models: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, http.StatusInternalServerError, "internal_error", fmt.Errorf("read models response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		status, kind, msg := classifyKiroHTTPError(resp.StatusCode, body)
		return nil, status, kind, fmt.Errorf("list available models: %s", msg)
	}

	var raw struct {
		Models       []kiroAvailableModel `json:"models"`
		DefaultModel *kiroAvailableModel  `json:"defaultModel"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, http.StatusBadGateway, "upstream_error", fmt.Errorf("parse models response: %w", err)
	}
	out := &ListModelsResponse{Models: make([]AvailableModel, 0, len(raw.Models))}
	for _, model := range raw.Models {
		out.Models = append(out.Models, mapAvailableModel(model))
	}
	if raw.DefaultModel != nil {
		defaultModel := mapAvailableModel(*raw.DefaultModel)
		out.DefaultModel = &defaultModel
	}
	return out, http.StatusOK, "", nil
}

type kiroAvailableModel struct {
	ModelID             string           `json:"modelId"`
	ModelName           string           `json:"modelName"`
	Description         string           `json:"description"`
	RateMultiplier      float64          `json:"rateMultiplier"`
	RateUnit            string           `json:"rateUnit"`
	SupportedInputTypes []string         `json:"supportedInputTypes"`
	IsDefault           bool             `json:"isDefault"`
	ContextWindow       *int64           `json:"contextWindow"`
	TokenLimits         *kiroTokenLimits `json:"tokenLimits"`
}

type kiroTokenLimits struct {
	MaxInputTokens  int64 `json:"maxInputTokens"`
	MaxOutputTokens int64 `json:"maxOutputTokens"`
}

func mapAvailableModel(model kiroAvailableModel) AvailableModel {
	out := AvailableModel{
		ModelID:             model.ModelID,
		ModelName:           model.ModelName,
		Description:         model.Description,
		RateMultiplier:      model.RateMultiplier,
		RateUnit:            model.RateUnit,
		SupportedInputTypes: append([]string(nil), model.SupportedInputTypes...),
		IsDefault:           model.IsDefault,
		ContextWindow:       model.ContextWindow,
	}
	if model.TokenLimits != nil {
		out.TokenLimits = &TokenLimits{MaxInputTokens: model.TokenLimits.MaxInputTokens, MaxOutputTokens: model.TokenLimits.MaxOutputTokens}
	}
	return out
}

func (h *Handler) fetchUsageLimits(ctx context.Context, acc *account.Account, started time.Time) TestAccountResponse {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://q."+accountAPIRegion(acc)+".amazonaws.com/getUsageLimits?origin=AI_EDITOR&resourceType=AGENTIC_REQUEST", nil)
	if err != nil {
		return TestAccountResponse{Status: "error", Message: fmt.Sprintf("build usage limits request: %v", err)}
	}
	setMinimalKiroHeaders(req, accountBearerToken(acc))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return TestAccountResponse{Status: "error", Message: fmt.Sprintf("get usage limits: %v", err)}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return TestAccountResponse{Status: "error", Message: fmt.Sprintf("read usage limits response: %v", err)}
	}

	result := classifyUsageLimitsResponse(resp.StatusCode, body)
	result.DurationMs = time.Since(started).Milliseconds()
	return result
}

func classifyUsageLimitsResponse(status int, body []byte) TestAccountResponse {
	switch {
	case status == http.StatusOK:
		var raw struct {
			SubscriptionInfo struct {
				SubscriptionTitle string `json:"subscriptionTitle"`
			} `json:"subscriptionInfo"`
			UserInfo struct {
				UserID string `json:"userId"`
			} `json:"userInfo"`
		}
		if err := json.Unmarshal(body, &raw); err != nil {
			return TestAccountResponse{Status: "error", Message: fmt.Sprintf("parse usage limits response: %v", err)}
		}
		return TestAccountResponse{Status: "valid", Message: "Account is valid", SubscriptionTitle: raw.SubscriptionInfo.SubscriptionTitle, UserID: raw.UserInfo.UserID}
	case status == http.StatusUnauthorized:
		return TestAccountResponse{Status: "token_expired", Message: "Token is invalid or expired"}
	case status == http.StatusForbidden && strings.Contains(string(body), "TemporarilySuspended"):
		return TestAccountResponse{Status: "banned", Message: "Account is temporarily suspended/banned"}
	case status == http.StatusLocked:
		return TestAccountResponse{Status: "suspended", Message: "Account is suspended"}
	default:
		return TestAccountResponse{Status: "error", Message: fmt.Sprintf("Kiro returned status %d: %s", status, strings.TrimSpace(string(body)))}
	}
}

func setMinimalKiroHeaders(req *http.Request, token string) {
	req.Header = http.Header{
		"Authorization":               {"Bearer " + token},
		"Content-Type":                {"application/json"},
		"Accept":                      {"application/json"},
		"Connection":                  {"close"},
		"x-amzn-kiro-agent-mode":      {"vibe"},
		"x-amzn-codewhisperer-optout": {"true"},
	}
}

func accountBearerToken(acc *account.Account) string {
	if acc == nil {
		return ""
	}
	if acc.AccessToken != nil && strings.TrimSpace(*acc.AccessToken) != "" {
		return strings.TrimSpace(*acc.AccessToken)
	}
	if acc.APIKey != nil {
		return strings.TrimSpace(*acc.APIKey)
	}
	return ""
}

func accountAPIRegion(acc *account.Account) string {
	region := ""
	if acc != nil {
		region = strings.TrimSpace(acc.Region)
		if acc.APIRegion != nil && strings.TrimSpace(*acc.APIRegion) != "" {
			region = strings.TrimSpace(*acc.APIRegion)
		}
	}
	if region == "" {
		region = "us-east-1"
	}
	return region
}

func classifyKiroHTTPError(status int, body []byte) (int, string, string) {
	message := strings.TrimSpace(string(body))
	if message == "" {
		message = http.StatusText(status)
	}
	switch {
	case status == http.StatusUnauthorized:
		return http.StatusUnauthorized, "authentication_error", "token is invalid or expired"
	case status == http.StatusForbidden && strings.Contains(string(body), "TemporarilySuspended"):
		return http.StatusForbidden, "banned_error", "account is temporarily suspended/banned"
	case status == http.StatusLocked:
		return http.StatusLocked, "suspended_error", "account is suspended"
	default:
		return http.StatusBadGateway, "upstream_error", fmt.Sprintf("Kiro returned status %d: %s", status, message)
	}
}

func (h *Handler) getQuotaSummary(c *gin.Context) {
	if h.quota == nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "quota fetcher is not configured")
		return
	}

	items, err := h.quota.Summary(c.Request.Context())
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", fmt.Sprintf("list quota summary: %v", err))
		return
	}

	resp := make([]quotaSummaryResponse, 0, len(items))
	for _, item := range items {
		entry := quotaSummaryResponse{AccountID: item.AccountID, Label: item.Label, Stale: true}
		if item.Quota != nil {
			entry.SubscriptionTitle = stringPtr(item.Quota.SubscriptionTitle)
			entry.LimitTotal = int64Ptr(item.Quota.LimitTotal)
			entry.LimitRemaining = int64Ptr(item.Quota.LimitRemaining)
			entry.CurrentUsage = int64Ptr(item.Quota.CurrentUsage)
			entry.OverageCap = int64Ptr(item.Quota.OverageCap)
			entry.OverageRate = float64Ptr(item.Quota.OverageRate)
			entry.Currency = stringPtr(item.Quota.Currency)
			if !item.Quota.ResetTime.IsZero() {
				entry.ResetTime = timePtr(item.Quota.ResetTime)
			}
			entry.FetchedAt = timePtr(item.Quota.FetchedAt)
			entry.Stale = h.quotaTTL > 0 && time.Since(item.Quota.FetchedAt.UTC()) > h.quotaTTL
			if h.quotaTTL <= 0 {
				entry.Stale = false
			}
		}
		resp = append(resp, entry)
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) loadAccount(c *gin.Context) (*account.Account, bool) {
	if h.store == nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "account store is not configured")
		return nil, false
	}

	acc, err := h.store.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, account.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeError(c, status, errorTypeForStatus(status), fmt.Sprintf("get account: %v", err))
		return nil, false
	}
	return acc, true
}

func decodeJSONObject(c *gin.Context, allowed map[string]struct{}, out any) (map[string]json.RawMessage, error) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return nil, fmt.Errorf("read request body: %w", err)
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return nil, errors.New("request body is required")
	}

	raw := make(map[string]json.RawMessage)
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("invalid JSON body: %w", err)
	}
	for field := range raw {
		if _, ok := allowed[field]; !ok {
			return nil, fmt.Errorf("field %s is not allowed", field)
		}
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return nil, fmt.Errorf("invalid request body: %w", err)
		}
	}
	return raw, nil
}

func requiredStringField(raw map[string]json.RawMessage, field string) (string, bool, error) {
	value, ok := raw[field]
	if !ok {
		return "", false, nil
	}
	if bytes.Equal(value, []byte("null")) {
		return "", true, fmt.Errorf("%s cannot be null", field)
	}
	var s string
	if err := json.Unmarshal(value, &s); err != nil {
		return "", true, fmt.Errorf("%s must be a string", field)
	}
	return s, true, nil
}

func requiredBoolField(raw map[string]json.RawMessage, field string) (bool, bool, error) {
	value, ok := raw[field]
	if !ok {
		return false, false, nil
	}
	if bytes.Equal(value, []byte("null")) {
		return false, true, fmt.Errorf("%s cannot be null", field)
	}
	var b bool
	if err := json.Unmarshal(value, &b); err != nil {
		return false, true, fmt.Errorf("%s must be a boolean", field)
	}
	return b, true, nil
}

func nullableStringField(raw map[string]json.RawMessage, field string) (*string, bool, error) {
	value, ok := raw[field]
	if !ok {
		return nil, false, nil
	}
	if bytes.Equal(value, []byte("null")) {
		return nil, true, nil
	}
	var s string
	if err := json.Unmarshal(value, &s); err != nil {
		return nil, true, fmt.Errorf("%s must be a string or null", field)
	}
	normalized := strings.TrimSpace(s)
	if normalized == "" {
		return nil, true, nil
	}
	return &normalized, true, nil
}

func normalizeNullableString(value *string) *string {
	if value == nil {
		return nil
	}
	normalized := strings.TrimSpace(*value)
	if normalized == "" {
		return nil
	}
	return &normalized
}

func normalizeAuthMethod(value string) (internal string, external string, err error) {
	switch strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "-", "_")) {
	case "social", "oauth":
		return "social", "social", nil
	case "apikey", "api_key":
		return "api_key", "apikey", nil
	default:
		return "", "", errors.New("auth_method must be one of: social, apikey")
	}
}

func externalAuthMethod(value string) string {
	_, external, err := normalizeAuthMethod(value)
	if err != nil {
		return "social"
	}
	return external
}

func parseOptionalBool(raw string, defaultValue bool) (bool, error) {
	if strings.TrimSpace(raw) == "" {
		return defaultValue, nil
	}
	return strconv.ParseBool(raw)
}

func toAccountResponse(acc *account.Account, externalMethod string) accountResponse {
	return accountResponse{
		ID:             acc.ID,
		Label:          acc.Label,
		AuthMethod:     externalMethod,
		AccessToken:    acc.AccessToken,
		RefreshToken:   acc.RefreshToken,
		APIKey:         acc.APIKey,
		ExpiresAt:      acc.ExpiresAt,
		ProfileARN:     acc.ProfileARN,
		Region:         acc.Region,
		AuthRegion:     acc.AuthRegion,
		APIRegion:      acc.APIRegion,
		MachineID:      acc.MachineID,
		ProxyURL:       acc.ProxyURL,
		ProxyUsername:  acc.ProxyUsername,
		ProxyPassword:  nil,
		Enabled:        acc.Enabled,
		DisabledReason: acc.DisabledReason,
		FailureCount:   acc.FailureCount,
		LastFailureAt:  acc.LastFailureAt,
		SuccessCount:   acc.SuccessCount,
		LastUsedAt:     acc.LastUsedAt,
		CreatedAt:      acc.CreatedAt,
		UpdatedAt:      acc.UpdatedAt,
	}
}

func toQuotaResponse(quota *account.Quota) quotaResponse {
	return quotaResponse{
		SubscriptionTitle: quota.SubscriptionTitle,
		LimitTotal:        quota.LimitTotal,
		LimitRemaining:    quota.LimitRemaining,
		CurrentUsage:      quota.CurrentUsage,
		OverageCap:        quota.OverageCap,
		OverageRate:       quota.OverageRate,
		Currency:          quota.Currency,
		ResourceType:      quota.ResourceType,
		ResetTime:         timePtr(quota.ResetTime),
		Raw:               append(json.RawMessage(nil), quota.Raw...),
		FetchedAt:         timePtr(quota.FetchedAt),
	}
}

func nullableStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func allowedFields(fields ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		out[field] = struct{}{}
	}
	return out
}

func writeError(c *gin.Context, status int, kind, message string) {
	var resp errorResponse
	resp.Error.Type = kind
	resp.Error.Message = message
	c.AbortWithStatusJSON(status, resp)
}

func errorTypeForStatus(status int) string {
	if status == http.StatusNotFound {
		return "not_found_error"
	}
	return "internal_error"
}

func stringPtr(value string) *string {
	return &value
}

func int64Ptr(value int64) *int64 {
	return &value
}

func float64Ptr(value float64) *float64 {
	return &value
}

func timePtr(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	v := value.UTC()
	return &v
}
