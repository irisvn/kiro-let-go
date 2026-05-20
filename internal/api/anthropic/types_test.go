package anthropic

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type badContentBlock struct{ Fn func() }

func (badContentBlock) blockType() string { return "bad" }

type badDelta struct{ Fn func() }

func (badDelta) deltaType() string { return "bad" }

func TestMessagesRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     MessagesRequest
		wantErr string
	}{
		{
			name: "valid",
			req: MessagesRequest{
				Model:     "claude-3-opus-20240229",
				MaxTokens: 1024,
				Messages:  []Message{{Role: "user", Content: &MessageContent{String: "hello"}}},
			},
		},
		{
			name:    "missing model",
			req:     MessagesRequest{MaxTokens: 1024, Messages: []Message{{Role: "user", Content: &MessageContent{String: "hi"}}}},
			wantErr: "model is required",
		},
		{
			name:    "zero max_tokens",
			req:     MessagesRequest{Model: "claude", MaxTokens: 0, Messages: []Message{{Role: "user", Content: &MessageContent{String: "hi"}}}},
			wantErr: "max_tokens must be greater than 0",
		},
		{
			name:    "negative max_tokens",
			req:     MessagesRequest{Model: "claude", MaxTokens: -1, Messages: []Message{{Role: "user", Content: &MessageContent{String: "hi"}}}},
			wantErr: "max_tokens must be greater than 0",
		},
		{
			name:    "empty messages",
			req:     MessagesRequest{Model: "claude", MaxTokens: 1024},
			wantErr: "messages is required",
		},
		{
			name:    "invalid role",
			req:     MessagesRequest{Model: "claude", MaxTokens: 1024, Messages: []Message{{Role: "system", Content: &MessageContent{String: "hi"}}}},
			wantErr: "messages[0].role must be user or assistant",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSystemField_RoundTrip(t *testing.T) {
	t.Run("string", func(t *testing.T) {
		original := &SystemField{String: "You are a helpful assistant.", IsArray: false}
		data, err := json.Marshal(original)
		require.NoError(t, err)
		assert.JSONEq(t, `"You are a helpful assistant."`, string(data))

		var parsed SystemField
		require.NoError(t, json.Unmarshal(data, &parsed))
		assert.Equal(t, original.String, parsed.String)
		assert.False(t, parsed.IsArray)
	})

	t.Run("array", func(t *testing.T) {
		original := &SystemField{
			IsArray: true,
			Blocks: []ContentBlock{
				TextBlock{Type: "text", Text: "You are a helpful assistant."},
			},
		}
		data, err := json.Marshal(original)
		require.NoError(t, err)
		assert.JSONEq(t, `[{"type":"text","text":"You are a helpful assistant."}]`, string(data))

		var parsed SystemField
		require.NoError(t, json.Unmarshal(data, &parsed))
		assert.True(t, parsed.IsArray)
		require.Len(t, parsed.Blocks, 1)
		assert.Equal(t, "text", parsed.Blocks[0].(TextBlock).Type)
	})
}

func TestMessageContent_RoundTrip(t *testing.T) {
	t.Run("string", func(t *testing.T) {
		original := &MessageContent{String: "hello", IsArray: false}
		data, err := json.Marshal(original)
		require.NoError(t, err)
		assert.JSONEq(t, `"hello"`, string(data))

		var parsed MessageContent
		require.NoError(t, json.Unmarshal(data, &parsed))
		assert.Equal(t, "hello", parsed.String)
		assert.False(t, parsed.IsArray)
	})

	t.Run("array", func(t *testing.T) {
		original := &MessageContent{
			IsArray: true,
			Blocks: []ContentBlock{
				TextBlock{Type: "text", Text: "hello"},
				ImageBlock{Type: "image", Source: ImageSource{Type: "base64", MediaType: "image/png", Data: "abc123"}},
			},
		}
		data, err := json.Marshal(original)
		require.NoError(t, err)

		var parsed MessageContent
		require.NoError(t, json.Unmarshal(data, &parsed))
		assert.True(t, parsed.IsArray)
		require.Len(t, parsed.Blocks, 2)
		assert.Equal(t, "hello", parsed.Blocks[0].(TextBlock).Text)
		assert.Equal(t, "image/png", parsed.Blocks[1].(ImageBlock).Source.MediaType)
	})
}

func TestContentBlock_RoundTrip(t *testing.T) {
	tests := []struct {
		name     string
		block    ContentBlock
		expected string
	}{
		{
			name:     "text",
			block:    TextBlock{Type: "text", Text: "hello", CacheControl: &CacheControl{Type: "ephemeral"}},
			expected: `{"type":"text","text":"hello","cache_control":{"type":"ephemeral"}}`,
		},
		{
			name:     "image",
			block:    ImageBlock{Type: "image", Source: ImageSource{Type: "base64", MediaType: "image/png", Data: "abc"}},
			expected: `{"type":"image","source":{"type":"base64","media_type":"image/png","data":"abc"}}`,
		},
		{
			name:     "tool_use",
			block:    ToolUseBlock{Type: "tool_use", ID: "tu_1", Name: "get_weather", Input: json.RawMessage(`{"city":"NYC"}`)},
			expected: `{"type":"tool_use","id":"tu_1","name":"get_weather","input":{"city":"NYC"}}`,
		},
		{
			name:     "tool_result_string",
			block:    ToolResultBlock{Type: "tool_result", ToolUseID: "tu_1", Content: &ToolResultContent{String: "sunny", IsArray: false}},
			expected: `{"type":"tool_result","tool_use_id":"tu_1","content":"sunny"}`,
		},
		{
			name: "tool_result_array",
			block: ToolResultBlock{Type: "tool_result", ToolUseID: "tu_1", Content: &ToolResultContent{
				IsArray: true,
				Blocks:  []ContentBlock{TextBlock{Type: "text", Text: "result"}},
			}},
			expected: `{"type":"tool_result","tool_use_id":"tu_1","content":[{"type":"text","text":"result"}]}`,
		},
		{
			name:     "thinking",
			block:    ThinkingBlock{Type: "thinking", Thinking: "I think...", Signature: "sig"},
			expected: `{"type":"thinking","thinking":"I think...","signature":"sig"}`,
		},
		{
			name:     "redacted_thinking",
			block:    RedactedThinkingBlock{Type: "redacted_thinking", Data: "redacted"},
			expected: `{"type":"redacted_thinking","data":"redacted"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.block)
			require.NoError(t, err)
			assert.JSONEq(t, tt.expected, string(data))

			parsed, err := unmarshalContentBlock(data)
			require.NoError(t, err)
			assert.Equal(t, tt.block.blockType(), parsed.blockType())
		})
	}
}

func TestContentBlock_UnknownType(t *testing.T) {
	_, err := unmarshalContentBlock([]byte(`{"type":"unknown"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown content block type")
}

func TestMessagesRequest_RoundTrip(t *testing.T) {
	temp := 0.7
	topP := 0.9
	topK := 50
	isError := true

	req := MessagesRequest{
		Model:         "claude-3-opus-20240229",
		MaxTokens:     1024,
		Stream:        true,
		Temperature:   &temp,
		TopP:          &topP,
		TopK:          &topK,
		StopSequences: []string{"STOP"},
		System: &SystemField{
			IsArray: true,
			Blocks: []ContentBlock{
				TextBlock{Type: "text", Text: "Be helpful."},
			},
		},
		Messages: []Message{
			{Role: "user", Content: &MessageContent{String: "hello"}},
			{Role: "assistant", Content: &MessageContent{IsArray: true, Blocks: []ContentBlock{
				TextBlock{Type: "text", Text: "hi"},
			}}},
		},
		Tools: []Tool{
			{Name: "get_weather", Description: "Get weather", InputSchema: json.RawMessage(`{"type":"object"}`)},
		},
		ToolChoice: &ToolChoice{Type: "auto"},
		Thinking:   &ThinkingConfig{Type: "enabled", BudgetTokens: 16000},
		Metadata:   &Metadata{UserID: "user_123"},
	}

	data, err := json.Marshal(req)
	require.NoError(t, err)

	var parsed MessagesRequest
	require.NoError(t, json.Unmarshal(data, &parsed))

	assert.Equal(t, req.Model, parsed.Model)
	assert.Equal(t, req.MaxTokens, parsed.MaxTokens)
	assert.True(t, parsed.Stream)
	assert.Equal(t, temp, *parsed.Temperature)
	assert.Equal(t, topP, *parsed.TopP)
	assert.Equal(t, topK, *parsed.TopK)
	require.Len(t, parsed.StopSequences, 1)
	assert.Equal(t, "STOP", parsed.StopSequences[0])
	require.NotNil(t, parsed.System)
	assert.True(t, parsed.System.IsArray)
	require.Len(t, parsed.Messages, 2)
	assert.Equal(t, "user", parsed.Messages[0].Role)
	assert.Equal(t, "assistant", parsed.Messages[1].Role)
	require.Len(t, parsed.Tools, 1)
	assert.Equal(t, "get_weather", parsed.Tools[0].Name)
	assert.Equal(t, "auto", parsed.ToolChoice.Type)
	assert.Equal(t, "enabled", parsed.Thinking.Type)
	assert.Equal(t, 16000, parsed.Thinking.BudgetTokens)
	assert.Equal(t, "user_123", parsed.Metadata.UserID)

	require.NotNil(t, parsed.Messages[1].Content)
	assert.True(t, parsed.Messages[1].Content.IsArray)
	require.Len(t, parsed.Messages[1].Content.Blocks, 1)
	assert.Equal(t, "hi", parsed.Messages[1].Content.Blocks[0].(TextBlock).Text)

	tr := ToolResultBlock{Type: "tool_result", ToolUseID: "tu_1", Content: &ToolResultContent{String: "sunny", IsArray: false}, IsError: &isError}
	trData, err := json.Marshal(tr)
	require.NoError(t, err)
	var parsedTR ToolResultBlock
	require.NoError(t, json.Unmarshal(trData, &parsedTR))
	require.NotNil(t, parsedTR.IsError)
	assert.True(t, *parsedTR.IsError)
	require.NotNil(t, parsedTR.Content)
	assert.Equal(t, "sunny", parsedTR.Content.String)
}

func TestMessagesResponse_RoundTrip(t *testing.T) {
	stopReason := "end_turn"
	resp := MessagesResponse{
		ID:         "msg_01",
		Type:       "message",
		Role:       "assistant",
		Content:    []ContentBlock{TextBlock{Type: "text", Text: "Hello!"}},
		Model:      "claude-3-opus-20240229",
		StopReason: &stopReason,
		Usage:      Usage{InputTokens: 10, OutputTokens: 5},
	}

	data, err := json.Marshal(resp)
	require.NoError(t, err)

	var parsed MessagesResponse
	require.NoError(t, json.Unmarshal(data, &parsed))

	assert.Equal(t, resp.ID, parsed.ID)
	assert.Equal(t, resp.Type, parsed.Type)
	assert.Equal(t, resp.Role, parsed.Role)
	assert.Equal(t, resp.Model, parsed.Model)
	require.NotNil(t, parsed.StopReason)
	assert.Equal(t, "end_turn", *parsed.StopReason)
	assert.Equal(t, 10, parsed.Usage.InputTokens)
	assert.Equal(t, 5, parsed.Usage.OutputTokens)
	require.Len(t, parsed.Content, 1)
	assert.Equal(t, "Hello!", parsed.Content[0].(TextBlock).Text)
}

func TestMessagesResponse_MarshalError(t *testing.T) {
	_, err := json.Marshal(MessagesResponse{Content: []ContentBlock{badContentBlock{Fn: func() {}}}})
	require.Error(t, err)
	require.ErrorContains(t, err, "marshal response content")
}

func TestCountTokensRequest_RoundTrip(t *testing.T) {
	req := CountTokensRequest{
		Model: "claude-3-opus-20240229",
		Messages: []Message{
			{Role: "user", Content: &MessageContent{String: "hello"}},
		},
		System: &SystemField{String: "sys", IsArray: false},
		Tools:  []Tool{{Name: "calc", InputSchema: json.RawMessage(`{}`)}},
	}

	data, err := json.Marshal(req)
	require.NoError(t, err)

	var parsed CountTokensRequest
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Equal(t, req.Model, parsed.Model)
	require.Len(t, parsed.Messages, 1)
	require.NotNil(t, parsed.System)
	assert.Equal(t, "sys", parsed.System.String)
}

func TestSSEEvents_RoundTrip(t *testing.T) {
	t.Run("MessageStartEvent", func(t *testing.T) {
		evt := MessageStartEvent{
			Type: "message_start",
			Message: MessagesResponse{
				ID:    "msg_01",
				Type:  "message",
				Role:  "assistant",
				Model: "claude-3-opus-20240229",
				Usage: Usage{InputTokens: 10, OutputTokens: 0},
			},
		}
		data, err := json.Marshal(evt)
		require.NoError(t, err)
		var parsed MessageStartEvent
		require.NoError(t, json.Unmarshal(data, &parsed))
		assert.Equal(t, evt.Type, parsed.Type)
		assert.Equal(t, evt.Message.ID, parsed.Message.ID)
	})

	t.Run("ContentBlockStartEvent_text", func(t *testing.T) {
		evt := ContentBlockStartEvent{
			Type:         "content_block_start",
			Index:        0,
			ContentBlock: TextBlock{Type: "text", Text: ""},
		}
		data, err := json.Marshal(evt)
		require.NoError(t, err)
		var parsed ContentBlockStartEvent
		require.NoError(t, json.Unmarshal(data, &parsed))
		assert.Equal(t, 0, parsed.Index)
		assert.Equal(t, "text", parsed.ContentBlock.(TextBlock).Type)
	})

	t.Run("ContentBlockStartEvent_tool_use", func(t *testing.T) {
		evt := ContentBlockStartEvent{
			Type:         "content_block_start",
			Index:        1,
			ContentBlock: ToolUseBlock{Type: "tool_use", ID: "tu_1", Name: "get_weather", Input: json.RawMessage(`{}`)},
		}
		data, err := json.Marshal(evt)
		require.NoError(t, err)
		var parsed ContentBlockStartEvent
		require.NoError(t, json.Unmarshal(data, &parsed))
		assert.Equal(t, "tool_use", parsed.ContentBlock.(ToolUseBlock).Type)
	})

	t.Run("ContentBlockStartEvent_marshal_error", func(t *testing.T) {
		_, err := json.Marshal(ContentBlockStartEvent{
			Type:         "content_block_start",
			Index:        0,
			ContentBlock: badContentBlock{Fn: func() {}},
		})
		require.Error(t, err)
		require.ErrorContains(t, err, "marshal content_block")
	})

	t.Run("ContentBlockDeltaEvent_text", func(t *testing.T) {
		evt := ContentBlockDeltaEvent{
			Type:  "content_block_delta",
			Index: 0,
			Delta: TextDelta{Type: "text_delta", Text: "Hello"},
		}
		data, err := json.Marshal(evt)
		require.NoError(t, err)
		var parsed ContentBlockDeltaEvent
		require.NoError(t, json.Unmarshal(data, &parsed))
		assert.Equal(t, "Hello", parsed.Delta.(TextDelta).Text)
	})

	t.Run("ContentBlockDeltaEvent_input_json", func(t *testing.T) {
		evt := ContentBlockDeltaEvent{
			Type:  "content_block_delta",
			Index: 1,
			Delta: InputJSONDelta{Type: "input_json_delta", PartialJSON: `{"city":`},
		}
		data, err := json.Marshal(evt)
		require.NoError(t, err)
		var parsed ContentBlockDeltaEvent
		require.NoError(t, json.Unmarshal(data, &parsed))
		assert.Equal(t, `{"city":`, parsed.Delta.(InputJSONDelta).PartialJSON)
	})

	t.Run("ContentBlockDeltaEvent_thinking", func(t *testing.T) {
		evt := ContentBlockDeltaEvent{
			Type:  "content_block_delta",
			Index: 0,
			Delta: ThinkingDelta{Type: "thinking_delta", Thinking: "I think..."},
		}
		data, err := json.Marshal(evt)
		require.NoError(t, err)
		var parsed ContentBlockDeltaEvent
		require.NoError(t, json.Unmarshal(data, &parsed))
		assert.Equal(t, "I think...", parsed.Delta.(ThinkingDelta).Thinking)
	})

	t.Run("ContentBlockDeltaEvent_signature", func(t *testing.T) {
		evt := ContentBlockDeltaEvent{
			Type:  "content_block_delta",
			Index: 0,
			Delta: SignatureDelta{Type: "signature_delta", Signature: "sig"},
		}
		data, err := json.Marshal(evt)
		require.NoError(t, err)
		var parsed ContentBlockDeltaEvent
		require.NoError(t, json.Unmarshal(data, &parsed))
		assert.Equal(t, "sig", parsed.Delta.(SignatureDelta).Signature)
	})

	t.Run("ContentBlockDeltaEvent_marshal_error", func(t *testing.T) {
		_, err := json.Marshal(ContentBlockDeltaEvent{
			Type:  "content_block_delta",
			Index: 0,
			Delta: badDelta{Fn: func() {}},
		})
		require.Error(t, err)
		require.ErrorContains(t, err, "marshal delta")
	})

	t.Run("ContentBlockStopEvent", func(t *testing.T) {
		evt := ContentBlockStopEvent{Type: "content_block_stop", Index: 0}
		data, err := json.Marshal(evt)
		require.NoError(t, err)
		var parsed ContentBlockStopEvent
		require.NoError(t, json.Unmarshal(data, &parsed))
		assert.Equal(t, 0, parsed.Index)
	})

	t.Run("MessageDeltaEvent", func(t *testing.T) {
		stopReason := "end_turn"
		evt := MessageDeltaEvent{
			Type: "message_delta",
			Delta: MessageDelta{
				StopReason: &stopReason,
			},
			Usage: Usage{OutputTokens: 25},
		}
		data, err := json.Marshal(evt)
		require.NoError(t, err)
		var parsed MessageDeltaEvent
		require.NoError(t, json.Unmarshal(data, &parsed))
		require.NotNil(t, parsed.Delta.StopReason)
		assert.Equal(t, "end_turn", *parsed.Delta.StopReason)
		assert.Equal(t, 25, parsed.Usage.OutputTokens)
	})

	t.Run("MessageStopEvent", func(t *testing.T) {
		evt := MessageStopEvent{Type: "message_stop"}
		data, err := json.Marshal(evt)
		require.NoError(t, err)
		var parsed MessageStopEvent
		require.NoError(t, json.Unmarshal(data, &parsed))
		assert.Equal(t, "message_stop", parsed.Type)
	})

	t.Run("PingEvent", func(t *testing.T) {
		evt := PingEvent{Type: "ping"}
		data, err := json.Marshal(evt)
		require.NoError(t, err)
		var parsed PingEvent
		require.NoError(t, json.Unmarshal(data, &parsed))
		assert.Equal(t, "ping", parsed.Type)
	})

	t.Run("ErrorEvent", func(t *testing.T) {
		evt := ErrorEvent{
			Type:  "error",
			Error: ErrorDetail{Type: "invalid_request_error", Message: "bad request"},
		}
		data, err := json.Marshal(evt)
		require.NoError(t, err)
		var parsed ErrorEvent
		require.NoError(t, json.Unmarshal(data, &parsed))
		assert.Equal(t, "invalid_request_error", parsed.Error.Type)
		assert.Equal(t, "bad request", parsed.Error.Message)
	})
}

func TestDelta_UnknownType(t *testing.T) {
	_, err := unmarshalDelta([]byte(`{"type":"unknown_delta"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown delta type")
}

func TestToolChoice_RoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		choice ToolChoice
	}{
		{name: "auto", choice: ToolChoice{Type: "auto"}},
		{name: "any", choice: ToolChoice{Type: "any"}},
		{name: "none", choice: ToolChoice{Type: "none"}},
		{name: "tool", choice: ToolChoice{Type: "tool", Name: "get_weather"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.choice)
			require.NoError(t, err)
			var parsed ToolChoice
			require.NoError(t, json.Unmarshal(data, &parsed))
			assert.Equal(t, tt.choice.Type, parsed.Type)
			assert.Equal(t, tt.choice.Name, parsed.Name)
		})
	}
}

func TestMessagesRequest_StringSystem(t *testing.T) {
	req := MessagesRequest{
		Model:     "claude-3-opus-20240229",
		MaxTokens: 1024,
		System:    &SystemField{String: "Be helpful.", IsArray: false},
		Messages:  []Message{{Role: "user", Content: &MessageContent{String: "hello"}}},
	}
	data, err := json.Marshal(req)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"system":"Be helpful."`)

	var parsed MessagesRequest
	require.NoError(t, json.Unmarshal(data, &parsed))
	require.NotNil(t, parsed.System)
	assert.Equal(t, "Be helpful.", parsed.System.String)
	assert.False(t, parsed.System.IsArray)
}

func TestMessagesRequest_StringContent(t *testing.T) {
	req := MessagesRequest{
		Model:     "claude-3-opus-20240229",
		MaxTokens: 1024,
		Messages:  []Message{{Role: "user", Content: &MessageContent{String: "hello world", IsArray: false}}},
	}
	data, err := json.Marshal(req)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"content":"hello world"`)

	var parsed MessagesRequest
	require.NoError(t, json.Unmarshal(data, &parsed))
	require.Len(t, parsed.Messages, 1)
	require.NotNil(t, parsed.Messages[0].Content)
	assert.Equal(t, "hello world", parsed.Messages[0].Content.String)
	assert.False(t, parsed.Messages[0].Content.IsArray)
}

func TestFixtures(t *testing.T) {
	tests := []struct {
		name     string
		file     string
		validate func(t *testing.T, data []byte)
	}{
		{
			name: "request_string_content",
			file: "testdata/request_string_content.json",
			validate: func(t *testing.T, data []byte) {
				var req MessagesRequest
				require.NoError(t, json.Unmarshal(data, &req))
				assert.Equal(t, "claude-3-opus-20240229", req.Model)
				assert.Equal(t, 1024, req.MaxTokens)
				require.Len(t, req.Messages, 1)
				assert.Equal(t, "user", req.Messages[0].Role)
				require.NotNil(t, req.Messages[0].Content)
				assert.Equal(t, "Hello, Claude!", req.Messages[0].Content.String)
				assert.NoError(t, req.Validate())
			},
		},
		{
			name: "request_string_system",
			file: "testdata/request_string_system.json",
			validate: func(t *testing.T, data []byte) {
				var req MessagesRequest
				require.NoError(t, json.Unmarshal(data, &req))
				require.NotNil(t, req.System)
				assert.Equal(t, "You are a helpful assistant.", req.System.String)
				assert.False(t, req.System.IsArray)
				assert.NoError(t, req.Validate())
			},
		},
		{
			name: "request_array_system_and_content",
			file: "testdata/request_array_system_and_content.json",
			validate: func(t *testing.T, data []byte) {
				var req MessagesRequest
				require.NoError(t, json.Unmarshal(data, &req))
				require.NotNil(t, req.System)
				assert.True(t, req.System.IsArray)
				require.Len(t, req.System.Blocks, 1)
				assert.Equal(t, "ephemeral", req.System.Blocks[0].(TextBlock).CacheControl.Type)
				require.Len(t, req.Messages, 1)
				assert.True(t, req.Messages[0].Content.IsArray)
				require.Len(t, req.Messages[0].Content.Blocks, 2)
				assert.Equal(t, "image/png", req.Messages[0].Content.Blocks[1].(ImageBlock).Source.MediaType)
				assert.NoError(t, req.Validate())
			},
		},
		{
			name: "response_text",
			file: "testdata/response_text.json",
			validate: func(t *testing.T, data []byte) {
				var resp MessagesResponse
				require.NoError(t, json.Unmarshal(data, &resp))
				assert.Equal(t, "msg_01", resp.ID)
				assert.Equal(t, "assistant", resp.Role)
				require.Len(t, resp.Content, 1)
				assert.Equal(t, "Hello! How can I help you today?", resp.Content[0].(TextBlock).Text)
				assert.Equal(t, 10, resp.Usage.InputTokens)
				assert.Equal(t, 15, resp.Usage.OutputTokens)
			},
		},
		{
			name: "response_thinking",
			file: "testdata/response_thinking.json",
			validate: func(t *testing.T, data []byte) {
				var resp MessagesResponse
				require.NoError(t, json.Unmarshal(data, &resp))
				require.Len(t, resp.Content, 2)
				assert.Equal(t, "thinking", resp.Content[0].(ThinkingBlock).Type)
				assert.Equal(t, "I need to analyze this...", resp.Content[0].(ThinkingBlock).Thinking)
				assert.Equal(t, "abc123", resp.Content[0].(ThinkingBlock).Signature)
				assert.Equal(t, "Here is my analysis.", resp.Content[1].(TextBlock).Text)
			},
		},
		{
			name: "request_tool_result",
			file: "testdata/request_tool_result.json",
			validate: func(t *testing.T, data []byte) {
				var req MessagesRequest
				require.NoError(t, json.Unmarshal(data, &req))
				require.Len(t, req.Messages, 1)
				require.True(t, req.Messages[0].Content.IsArray)
				require.Len(t, req.Messages[0].Content.Blocks, 1)
				tr := req.Messages[0].Content.Blocks[0].(ToolResultBlock)
				assert.Equal(t, "tu_1", tr.ToolUseID)
				require.NotNil(t, tr.Content)
				assert.Equal(t, "The weather is sunny.", tr.Content.String)
				require.NotNil(t, tr.IsError)
				assert.False(t, *tr.IsError)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := os.ReadFile(tt.file)
			require.NoError(t, err)
			tt.validate(t, data)
		})
	}
}
