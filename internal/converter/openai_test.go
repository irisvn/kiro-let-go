package converter

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/irisvn/kiro-let-go/internal/api/openai"
	"github.com/irisvn/kiro-let-go/internal/errs"
	"github.com/stretchr/testify/require"
)

func TestOpenAIToNormalizedFixtures(t *testing.T) {
	tests := []struct {
		name  string
		check func(*testing.T, *NormalizedRequest)
	}{
		{
			name: "chat",
			check: func(t *testing.T, got *NormalizedRequest) {
				require.Equal(t, "You are concise.\nPrefer examples.", got.SystemPrompt)
				require.Equal(t, 4000, got.ThinkingBudget)
				require.Equal(t, []string{"END"}, got.Stop)
				require.Len(t, got.Messages, 2)
				require.Equal(t, "user", got.Messages[0].Role)
				require.Equal(t, "HelloAlso adjacent", got.Messages[0].Parts[0].(Text).Text+got.Messages[0].Parts[1].(Text).Text)
			},
		},
		{
			name: "with-tools",
			check: func(t *testing.T, got *NormalizedRequest) {
				require.Len(t, got.Tools, 1)
				require.Equal(t, "get_weather", got.Tools[0].Name)
				require.JSONEq(t, `{"type":"object","properties":{"location":{"type":"string"}},"required":["location"]}`, got.Tools[0].SchemaJSON)
				require.Len(t, got.Messages, 2)
				toolUse := got.Messages[1].Parts[1].(ToolUse)
				require.Equal(t, "call_1", toolUse.ID)
				require.Equal(t, `{"location":"Paris"}`, toolUse.InputJSON)
			},
		},
		{
			name: "with-tool-result",
			check: func(t *testing.T, got *NormalizedRequest) {
				require.Len(t, got.Messages, 3)
				require.Equal(t, "tool", got.Messages[2].Role)
				result := got.Messages[2].Parts[0].(ToolResult)
				require.Equal(t, "call_1", result.ToolUseID)
				require.Equal(t, `{"temperature":18}`, result.ContentText)
			},
		},
		{
			name: "multimodal-data-url",
			check: func(t *testing.T, got *NormalizedRequest) {
				require.Len(t, got.Messages, 1)
				require.Equal(t, "describe", got.Messages[0].Parts[0].(Text).Text)
				img := got.Messages[0].Parts[1].(Image)
				require.Equal(t, "image/png", img.MediaType)
				require.Equal(t, "aGVsbG8=", img.DataB64)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := readOpenAIRequestFixture(t, tt.name+".json")
			encoded, err := json.Marshal(req)
			require.NoError(t, err)
			var roundTripped openai.ChatCompletionRequest
			require.NoError(t, json.Unmarshal(encoded, &roundTripped))
			req = &roundTripped

			got, err := OpenAIToNormalized(req)
			require.NoError(t, err)
			tt.check(t, got)
		})
	}
}

func TestOpenAIToNormalizedRejectsHTTPImageURL(t *testing.T) {
	req := readOpenAIRequestFixture(t, "http-image-url.json")
	_, err := OpenAIToNormalized(req)
	require.Error(t, err)

	var classified *errs.Error
	require.True(t, errors.As(err, &classified))
	require.Equal(t, errs.ClassFatal, classified.Class)
	require.Equal(t, "IMAGE_URL_UNSUPPORTED", classified.Code)
}

func TestNormalizedToOpenAIResponse(t *testing.T) {
	resp, err := NormalizedToOpenAI(&NormalizedResponse{
		ID:         "chatcmpl_1",
		Model:      "gpt-4o",
		Created:    123,
		StopReason: "tool_calls",
		Usage:      Usage{InputTokens: 10, OutputTokens: 5},
		Message: NormalizedMessage{Role: "assistant", Parts: []NormalizedPart{
			Text{Text: "Calling tool"},
			ToolUse{ID: "call_1", Name: "get_weather", InputJSON: `{"location":"Paris"}`},
		}},
	})
	require.NoError(t, err)
	require.Equal(t, "chat.completion", resp.Object)
	require.Equal(t, "Calling tool", resp.Choices[0].Message.Content.Text)
	require.Equal(t, `{"location":"Paris"}`, resp.Choices[0].Message.ToolCalls[0].Function.Arguments)
	require.Equal(t, 15, resp.Usage.TotalTokens)
}

func readOpenAIRequestFixture(t *testing.T, name string) *openai.ChatCompletionRequest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "openai", name))
	require.NoError(t, err)
	var req openai.ChatCompletionRequest
	require.NoError(t, json.Unmarshal(data, &req))
	return &req
}
