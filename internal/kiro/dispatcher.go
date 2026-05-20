package kiro

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/irisvn/kiro-let-go/internal/account"
	"github.com/irisvn/kiro-let-go/internal/antiban"
	"github.com/irisvn/kiro-let-go/internal/errs"
)

const (
	defaultDispatcherMaxAttempts = 9
	defaultDispatcherBaseRetryMs = 100
	maxDispatcherBackoff         = 2 * time.Second
)

type Dispatcher struct {
	client  *Client
	manager *account.Manager
	cfg     DispatcherConfig
	logger  *slog.Logger
}

type DispatcherConfig struct {
	MaxAttempts int
	BaseRetryMs int
}

type FullResponse struct {
	Text         string
	Thinking     string
	ToolUses     []ToolUseEntry
	Usage        Usage
	ContextUsage *ContextUsage
	StopReason   string
}

func NewDispatcher(client *Client, manager *account.Manager, cfg DispatcherConfig, logger *slog.Logger) *Dispatcher {
	if logger == nil {
		logger = slog.Default()
	}
	return &Dispatcher{client: client, manager: manager, cfg: normalizeDispatcherConfig(cfg), logger: logger}
}

func (d *Dispatcher) Stream(ctx context.Context, payload *KiroPayload, hint account.SelectionHint) (<-chan StreamEvent, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if d == nil || d.client == nil || d.manager == nil {
		return nil, errs.New(errs.ClassFatal, "DISPATCHER_NOT_READY", "dispatcher is not configured")
	}
	cfg := normalizeDispatcherConfig(d.cfg)
	requestPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, errs.Wrap(err, errs.ClassFatal, "failed to marshal Kiro payload")
	}

	var lastErr error
	for attempt := 0; attempt < cfg.MaxAttempts; attempt++ {
		acq, err := d.manager.Acquire(ctx, hint)
		if err != nil {
			if errors.Is(err, account.NoAccountsAvailable) || errors.Is(err, account.ErrNoCandidates) {
				return nil, err
			}
			lastErr = classifyError(err)
			if !isRecoverable(lastErr) {
				return nil, lastErr
			}
			if err := dispatcherBackoff(ctx, cfg, attempt); err != nil {
				return nil, err
			}
			continue
		}
		if acq == nil || acq.Account == nil {
			lastErr = errs.New(errs.ClassRecoverable, "NIL_ACQUISITION", "account manager returned no account")
			if err := dispatcherBackoff(ctx, cfg, attempt); err != nil {
				return nil, err
			}
			continue
		}

		req, err := buildKiroRequest(acq.Account, requestPayload, acq.Token, acq.Region)
		if err != nil {
			releaseFailure(acq, "build_request_error")
			return nil, err
		}

		body, resp, err := d.client.Stream(ctx, acq.Account, req)
		if resp != nil && resp.StatusCode != http.StatusOK {
			if body != nil {
				_ = body.Close()
			}
			classified := classifyResponse(resp, err)
			lastErr = classified
			if isAuthStatus(resp.StatusCode) {
				if refreshErr := d.manager.Refresh(ctx, acq.Account.ID); refreshErr != nil {
					d.logger.Warn("failed to refresh account after auth failure", "account_id", acq.Account.ID, "error", refreshErr)
				}
			}
			if isRecoverable(classified) {
				releaseFailure(acq, failureReason(classified))
				hint.ExcludeIDs = appendExcluded(hint.ExcludeIDs, acq.Account.ID)
				if err := dispatcherBackoff(ctx, cfg, attempt); err != nil {
					return nil, err
				}
				continue
			}
			releaseFailure(acq, failureReason(classified))
			return nil, classified
		}
		if err != nil {
			if body != nil {
				_ = body.Close()
			}
			classified := classifyError(err)
			lastErr = classified
			if isRecoverable(classified) {
				releaseFailure(acq, failureReason(classified))
				hint.ExcludeIDs = appendExcluded(hint.ExcludeIDs, acq.Account.ID)
				if err := dispatcherBackoff(ctx, cfg, attempt); err != nil {
					return nil, err
				}
				continue
			}
			releaseFailure(acq, failureReason(classified))
			return nil, classified
		}
		if body == nil {
			lastErr = errs.New(errs.ClassRecoverable, "EMPTY_STREAM", "Kiro returned an empty stream body")
			releaseFailure(acq, "empty_stream")
			hint.ExcludeIDs = appendExcluded(hint.ExcludeIDs, acq.Account.ID)
			if err := dispatcherBackoff(ctx, cfg, attempt); err != nil {
				return nil, err
			}
			continue
		}

		return d.forwardStream(ctx, acq, body, requestPayload), nil
	}

	msg := "all Kiro accounts failed"
	if lastErr != nil {
		msg = fmt.Sprintf("%s: %v", msg, lastErr)
	}
	return nil, errs.New(errs.ClassFatal, "ALL_ACCOUNTS_FAILED", msg)
}

