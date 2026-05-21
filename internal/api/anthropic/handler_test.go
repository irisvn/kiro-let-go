package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/irisvn/kiro-let-go/internal/account"
	"github.com/irisvn/kiro-let-go/internal/errs"
	"github.com/irisvn/kiro-let-go/internal/kiro"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandlerPostMessages_NonStreaming(t *testing.T) {
	t.Cleanup(overrideHandlerDeps(t))
	handler := NewHandler(&kiro.Dispatcher{}, &kiro.Estimator{}, nil)
	handler.dispatcherOnce = func(_ *kiro.Dispatcher, _ context.Context, _ *kiro.KiroPayload, _ account.SelectionHint) (kiro.FullResponse, error) {
		return kiro.FullResponse{
			Thinking:   "plan",
			Text:       "hello world",
			ToolUses:   []kiro.ToolUseEntry{{ToolUseID: "toolu_01", Name: "lookup", Input: json.RawMessage(`{"q":"golang"}`)}},
			Usage:      kiro.Usage{OutputTokens: 9},
			StopReason: "end_turn",
		}, nil
	}
	handler.newMessageID = func() string { return "msg_test_nonstream" }

	r := gin.New()
	handler.Register(r)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewBufferString(`{"model":"claude-sonnet-4.6","messages":[{"role":"user","content":"hello"}],"max_tokens":64}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp MessagesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "msg_test_nonstream", resp.ID)
	assert.Equal(t, "message", resp.Type)
	assert.Equal(t, "assistant", resp.Role)
	assert.Equal(t, "claude-sonnet-4.6", resp.Model)
	require.NotNil(t, resp.StopReason)
	assert.Equal(t, "tool_use", *resp.StopReason)
	assert.Positive(t, resp.Usage.InputTokens)
	assert.Equal(t, 9, resp.Usage.OutputTokens)
	require.Len(t, resp.Content, 3)
	assert.Equal(t, ThinkingBlock{Type: "thinking", Thinking: "plan", Signature: ""}, resp.Content[0])
	assert.Equal(t, TextBlock{Type: "text", Text: "hello world", CacheControl: nil}, resp.Content[1])
	tool, ok := resp.Content[2].(ToolUseBlock)
	require.True(t, ok)
	assert.Equal(t, "toolu_01", tool.ID)
	assert.Equal(t, "lookup", tool.Name)
	assert.JSONEq(t, `{"q":"golang"}`, string(tool.Input))
}

func TestHandlerPostMessages_Streaming(t *testing.T) {
	t.Cleanup(overrideHandlerDeps(t))
	handler := NewHandler(&kiro.Dispatcher{}, &kiro.Estimator{}, nil)
	handler.newMessageID = func() string { return "msg_test_stream" }
	handler.pingInterval = 5 * time.Millisecond
	handler.dispatcherStream = func(_ *kiro.Dispatcher, _ context.Context, _ *kiro.KiroPayload, _ account.SelectionHint) (<-chan kiro.StreamEvent, *kiro.StreamMeta, error) {
		ch := make(chan kiro.StreamEvent, 8)
		go func() {
			defer close(ch)
			ch <- kiro.ThinkingDelta{Text: "plan"}
			time.Sleep(15 * time.Millisecond)
			ch <- kiro.TextDelta{Text: "hello"}
			ch <- kiro.ToolUseStart{ID: "toolu_01", Name: "lookup"}
			ch <- kiro.ToolUseDelta{ID: "toolu_01", InputDelta: `{"q":`}
			ch <- kiro.ToolUseDelta{ID: "toolu_01", InputDelta: `"golang"}`}
			ch <- kiro.ToolUseStop{ID: "toolu_01"}
			ch <- kiro.Stop{Reason: "end_turn"}
		}()
		return ch, nil, nil
	}

	r := gin.New()
	handler.Register(r)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewBufferString(`{"model":"claude-sonnet-4.6","messages":[{"role":"user","content":"hello"}],"max_tokens":64,"stream":true}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
	assert.Equal(t, "no-cache", rec.Header().Get("Cache-Control"))
	assert.Equal(t, "keep-alive", rec.Header().Get("Connection"))
	assert.Equal(t, "no", rec.Header().Get("X-Accel-Buffering"))
	assert.NotContains(t, rec.Body.String(), "DONE")

	events := parseSSE(t, rec.Body.String())
	require.NotEmpty(t, events)
	assert.Equal(t, "message_start", events[0].name)
	assert.Contains(t, eventNames(events), "ping")

	var messageStart MessageStartEvent
	require.NoError(t, json.Unmarshal(events[0].data, &messageStart))
	assert.Equal(t, "msg_test_stream", messageStart.Message.ID)
	assert.Equal(t, "claude-sonnet-4.6", messageStart.Message.Model)
	assert.Positive(t, messageStart.Message.Usage.InputTokens)

	filtered := withoutPing(events)
	require.Len(t, filtered, 13)
	assert.Equal(t, "content_block_start", filtered[1].name)
	assert.Equal(t, "content_block_delta", filtered[2].name)
	assert.Equal(t, "content_block_start", filtered[3].name)
	assert.Equal(t, "content_block_delta", filtered[4].name)
	assert.Equal(t, "content_block_start", filtered[5].name)
	assert.Equal(t, "content_block_delta", filtered[6].name)
	assert.Equal(t, "content_block_delta", filtered[7].name)
	assert.Equal(t, "content_block_stop", filtered[8].name)
	assert.Equal(t, "content_block_stop", filtered[9].name)
	assert.Equal(t, "content_block_stop", filtered[10].name)
	assert.Equal(t, "message_delta", filtered[11].name)
	assert.Equal(t, "message_stop", filtered[12].name)

	var messageDelta MessageDeltaEvent
	require.NoError(t, json.Unmarshal(filtered[11].data, &messageDelta))
	require.NotNil(t, messageDelta.Delta.StopReason)
	assert.Equal(t, "tool_use", *messageDelta.Delta.StopReason)

	var toolDelta ContentBlockDeltaEvent
	require.NoError(t, json.Unmarshal(filtered[6].data, &toolDelta))
	assert.Equal(t, `{"q":`, toolDelta.Delta.(InputJSONDelta).PartialJSON)
}

func TestHandlerPostMessages_StreamingDispatcherError(t *testing.T) {
	t.Cleanup(overrideHandlerDeps(t))
	handler := NewHandler(&kiro.Dispatcher{}, &kiro.Estimator{}, nil)
	handler.newMessageID = func() string { return "msg_test_error" }
	handler.dispatcherStream = func(_ *kiro.Dispatcher, _ context.Context, _ *kiro.KiroPayload, _ account.SelectionHint) (<-chan kiro.StreamEvent, *kiro.StreamMeta, error) {
		return nil, nil, errs.New(errs.ClassRateLimited, "", "slow down")
	}

	r := gin.New()
	handler.Register(r)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewBufferString(`{"model":"claude-sonnet-4.6","messages":[{"role":"user","content":"hello"}],"max_tokens":64,"stream":true}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	events := parseSSE(t, rec.Body.String())
	require.Len(t, events, 2)
	assert.Equal(t, "message_start", events[0].name)
	assert.Equal(t, "error", events[1].name)
	assert.NotContains(t, rec.Body.String(), "event: message_stop")

	var errEvent ErrorEvent
	require.NoError(t, json.Unmarshal(events[1].data, &errEvent))
	assert.Equal(t, "rate_limited", errEvent.Error.Type)
	assert.Equal(t, "slow down", errEvent.Error.Message)
}

func TestHandlerPostMessages_StreamingClientDisconnect(t *testing.T) {
	t.Cleanup(overrideHandlerDeps(t))
	handler := NewHandler(&kiro.Dispatcher{}, &kiro.Estimator{}, nil)
	handler.newMessageID = func() string { return "msg_test_disconnect" }
	started := make(chan struct{})
	stopped := make(chan struct{})
	handler.dispatcherStream = func(_ *kiro.Dispatcher, ctx context.Context, _ *kiro.KiroPayload, _ account.SelectionHint) (<-chan kiro.StreamEvent, *kiro.StreamMeta, error) {
		close(started)
		ch := make(chan kiro.StreamEvent, 1)
		ch <- kiro.TextDelta{Text: "hello"}
		go func() {
			defer close(stopped)
			<-ctx.Done()
			close(ch)
		}()
		return ch, nil, nil
	}

	r := gin.New()
	handler.Register(r)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewBufferString(`{"model":"claude-sonnet-4.6","messages":[{"role":"user","content":"hello"}],"max_tokens":64,"stream":true}`)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")

	handlerDone := make(chan struct{})
	go func() {
		defer close(handlerDone)
		r.ServeHTTP(rec, req)
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("dispatcher stream was not started")
	}
	cancel()

	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("stream context was not canceled")
	}
	select {
	case <-handlerDone:
	case <-time.After(time.Second):
		t.Fatal("handler did not exit after client disconnect")
	}
	assert.NotContains(t, rec.Body.String(), "event: message_stop")
}

func TestHandlerPostCountTokens(t *testing.T) {
	t.Cleanup(overrideHandlerDeps(t))

	r := gin.New()
	NewHandler(&kiro.Dispatcher{}, &kiro.Estimator{}, nil).Register(r)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", bytes.NewBufferString(`{"model":"claude-sonnet-4.6","system":"You are helpful.","messages":[{"role":"user","content":"hello"}],"tools":[{"name":"lookup","description":"Lookup data","input_schema":{"type":"object"}}]}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp CountTokensResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Positive(t, resp.InputTokens)
	assert.Contains(t, rec.Body.String(), "input_tokens")
}

func overrideHandlerDeps(t *testing.T) func() {
	t.Helper()
	gin.SetMode(gin.TestMode)
	return func() {}
}

func eventNames(events []sseEvent) []string {
	names := make([]string, 0, len(events))
	for _, event := range events {
		names = append(names, event.name)
	}
	return names
}

func withoutPing(events []sseEvent) []sseEvent {
	filtered := make([]sseEvent, 0, len(events))
	for _, event := range events {
		if event.name != "ping" {
			filtered = append(filtered, event)
		}
	}
	return filtered
}
