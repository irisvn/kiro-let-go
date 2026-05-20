package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/irisvn/kiro-let-go/internal/account"
	"github.com/irisvn/kiro-let-go/internal/kiro"
	"github.com/stretchr/testify/require"
)

type fakeDispatcher struct {
	streamFn func(context.Context, *kiro.KiroPayload, account.SelectionHint) (<-chan kiro.StreamEvent, error)
	onceFn   func(context.Context, *kiro.KiroPayload, account.SelectionHint) (kiro.FullResponse, error)
	lastCtx  context.Context
	lastHint account.SelectionHint
	lastBody *kiro.KiroPayload
}

func (f *fakeDispatcher) Stream(ctx context.Context, payload *kiro.KiroPayload, hint account.SelectionHint) (<-chan kiro.StreamEvent, error) {
	f.lastCtx = ctx
	f.lastHint = hint
	f.lastBody = payload
	if f.streamFn == nil {
		ch := make(chan kiro.StreamEvent)
		close(ch)
		return ch, nil
	}
	return f.streamFn(ctx, payload, hint)
}

func (f *fakeDispatcher) Once(ctx context.Context, payload *kiro.KiroPayload, hint account.SelectionHint) (kiro.FullResponse, error) {
	f.lastCtx = ctx
	f.lastHint = hint
	f.lastBody = payload
	if f.onceFn == nil {
		return kiro.FullResponse{}, nil
	}
	return f.onceFn(ctx, payload, hint)
}

func TestHandlerNonStreamingResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dispatcher := &fakeDispatcher{
		onceFn: func(context.Context, *kiro.KiroPayload, account.SelectionHint) (kiro.FullResponse, error) {
			return kiro.FullResponse{
				Text:       "hello",
				Thinking:   "considering options",
				ToolUses:   []kiro.ToolUseEntry{{ToolUseID: "tool-1", Name: "lookup", Input: `{"city":"Paris"}`}},
				Usage:      kiro.Usage{InputTokens: 11, OutputTokens: 7},
				StopReason: "tool_use",
			}, nil
		},
	}

	r := gin.New()
	r.POST("/v1/chat/completions", Handler(HandlerOptions{
		Dispatcher: dispatcher,
		Now:        func() time.Time { return time.Unix(1700000000, 0) },
		NewID:      func() string { return "test-id" },
	}))

	body := `{"model":"claude-sonnet-4.6","reasoning_effort":"medium","messages":[{"role":"user","content":"Hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	r.ServeHTTP(resp, req)

	require.Equal(t, http.StatusOK, resp.Code)
	require.NotNil(t, dispatcher.lastCtx)
	require.NotNil(t, dispatcher.lastBody)
	require.NotEmpty(t, dispatcher.lastHint.ConversationID)
	require.Equal(t, kiro.ModelClaudeSonnet46, dispatcher.lastHint.Model)
	require.Contains(t, dispatcher.lastBody.ConversationState.CurrentMessage.UserInputMessage.Content, "<thinking_mode>extended</thinking_mode>")

	var got ChatCompletionResponse
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &got))
	require.Equal(t, "chatcmpl-test-id", got.ID)
	require.Equal(t, "chat.completion", got.Object)
	require.Equal(t, int64(1700000000), got.Created)
	require.Equal(t, "claude-sonnet-4.6", got.Model)
	require.Len(t, got.Choices, 1)
	require.Equal(t, "assistant", got.Choices[0].Message.Role)
	require.Equal(t, "hello", got.Choices[0].Message.Content.Text)
	require.Equal(t, "considering options", got.Choices[0].Message.ReasoningContent)
	require.Equal(t, "tool_calls", got.Choices[0].FinishReason)
	require.Len(t, got.Choices[0].Message.ToolCalls, 1)
	require.Equal(t, "tool-1", got.Choices[0].Message.ToolCalls[0].ID)
	require.Equal(t, "lookup", got.Choices[0].Message.ToolCalls[0].Function.Name)
	require.Equal(t, `{"city":"Paris"}`, got.Choices[0].Message.ToolCalls[0].Function.Arguments)
	require.Equal(t, Usage{PromptTokens: 11, CompletionTokens: 7, TotalTokens: 18}, got.Usage)
}

func TestHandlerNonStreamingOmitsReasoningWhenNotRequested(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dispatcher := &fakeDispatcher{
		onceFn: func(context.Context, *kiro.KiroPayload, account.SelectionHint) (kiro.FullResponse, error) {
			return kiro.FullResponse{Text: "hello", Thinking: "hidden", StopReason: "end_turn"}, nil
		},
	}

	r := gin.New()
	r.POST("/v1/chat/completions", Handler(HandlerOptions{Dispatcher: dispatcher}))

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"claude-sonnet-4.6","messages":[{"role":"user","content":"Hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	r.ServeHTTP(resp, req)

	require.Equal(t, http.StatusOK, resp.Code)
	var got ChatCompletionResponse
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &got))
	require.Empty(t, got.Choices[0].Message.ReasoningContent)
	require.Equal(t, "stop", got.Choices[0].FinishReason)
}

func TestHandlerStreamingResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dispatcher := &fakeDispatcher{
		streamFn: func(context.Context, *kiro.KiroPayload, account.SelectionHint) (<-chan kiro.StreamEvent, error) {
			ch := make(chan kiro.StreamEvent, 16)
			ch <- kiro.TextDelta{Text: "Hello"}
			ch <- kiro.ThinkingDelta{Text: "thinking"}
			ch <- kiro.ToolUseStart{ID: "tool-2", Name: "second"}
			ch <- kiro.ToolUseStart{ID: "tool-1", Name: "first"}
			ch <- kiro.ToolUseDelta{ID: "tool-2", InputDelta: `{"city"`}
			ch <- kiro.ToolUseDelta{ID: "tool-1", InputDelta: `{"q"`}
			ch <- kiro.ToolUseStop{ID: "tool-2"}
			ch <- kiro.Usage{InputTokens: 3, OutputTokens: 5}
			ch <- kiro.Stop{Reason: "tool_use"}
			close(ch)
			return ch, nil
		},
	}

	r := gin.New()
	r.POST("/v1/chat/completions", Handler(HandlerOptions{
		Dispatcher: dispatcher,
		Now:        func() time.Time { return time.Unix(1700000001, 0) },
		NewID:      func() string { return "stream-id" },
	}))

	body := `{"model":"claude-sonnet-4.6","stream":true,"reasoning_effort":"low","messages":[{"role":"user","content":"Hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	r.ServeHTTP(resp, req)

	require.Equal(t, http.StatusOK, resp.Code)
	require.Contains(t, resp.Header().Get("Content-Type"), "text/event-stream")
	require.Equal(t, "no-cache", resp.Header().Get("Cache-Control"))
	require.Equal(t, "keep-alive", resp.Header().Get("Connection"))
	require.Equal(t, "no", resp.Header().Get("X-Accel-Buffering"))

	events := splitEvents(resp.Body.String())
	require.Len(t, events, 9)

	role := parseSSE[ChatCompletionChunk](t, events[0])
	require.Equal(t, "assistant", role.Choices[0].Delta.Role)

	content := parseSSE[ChatCompletionChunk](t, events[1])
	require.Equal(t, "Hello", content.Choices[0].Delta.Content)

	reasoning := parseSSE[ChatCompletionChunk](t, events[2])
	require.Equal(t, "thinking", reasoning.Choices[0].Delta.ReasoningContent)

	start2 := parseSSE[ChatCompletionChunk](t, events[3]).Choices[0].Delta.ToolCalls[0]
	require.Equal(t, 0, start2.Index)
	require.Equal(t, "tool-2", start2.ID)
	require.Equal(t, "second", start2.Function.Name)

	start1 := parseSSE[ChatCompletionChunk](t, events[4]).Choices[0].Delta.ToolCalls[0]
	require.Equal(t, 1, start1.Index)
	require.Equal(t, "tool-1", start1.ID)
	require.Equal(t, "first", start1.Function.Name)

	delta2 := parseSSE[ChatCompletionChunk](t, events[5]).Choices[0].Delta.ToolCalls[0]
	require.Equal(t, 0, delta2.Index)
	require.Equal(t, `{"city"`, delta2.Function.Arguments)

	delta1 := parseSSE[ChatCompletionChunk](t, events[6]).Choices[0].Delta.ToolCalls[0]
	require.Equal(t, 1, delta1.Index)
	require.Equal(t, `{"q"`, delta1.Function.Arguments)

	final := parseSSE[ChatCompletionChunk](t, events[7])
	require.Equal(t, "tool_calls", final.Choices[0].FinishReason)
	require.Equal(t, &Usage{PromptTokens: 3, CompletionTokens: 5, TotalTokens: 8}, final.Usage)
	require.Equal(t, "data: [DONE]", events[8])
}

func TestHandlerInvalidRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/chat/completions", Handler(HandlerOptions{}))

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"messages":[{"role":"user","content":"Hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	r.ServeHTTP(resp, req)

	require.Equal(t, http.StatusBadRequest, resp.Code)
	var got struct {
		Error openAIError `json:"error"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &got))
	require.Equal(t, "invalid_request_error", got.Error.Code)
	require.Equal(t, http.StatusBadRequest, got.Error.Status)
	require.Contains(t, got.Error.Message, "model")
}