func (d *Dispatcher) Once(ctx context.Context, payload *KiroPayload, hint account.SelectionHint) (FullResponse, error) {
	events, err := d.Stream(ctx, payload, hint)
	if err != nil {
		return FullResponse{}, err
	}
	var full FullResponse
	toolIndex := map[string]int{}
	for event := range events {
		switch e := event.(type) {
		case TextDelta:
			full.Text += e.Text
		case ThinkingDelta:
			full.Thinking += e.Text
		case ToolUseStart:
			toolIndex[e.ID] = len(full.ToolUses)
			full.ToolUses = append(full.ToolUses, ToolUseEntry{ToolUseID: e.ID, Name: e.Name})
		case ToolUseDelta:
			idx, ok := toolIndex[e.ID]
			if !ok {
				idx = len(full.ToolUses)
				toolIndex[e.ID] = idx
				full.ToolUses = append(full.ToolUses, ToolUseEntry{ToolUseID: e.ID})
			}
			full.ToolUses[idx].Input += e.InputDelta
		case Usage:
			full.Usage = e
		case ContextUsage:
			ctxUsage := e
			full.ContextUsage = &ctxUsage
		case Stop:
			full.StopReason = e.Reason
		case ErrorEvent:
			return full, e.Err
		}
	}
	return full, nil
}

func (d *Dispatcher) forwardStream(ctx context.Context, acq *account.Acquisition, body io.ReadCloser, payload []byte) <-chan StreamEvent {
	out := make(chan StreamEvent, streamChannelCapacity)
	decoder := NewStreamDecoder(d.logger)
	decoded := decoder.Decode(ctx, body, payload)
	go func() {
		defer close(out)
		failed := false
		for event := range decoded {
			if _, ok := event.(ErrorEvent); ok {
				failed = true
			}
			select {
			case out <- event:
			case <-ctx.Done():
				releaseFailure(acq, "mid_stream_error")
				return
			}
		}
		if failed {
			releaseFailure(acq, "mid_stream_error")
			return
		}
		releaseSuccess(acq)
	}()
	return out
}

