package converter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/irisvn/kiro-let-go/internal/api/anthropic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnthropicRoundTripFixtures(t *testing.T) {
	fixtures := []string{"text-only", "with-tools", "with-thinking", "multimodal"}
	for _, fixture := range fixtures {
		t.Run(fixture, func(t *testing.T) {
			req := loadAnthropicFixture(t, fixture)

			normalized, err := AnthropicToNormalized(req)
			require.NoError(t, err)
			assert.Equal(t, req.Model, normalized.Model)
			assert.Equal(t, req.MaxTokens, normalized.MaxOutputTokens)
			assert.Len(t, normalized.Messages, len(req.Messages))

			resp, err := NormalizedToAnthropic(normalized)
			require.NoError(t, err)
			assert.Equal(t, req.Model, resp.Model)
			assertAnthropicContentPreserved(t, req, resp)
		})
	}
}

func TestAnthropicToNormalizedFields(t *testing.T) {
	req := loadAnthropicFixture(t, "with-tools")
	normalized, err := AnthropicToNormalized(req)
	require.NoError(t, err)

	assert.Equal(t, "Use tools when useful.", normalized.SystemPrompt)
	require.Len(t, normalized.Tools, 1)
	assert.Equal(t, "get_weather", normalized.Tools[0].Name)
	assert.Equal(t, "Get the weather", normalized.Tools[0].Description)
	assert.JSONEq(t, string(req.Tools[0].InputSchema), normalized.Tools[0].SchemaJSON)
	assert.Equal(t, NormalizedToolChoice{Mode: "tool", Name: "get_weather"}, normalized.ToolChoice)
}

func TestAnthropicToNormalizedThinkingDefault(t *testing.T) {
	req := loadAnthropicFixture(t, "with-thinking")
	normalized, err := AnthropicToNormalized(req)
	require.NoError(t, err)
	assert.Equal(t, 4000, normalized.ThinkingBudget)
}

func loadAnthropicFixture(t *testing.T, name string) *anthropic.MessagesRequest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "anthropic", name+".json"))
	require.NoError(t, err)
	var req anthropic.MessagesRequest
	require.NoError(t, json.Unmarshal(data, &req))
	return &req
}

func assertAnthropicContentPreserved(t *testing.T, req *anthropic.MessagesRequest, resp *anthropic.MessagesResponse) {
	t.Helper()
	want := flattenRequestParts(t, req)
	got := contentBlocksToParts(t, resp.Content)
	require.Len(t, got, len(want))
	for i := range want {
		assert.Equal(t, want[i].Kind(), got[i].Kind(), "part %d kind", i)
		switch w := want[i].(type) {
		case Text:
			assert.Equal(t, w, got[i])
		case Image:
			assert.Equal(t, w, got[i])
		case ToolUse:
			assert.Equal(t, w, got[i])
		case ToolResult:
			assert.Equal(t, w, got[i])
		case Thinking:
			assert.Equal(t, w, got[i])
		}
	}
}

func flattenRequestParts(t *testing.T, req *anthropic.MessagesRequest) []NormalizedPart {
	t.Helper()
	var out []NormalizedPart
	for _, msg := range req.Messages {
		parts, err := messageContentToParts(msg.Content)
		require.NoError(t, err)
		out = append(out, parts...)
	}
	return out
}

func contentBlocksToParts(t *testing.T, blocks []anthropic.ContentBlock) []NormalizedPart {
	t.Helper()
	parts := make([]NormalizedPart, 0, len(blocks))
	for _, block := range blocks {
		part, err := anthropicBlockToNormalizedPart(block)
		require.NoError(t, err)
		parts = append(parts, part)
	}
	return parts
}
