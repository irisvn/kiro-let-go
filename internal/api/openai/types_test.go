package openai

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func requireRoundTripEqual(t *testing.T, v1, v2 any) {
	t.Helper()
	b1, err := json.Marshal(v1)
	require.NoError(t, err)
	b2, err := json.Marshal(v2)
	require.NoError(t, err)
	require.JSONEq(t, string(b1), string(b2))
}

func TestRoundTrip_ChatCompletionRequest_StringContent(t *testing.T) {
	data, err := os.ReadFile("testdata/request_string_content.json")
	require.NoError(t, err)

	var req ChatCompletionRequest
	require.NoError(t, json.Unmarshal(data, &req))

	out, err := json.Marshal(&req)
	require.NoError(t, err)

	var req2 ChatCompletionRequest
	require.NoError(t, json.Unmarshal(out, &req2))

	requireRoundTripEqual(t, req, req2)
}

func TestRoundTrip_ChatCompletionRequest_Multimodal(t *testing.T) {
	data, err := os.ReadFile("testdata/request_multimodal.json")
	require.NoError(t, err)

	var req ChatCompletionRequest
	require.NoError(t, json.Unmarshal(data, &req))

	require.Len(t, req.Messages[0].Content.Parts, 2)
	require.Equal(t, "text", req.Messages[0].Content.Parts[0].Type)
	require.Equal(t, "What is in this image?", req.Messages[0].Content.Parts[0].Text)
	require.Equal(t, "image_url", req.Messages[0].Content.Parts[1].Type)
	require.Equal(t, "https://example.com/image.png", req.Messages[0].Content.Parts[1].ImageURL.URL)
	require.Equal(t, "high", req.Messages[0].Content.Parts[1].ImageURL.Detail)

	out, err := json.Marshal(&req)
	require.NoError(t, err)

	var req2 ChatCompletionRequest
	require.NoError(t, json.Unmarshal(out, &req2))

	requireRoundTripEqual(t, req, req2)
}

func TestRoundTrip_ChatCompletionRequest_Tools(t *testing.T) {
	data, err := os.ReadFile("testdata/request_tools.json")
	require.NoError(t, err)

	var req ChatCompletionRequest
	require.NoError(t, json.Unmarshal(data, &req))

	require.Len(t, req.Tools, 1)
	require.Equal(t, "function", req.Tools[0].Type)
	require.Equal(t, "get_weather", req.Tools[0].Function.Name)
	require.JSONEq(t, `{"type":"object","properties":{"location":{"type":"string"}}}`, string(req.Tools[0].Function.Parameters))

	out, err := json.Marshal(&req)
	require.NoError(t, err)

	var req2 ChatCompletionRequest
	require.NoError(t, json.Unmarshal(out, &req2))

	requireRoundTripEqual(t, req, req2)
}

func TestRoundTrip_ChatCompletionResponse(t *testing.T) {
	data, err := os.ReadFile("testdata/response.json")
	require.NoError(t, err)

	var resp ChatCompletionResponse
	require.NoError(t, json.Unmarshal(data, &resp))

	require.Equal(t, "chatcmpl-abc123", resp.ID)
	require.Equal(t, "stop", resp.Choices[0].FinishReason)
	require.Equal(t, "{\"location\":\"Paris\"}", resp.Choices[0].Message.ToolCalls[0].Function.Arguments)

	out, err := json.Marshal(&resp)
	require.NoError(t, err)

	var resp2 ChatCompletionResponse
	require.NoError(t, json.Unmarshal(out, &resp2))

	requireRoundTripEqual(t, resp, resp2)
}

func TestRoundTrip_ChatCompletionChunk(t *testing.T) {
	data, err := os.ReadFile("testdata/chunk.json")
	require.NoError(t, err)

	var chunk ChatCompletionChunk
	require.NoError(t, json.Unmarshal(data, &chunk))

	require.Equal(t, "chat.completion.chunk", chunk.Object)
	require.Equal(t, "Hello", chunk.Choices[0].Delta.Content)
	require.Equal(t, "thinking...", chunk.Choices[0].Delta.ReasoningContent)
	require.Equal(t, "{\"loc", chunk.Choices[0].Delta.ToolCalls[0].Function.Arguments)

	out, err := json.Marshal(&chunk)
	require.NoError(t, err)

	var chunk2 ChatCompletionChunk
	require.NoError(t, json.Unmarshal(out, &chunk2))

	requireRoundTripEqual(t, chunk, chunk2)
}