func buildKiroRequest(acc *account.Account, payload []byte, token, region string) (*http.Request, error) {
	if acc == nil {
		return nil, errs.New(errs.ClassFatal, "MISSING_ACCOUNT", "missing Kiro account")
	}
	var requestPayload KiroPayload
	if err := json.Unmarshal(payload, &requestPayload); err != nil {
		return nil, errs.Wrap(err, errs.ClassFatal, "failed to unmarshal Kiro payload")
	}
	if acc.ProfileARN != nil {
		profileARN := strings.TrimSpace(*acc.ProfileARN)
		if profileARN != "" {
			requestPayload.ProfileArn = profileARN
		}
	}
	var err error
	payload, err = json.Marshal(requestPayload)
	if err != nil {
		return nil, errs.Wrap(err, errs.ClassFatal, "failed to marshal Kiro payload")
	}

	apiRegion := strings.TrimSpace(region)
	if apiRegion == "" && acc.APIRegion != nil {
		apiRegion = strings.TrimSpace(*acc.APIRegion)
	}
	if apiRegion == "" {
		apiRegion = strings.TrimSpace(acc.Region)
	}
	if apiRegion == "" {
		apiRegion = "us-east-1"
	}
	url := fmt.Sprintf("https://q.%s.amazonaws.com/generateAssistantResponse", apiRegion)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, errs.Wrap(err, errs.ClassFatal, "failed to build Kiro request")
	}
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(payload)), nil }

	shadow := *acc
	if token != "" {
		if strings.EqualFold(acc.AuthMethod, "apikey") || strings.EqualFold(acc.AuthMethod, "api_key") {
			shadow.APIKey = &token
		} else {
			shadow.AccessToken = &token
		}
	}
	req.Header = antiban.BuildKiroRequestHeaders(&shadow, apiRegion)
	return req, nil
}

func normalizeDispatcherConfig(cfg DispatcherConfig) DispatcherConfig {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = defaultDispatcherMaxAttempts
	}
	if cfg.BaseRetryMs <= 0 {
		cfg.BaseRetryMs = defaultDispatcherBaseRetryMs
	}
	return cfg
}

func dispatcherBackoff(ctx context.Context, cfg DispatcherConfig, attempt int) error {
	d := time.Duration(cfg.BaseRetryMs) * time.Millisecond * time.Duration(1<<min(attempt, 30))
	d = min(d, maxDispatcherBackoff)
	if d > 0 {
		d += time.Duration(rand.Int63n(int64(d/4) + 1))
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		if classified := errs.FromNetwork(ctx.Err()); classified != nil {
			return classified
		}
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func classifyResponse(resp *http.Response, fallback error) error {
	var body []byte
	if resp != nil && resp.Body != nil {
		var readErr error
		body, readErr = io.ReadAll(resp.Body)
		if readErr != nil {
			slog.Warn("failed to read non-200 Kiro response body", "status_code", resp.StatusCode, "error", readErr)
		}
		if closeErr := resp.Body.Close(); closeErr != nil {
			slog.Warn("failed to close non-200 Kiro response body", "status_code", resp.StatusCode, "error", closeErr)
		}
	}
	if resp != nil {
		if classified := errs.FromKiroResponse(resp.StatusCode, body); classified != nil {
			return classified
		}
	}
	return classifyError(fallback)
}

func classifyError(err error) error {
	if err == nil {
		return errs.New(errs.ClassRecoverable, "UNKNOWN_ERROR", "unknown Kiro error")
	}
	var classified *errs.Error
	if errors.As(err, &classified) {
		return err
	}
	if network := errs.FromNetwork(err); network != nil {
		return network
	}
	return errs.Wrap(err, errs.ClassFatal, err.Error())
}

func isRecoverable(err error) bool {
	switch errs.ClassOf(err) {
	case errs.ClassRecoverable, errs.ClassQuotaExhausted, errs.ClassAuthExpired, errs.ClassRateLimited, errs.ClassNetwork:
		return true
	default:
		return false
	}
}

func failureReason(err error) string {
	var classified *errs.Error
	if errors.As(err, &classified) {
		if classified.Code != "" {
			return classified.Code
		}
		return classified.Class.String()
	}
	return "request_failed"
}

func isAuthStatus(status int) bool {
	return status == http.StatusUnauthorized || status == http.StatusForbidden
}

func appendExcluded(ids []string, id string) []string {
	if id == "" {
		return ids
	}
	if slices.Contains(ids, id) {
		return ids
	}
	return append(ids, id)
}

func releaseSuccess(acq *account.Acquisition) {
	if acq != nil && acq.ReleaseSuccess != nil {
		acq.ReleaseSuccess()
	}
}

func releaseFailure(acq *account.Acquisition, reason string) {
	if acq != nil && acq.ReleaseFailure != nil {
		acq.ReleaseFailure(reason)
	}
}
