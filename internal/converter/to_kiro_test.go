package converter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizedToKiroFixtures(t *testing.T) {
	tests := []struct {
		name string
		req  *NormalizedRequest
	}{
		{name: "text-only", req: &NormalizedRequest{
			Model: "sonnet",
			Messages: []NormalizedMessage{
				{Role: "user", Parts: []NormalizedPart{Text{Text: "Hi"}}},
				{Role: "assistant", Parts: []NormalizedPart{Text{Text: "Hello"}}},
				{Role: "user", Parts: []NormalizedPart{Text{Text: "Tell me a joke"}}},
			},
		}},
		{name: "with-tools", req: &NormalizedRequest{
			Model: "opus-4.7",
			Tools: []NormalizedTool{{
				Name:        "get_weather",
				Description: "Get current weather",
				SchemaJSON:  `{"type":"object","properties":{"location":{"type":"string"}},"required":["location"]}`,
			}},
			Messages: []NormalizedMessage{
				{Role: "user", Parts: []NormalizedPart{Text{Text: "Weather?"}}},
				{Role: "assistant", Parts: []NormalizedPart{Text{Text: "Checking"}, ToolUse{ID: "call_1", Name: "get_weather", InputJSON: `{"location":"Paris"}`}}},
				{Role: "tool", Parts: []NormalizedPart{ToolResult{ToolUseID: "call_1", ContentText: "18C", IsError: false}}},
				{Role: "user", Parts: []NormalizedPart{Text{Text: "Thanks"}}},
			},
		}},
		{name: "adjacent-assistant-collapse", req: &NormalizedRequest{
			Model: "haiku",
			Messages: []NormalizedMessage{
				{Role: "assistant", Parts: []NormalizedPart{Text{Text: "First"}, ToolUse{ID: "call_a", Name: "a", InputJSON: `{}`}}},
				{Role: "assistant", Parts: []NormalizedPart{Text{Text: "Second"}, ToolUse{ID: "call_b", Name: "b", InputJSON: `{"x":1}`}}},
				{Role: "tool", Parts: []NormalizedPart{ToolResult{ToolUseID: "call_a", ContentText: "A"}, ToolResult{ToolUseID: "call_b", ContentText: "B"}}},
				{Role: "user", Parts: []NormalizedPart{Text{Text: "continue"}}},
			},
		}},
		{name: "multimodal-thinking", req: &NormalizedRequest{
			Model:          "claude-sonnet-4.5",
			ThinkingBudget: 8000,
			Messages: []NormalizedMessage{
				{Role: "user", Parts: []NormalizedPart{Text{Text: "Look"}, Image{MediaType: "image/png", DataB64: "aGVsbG8="}, Text{Text: " now"}}},
			},
		}},
		{name: "unpaired-dropped", req: &NormalizedRequest{
			Model: "sonnet-4.6",
			Messages: []NormalizedMessage{
				{Role: "assistant", Parts: []NormalizedPart{ToolUse{ID: "orphan_use", Name: "missing_result", InputJSON: `{}`}}},
				{Role: "tool", Parts: []NormalizedPart{ToolResult{ToolUseID: "orphan_result", ContentText: "no use"}}},
				{Role: "assistant", Parts: []NormalizedPart{ToolUse{ID: "paired", Name: "ok", InputJSON: `{}`}}},
				{Role: "tool", Parts: []NormalizedPart{ToolResult{ToolUseID: "paired", ContentText: "ok"}}},
				{Role: "user", Parts: []NormalizedPart{Text{Text: "done?"}}},
			},
		}},
		{name: "trailing-tool", req: &NormalizedRequest{
			Model: "claude-sonnet-4.5",
			Tools: []NormalizedTool{{
				Name:        "glob",
				Description: "Glob files",
				SchemaJSON:  `{"type":"object"}`,
			}},
			Messages: []NormalizedMessage{
				{Role: "user", Parts: []NormalizedPart{Text{Text: "Find files"}}},
				{Role: "assistant", Parts: []NormalizedPart{Text{Text: "Let me search"}, ToolUse{ID: "call_glob", Name: "glob", InputJSON: `{"pattern":"*"}`}}},
				{Role: "tool", Parts: []NormalizedPart{ToolResult{ToolUseID: "call_glob", ContentText: "file1.txt, file2.txt", IsError: false}}},
			},
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizedToKiro(tt.req, "arn:aws:kiro:test-profile")
			require.NoError(t, err)

			got.ConversationState.ConversationID = "00000000-0000-4000-8000-000000000001"
			got.ConversationState.AgentContinuationID = "00000000-0000-4000-8000-000000000002"
			encoded, err := json.MarshalIndent(got, "", "  ")
			require.NoError(t, err)

			want := readKiroSnapshot(t, tt.name+".json")
			assert.JSONEq(t, want, string(encoded))
		})
	}
}

func TestNormalizedToKiroDoesNotMutateInput(t *testing.T) {
	req := &NormalizedRequest{
		Model:    "sonnet",
		Tools:    []NormalizedTool{{Name: "tool", SchemaJSON: `{}`}},
		Messages: []NormalizedMessage{{Role: "user", Parts: []NormalizedPart{Text{Text: "hello"}}}},
	}
	before, err := json.Marshal(req)
	require.NoError(t, err)

	_, err = NormalizedToKiro(req, "")
	require.NoError(t, err)
	after, err := json.Marshal(req)
	require.NoError(t, err)
	assert.JSONEq(t, string(before), string(after))
}

func readKiroSnapshot(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "kiro", name))
	require.NoError(t, err)
	return string(b)
}
