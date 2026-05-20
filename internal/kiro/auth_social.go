package kiro

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/irisvn/kiro-let-go/internal/account"
	"github.com/irisvn/kiro-let-go/internal/errs"
)

// SocialAuth handles OAuth-style token refresh for social-authenticated accounts.
type SocialAuth struct {
	httpClient *http.Client
	logger     *slog.Logger
	baseURL    string // overridable for testing
}

// NewSocialAuth creates a SocialAuth with the given HTTP client and logger.
func NewSocialAuth(httpClient *http.Client, logger *slog.Logger) *SocialAuth {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &SocialAuth{
		httpClient: httpClient,
		logger:     logger,
	}
}

// Refresh exchanges the account's refresh token for new access credentials.
func (s *SocialAuth) Refresh(ctx context.Context, acc *account.Account) (newAccessToken, newRefreshToken string, expiresAt time.Time, err error) {
	if acc.RefreshToken == nil || *acc.RefreshToken == "" {
		return "", "", time.Time{}, errs.New(errs.ClassFatal, "MISSING_REFRESH_TOKEN", "account has no refresh token")
	}

	authRegion := "us-east-1"
	if acc.AuthRegion != nil && *acc.AuthRegion != "" {
		authRegion = *acc.AuthRegion
	} else if acc.Region != "" {
		authRegion = acc.Region
	}

	endpoint := fmt.Sprintf("https://prod.%s.auth.desktop.kiro.dev/refreshToken", authRegion)
	if s.baseURL != "" {
		endpoint = s.baseURL + "/refreshToken"
	}

	reqBody, err := json.Marshal(map[string]string{
		"refreshToken": *acc.RefreshToken,
	})
	if err != nil {
		return "", "", time.Time{}, errs.Wrap(err, errs.ClassFatal, "failed to marshal refresh request")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return "", "", time.Time{}, errs.Wrap(err, errs.ClassFatal, "failed to create refresh request")
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", fmt.Sprintf("KiroIDE-%s-%s", KiroVersion, acc.MachineID))
	req.Host = fmt.Sprintf("prod.%s.auth.desktop.kiro.dev", authRegion)
	req.Header.Set("Connection", "close")

	client := s.httpClient
	if acc.ProxyURL != nil && *acc.ProxyURL != "" {
		proxyClient, err := s.buildProxiedClient(acc)
		if err != nil {
			return "", "", time.Time{}, err
		}
		client = proxyClient
	}

	resp, err := client.Do(req)
	if err != nil {
		if netErr := errs.FromNetwork(err); netErr != nil {
			return "", "", time.Time{}, netErr
		}
		return "", "", time.Time{}, errs.Wrap(err, errs.ClassRecoverable, "refresh request failed")
	}
	defer resp.Body.Close()

	var respBody map[string]json.RawMessage
	if decodeErr := json.NewDecoder(resp.Body).Decode(&respBody); decodeErr != nil {
		// Non-2xx with unreadable body falls through to status-based handling.
		s.warn("failed to decode social auth refresh response", "status_code", resp.StatusCode, "error", decodeErr)
	}

	switch {
	case resp.StatusCode == http.StatusOK:
		return s.parseSuccessResponse(respBody)
	case resp.StatusCode == http.StatusBadRequest:
		if isInvalidGrant(respBody) {
			return "", "", time.Time{}, errs.New(errs.ClassFatal, "INVALID_REFRESH_TOKEN", "invalid refresh token provided")
		}
		return "", "", time.Time{}, errs.New(errs.ClassFatal, "REFRESH_FATAL", fmt.Sprintf("bad request: %d", resp.StatusCode))
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return "", "", time.Time{}, errs.New(errs.ClassFatal, "REFRESH_AUTH_FAILURE", fmt.Sprintf("authentication failed: %d", resp.StatusCode))
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return "", "", time.Time{}, errs.New(errs.ClassRecoverable, "REFRESH_RECOVERABLE", fmt.Sprintf("server/rate-limit error: %d", resp.StatusCode))
	default:
		return "", "", time.Time{}, errs.New(errs.ClassFatal, "REFRESH_UNKNOWN", fmt.Sprintf("unexpected status: %d", resp.StatusCode))
	}
}

func (s *SocialAuth) buildProxiedClient(acc *account.Account) (*http.Client, error) {
	proxyURL, err := url.Parse(*acc.ProxyURL)
	if err != nil {
		return nil, errs.Wrap(err, errs.ClassFatal, "invalid proxy URL")
	}
	if acc.ProxyUsername != nil && *acc.ProxyUsername != "" {
		password := ""
		if acc.ProxyPassword != nil {
			password = *acc.ProxyPassword
		}
		proxyURL.User = url.UserPassword(*acc.ProxyUsername, password)
	}
	transport := &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
	}
	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}, nil
}

func (s *SocialAuth) parseSuccessResponse(body map[string]json.RawMessage) (newAccessToken, newRefreshToken string, expiresAt time.Time, err error) {
	var accessToken string
	if raw, ok := body["accessToken"]; ok {
		if unmarshalErr := json.Unmarshal(raw, &accessToken); unmarshalErr != nil {
			return "", "", time.Time{}, errs.Wrap(unmarshalErr, errs.ClassFatal, "invalid accessToken in response")
		}
	}
	if accessToken == "" {
		return "", "", time.Time{}, errs.New(errs.ClassFatal, "MISSING_ACCESS_TOKEN", "response missing accessToken")
	}

	var refreshToken string
	if raw, ok := body["refreshToken"]; ok {
		if unmarshalErr := json.Unmarshal(raw, &refreshToken); unmarshalErr != nil {
			s.warn("failed to decode refreshToken from social auth response", "error", unmarshalErr)
		}
	}

	expiresIn := 3600
	if raw, ok := body["expiresIn"]; ok {
		var expiresInInt int
		if unmarshalErr := json.Unmarshal(raw, &expiresInInt); unmarshalErr == nil {
			expiresIn = expiresInInt
		} else {
			s.warn("failed to decode expiresIn from social auth response", "error", unmarshalErr)
		}
	}

	expiresAt = time.Now().UTC().Add(time.Duration(expiresIn) * time.Second)
	return accessToken, refreshToken, expiresAt, nil
}

func isInvalidGrant(body map[string]json.RawMessage) bool {
	var errStr string
	if raw, ok := body["error"]; ok {
		if unmarshalErr := json.Unmarshal(raw, &errStr); unmarshalErr != nil {
			slog.Warn("failed to decode error from social auth response", "error", unmarshalErr)
		}
	}
	if errStr != "invalid_grant" {
		return false
	}
	var desc string
	if raw, ok := body["error_description"]; ok {
		if unmarshalErr := json.Unmarshal(raw, &desc); unmarshalErr != nil {
			slog.Warn("failed to decode error_description from social auth response", "error", unmarshalErr)
		}
	}
	return strings.Contains(desc, "Invalid refresh token provided")
}

func (s *SocialAuth) warn(msg string, args ...any) {
	if s != nil && s.logger != nil {
		s.logger.Warn(msg, args...)
		return
	}
	slog.Warn(msg, args...)
}