func TestRoundTrip_ModelsResponse(t *testing.T) {
	data, err := os.ReadFile("testdata/models.json")
	require.NoError(t, err)

	var mr ModelsResponse
	require.NoError(t, json.Unmarshal(data, &mr))

	require.Len(t, mr.Data, 2)
	require.Equal(t, "gpt-4o", mr.Data[0].ID)

	out, err := json.Marshal(&mr)
	require.NoError(t, err)

	var mr2 ModelsResponse
	require.NoError(t, json.Unmarshal(out, &mr2))

	requireRoundTripEqual(t, mr, mr2)
}

func TestMessageContent_Polymorphic(t *testing.T) {
	t.Run("string", func(t *testing.T) {
		var mc MessageContent
		require.NoError(t, json.Unmarshal([]byte(`"hello world"`), &mc))
		require.Equal(t, "hello world", mc.Text)
		require.Nil(t, mc.Parts)

		b, err := json.Marshal(mc)
		require.NoError(t, err)
		require.Equal(t, `"hello world"`, string(b))
	})

	t.Run("parts", func(t *testing.T) {
		var mc MessageContent
		require.NoError(t, json.Unmarshal([]byte(`[{"type":"text","text":"hi"}]`), &mc))
		require.Len(t, mc.Parts, 1)
		require.Equal(t, "text", mc.Parts[0].Type)
		require.Equal(t, "hi", mc.Parts[0].Text)

		b, err := json.Marshal(mc)
		require.NoError(t, err)
		require.JSONEq(t, `[{"type":"text","text":"hi"}]`, string(b))
	})

	t.Run("invalid", func(t *testing.T) {
		var mc MessageContent
		err := json.Unmarshal([]byte(`123`), &mc)
		require.Error(t, err)
	})

	t.Run("empty string", func(t *testing.T) {
		mc := MessageContent{Text: ""}
		b, err := json.Marshal(mc)
		require.NoError(t, err)
		require.Equal(t, `""`, string(b))
	})

	t.Run("empty parts", func(t *testing.T) {
		mc := MessageContent{Parts: []ContentPart{}}
		b, err := json.Marshal(mc)
		require.NoError(t, err)
		require.Equal(t, `[]`, string(b))
	})
}

func TestValidate(t *testing.T) {
	t.Run("missing model", func(t *testing.T) {
		r := ChatCompletionRequest{Messages: []ChatMessage{{Role: "user", Content: MessageContent{Text: "hi"}}}}
		err := r.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "model")
	})

	t.Run("missing messages", func(t *testing.T) {
		r := ChatCompletionRequest{Model: "claude-sonnet-4.6"}
		err := r.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "messages")
	})

	t.Run("invalid role", func(t *testing.T) {
		r := ChatCompletionRequest{
			Model:    "claude-sonnet-4.6",
			Messages: []ChatMessage{{Role: "invalid", Content: MessageContent{Text: "hi"}}},
		}
		err := r.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid role")
	})

	t.Run("tool message without tool_call_id", func(t *testing.T) {
		r := ChatCompletionRequest{
			Model:    "claude-sonnet-4.6",
			Messages: []ChatMessage{{Role: "tool", Content: MessageContent{Text: "result"}}},
		}
		err := r.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "tool_call_id")
	})

	t.Run("valid", func(t *testing.T) {
		r := ChatCompletionRequest{
			Model: "claude-sonnet-4.6",
			Messages: []ChatMessage{
				{Role: "system", Content: MessageContent{Text: "sys"}},
				{Role: "user", Content: MessageContent{Text: "hi"}},
				{Role: "assistant", Content: MessageContent{Text: "hello"}},
				{Role: "tool", Content: MessageContent{Text: "result"}, ToolCallID: "call_1"},
			},
		}
		require.NoError(t, r.Validate())
	})

	t.Run("non-Claude model", func(t *testing.T) {
		r := ChatCompletionRequest{
			Model:    "gpt-4o",
			Messages: []ChatMessage{{Role: "user", Content: MessageContent{Text: "hi"}}},
		}
		err := r.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "Claude model")
	})
}
