package antiban_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/irisvn/kiro-let-go/internal/account"
	"github.com/irisvn/kiro-let-go/internal/antiban"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildKiroRequestHeadersAPIKey(t *testing.T) {
	apiKey := "test-api-key"
	acc := &account.Account{ID: "account-a", AuthMethod: "apikey", APIKey: &apiKey}

	headers := antiban.BuildKiroRequestHeaders(acc, "us-east-1")

	assert.Equal(t, "Bearer test-api-key", headers.Get("Authorization"))
	assert.Equal(t, "application/json", headers.Get("Content-Type"))
	assert.Equal(t, "close", headers.Get("Connection"))
	assert.Equal(t, "q.us-east-1.amazonaws.com", headers.Get("host"))
	assert.Equal(t, "true", headers.Get("x-amzn-codewhisperer-optout"))
	assert.Equal(t, "vibe", headers.Get("x-amzn-kiro-agent-mode"))
	assert.Equal(t, "attempt=1; max=3", headers.Get("amz-sdk-request"))
	assert.Equal(t, "API_KEY", headers.Get("tokentype"))
	assert.Contains(t, headers.Get("User-Agent"), "1.0.34")
	assert.Contains(t, headers.Get("x-amz-user-agent"), "1.0.34")

	invocationID, err := uuid.Parse(headers.Get("amz-sdk-invocation-id"))
	require.NoError(t, err)
	assert.Equal(t, uuid.Version(4), invocationID.Version())
}

func TestBuildKiroRequestHeadersOAuth(t *testing.T) {
	accessToken := "access-token"
	acc := &account.Account{ID: "account-oauth", AuthMethod: "oauth", AccessToken: &accessToken}

	headers := antiban.BuildKiroRequestHeaders(acc, "eu-west-1")

	assert.Equal(t, "Bearer access-token", headers.Get("Authorization"))
	assert.Empty(t, headers.Get("tokentype"))
}

func TestBuildKiroRequestHeadersStableFingerprintFreshInvocationID(t *testing.T) {
	apiKey := "test-api-key"
	acc := &account.Account{ID: "stable-account", AuthMethod: "api_key", APIKey: &apiKey}

	first := antiban.BuildKiroRequestHeaders(acc, "us-west-2")
	second := antiban.BuildKiroRequestHeaders(acc, "us-west-2")

	assert.Equal(t, first.Get("User-Agent"), second.Get("User-Agent"))
	assert.Equal(t, first.Get("x-amz-user-agent"), second.Get("x-amz-user-agent"))
	assert.NotEqual(t, first.Get("amz-sdk-invocation-id"), second.Get("amz-sdk-invocation-id"))
}

func TestOnceForDeterministicAndBounded(t *testing.T) {
	first := antiban.OnceFor("account-id", 7)
	second := antiban.OnceFor("account-id", 7)

	assert.Equal(t, first, second)
	assert.GreaterOrEqual(t, first, 0)
	assert.Less(t, first, 7)
	assert.Zero(t, antiban.OnceFor("account-id", 0))
}
