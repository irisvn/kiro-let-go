package kiro

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func loadGolden(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return string(b)
}

func TestMapModel(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"sonnet", ModelClaudeSonnet46},
		{"sonnet-4.5", ModelClaudeSonnet45},
		{"claude-sonnet-4.5", ModelClaudeSonnet45},
		{"sonnet-4.6", ModelClaudeSonnet46},
		{"claude-sonnet-4.6", ModelClaudeSonnet46},
		{"opus", ModelClaudeOpus47},
		{"opus-4.5", ModelClaudeOpus45},
		{"claude-opus-4.5", ModelClaudeOpus45},
		{"opus-4.6", ModelClaudeOpus46},
		{"opus-4.7", ModelClaudeOpus47},
		{"haiku", ModelClaudeHaiku45},
		{"haiku-4.5", ModelClaudeHaiku45},
		{"claude-haiku-4.5", ModelClaudeHaiku45},
		{"unknown-model", "unknown-model"},
		// Prefix stripping
		{"kiro/claude-sonnet-4.6", ModelClaudeSonnet46},
		{"anthropic/claude-sonnet-4.6", ModelClaudeSonnet46},
		{"openai/claude-opus-4.7", ModelClaudeOpus47},
		{"aws/claude-haiku-4.5", ModelClaudeHaiku45},
		// Version separator normalization
		{"claude-sonnet-4-6", ModelClaudeSonnet46},
		{"claude-opus-4-7", ModelClaudeOpus47},
		{"claude-haiku-4-5", ModelClaudeHaiku45},
		// Case insensitive
		{"CLAUDE-SONNET-4.6", ModelClaudeSonnet46},
		{"Claude-Opus-4.7", ModelClaudeOpus47},
		// Underscore normalization
		{"claude_sonnet_4.5", ModelClaudeSonnet45},
		// Combined: prefix + separator + case
		{"Kiro/Claude-Sonnet-4-6", ModelClaudeSonnet46},
		{"ANTHROPIC/CLAUDE-OPUS-4-7", ModelClaudeOpus47},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, MapModel(tt.input))
		})
	}
}

func TestPayloadMarshalSnapshot(t *testing.T) {
	payload := KiroPayload{
		ConversationState: ConversationState{
			ConversationID:      "conv-123",
			AgentContinuationID: "agent-456",
			AgentTaskType:       "chat",
			ChatTriggerType:     "manual",
			CurrentMessage: CurrentMessage{
				UserInputMessage: UserInputMessage{
					Content: "What is the weather?",
					ModelID: ModelClaudeSonnet46,
					Origin:  "vscode",
					UserInputMessageContext: &UserInputMessageContext{
						Tools: []Tool{
							{
								ToolSpecification: ToolSpecification{
									Name:        "get_weather",
									Description: "Get current weather for a city",
									InputSchema: InputSchema{
										JSON: json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}}}`),
									},
								},
							},
						},
						ToolResults: []ToolResult{
							{
								ToolUseID: "tu_1",
								Content: []ToolResultContent{
									{Text: "Sunny, 25C"},
								},
								Status: "success",
							},
						},
					},
					Images: []ImagePart{
						{
							Format: "png",
							Source: ImageSource{Bytes: []byte{0x89, 0x50}},
						},
					},
				},
			},
			History: []HistoryItem{
				{UserInputMessage: &UserInputMessage{Content: "Hi"}},
				{AssistantResponseMessage: &AssistantResponseMessage{Content: "Hello!"}},
			},
		},
		ProfileArn: "arn:aws:iam::123456789012:profile/kiro",
	}

	got, err := json.MarshalIndent(payload, "", "  ")
	require.NoError(t, err)

	want := loadGolden(t, "payload.json")
	assert.JSONEq(t, want, string(got))
}

func TestEventsMarshalSnapshot(t *testing.T) {
	events := map[string]any{
		"assistantResponse": AssistantResponseEvent{Content: "Hello there"},
		"toolUse":           ToolUseEvent{Name: "get_weather", ToolUseID: "tu_1", Input: `{"city":"NYC"`, Stop: false},
		"contextUsage":      ContextUsageEvent{ContextUsagePercentage: 0.42},
	}

	got, err := json.MarshalIndent(events, "", "  ")
	require.NoError(t, err)

	want := loadGolden(t, "events.json")
	assert.JSONEq(t, want, string(got))
}

func TestHistoryItemMarshalJSON(t *testing.T) {
	t.Run("user", func(t *testing.T) {
		h := HistoryItem{UserInputMessage: &UserInputMessage{Content: "user msg"}}
		b, err := json.Marshal(h)
		require.NoError(t, err)
		assert.JSONEq(t, `{"userInputMessage":{"content":"user msg"}}`, string(b))
	})

	t.Run("assistant", func(t *testing.T) {
		h := HistoryItem{AssistantResponseMessage: &AssistantResponseMessage{Content: "assistant msg"}}
		b, err := json.Marshal(h)
		require.NoError(t, err)
		assert.JSONEq(t, `{"assistantResponseMessage":{"content":"assistant msg"}}`, string(b))
	})

	t.Run("empty", func(t *testing.T) {
		h := HistoryItem{}
		b, err := json.Marshal(h)
		require.NoError(t, err)
		assert.Equal(t, "null", string(b))
	})
}
