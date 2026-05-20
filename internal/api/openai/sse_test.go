package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/irisvn/kiro-let-go/internal/errs"
	"github.com/stretchr/testify/require"
)

type flushBuffer struct {
	strings.Builder
	flushes int
}

func (f *flushBuffer) Flush() { f.flushes++ }

type errWriter struct{ err error }

func (w *errWriter) Write([]byte) (int, error) { return 0, w.err }

type flushErrorResponseWriter struct {
	header   http.Header
	status   int
	flushes  int
	flushErr error
	body     bytes.Buffer
}

func (w *flushErrorResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *flushErrorResponseWriter) Write(data []byte) (int, error) { return w.body.Write(data) }

func (w *flushErrorResponseWriter) WriteHeader(status int) { w.status = status }

func (w *flushErrorResponseWriter) Flush() { w.flushes++ }

func (w *flushErrorResponseWriter) FlushError() error {
	w.flushes++
	return w.flushErr
}

func parseSSE[T any](t *testing.T, event string) T {
	t.Helper()
	require.True(t, strings.HasPrefix(event, "data: "))
	var out T
	require.NoError(t, json.Unmarshal([]byte(strings.TrimPrefix(event, "data: ")), &out))
	return out
}

func splitEvents(s string) []string {
	s = strings.TrimSuffix(s, "\n\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n\n")
}

func captureDefaultLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() {
		slog.SetDefault(old)
	})
	return &buf
}

func TestSSEWriter_ContentReasoningFinalDone(t *testing.T) {
	buf := &flushBuffer{}
	w := NewSSEWriter(buf, buf, "chatcmpl-test", "gpt-test", 123)

	w.WriteContentDelta("hello")
	w.WriteReasoningDelta("thinking")
	w.WriteFinalChunk("stop", &Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3})
	w.WriteDone()

	events := splitEvents(buf.String())
	require.Len(t, events, 4)
	require.Equal(t, 4, buf.flushes)

	content := parseSSE[ChatCompletionChunk](t, events[0])
	require.Equal(t, "chatcmpl-test", content.ID)
	require.Equal(t, "chat.completion.chunk", content.Object)
	require.Equal(t, int64(123), content.Created)
	require.Equal(t, "gpt-test", content.Model)
	require.Equal(t, "hello", content.Choices[0].Delta.Content)

	reasoning := parseSSE[ChatCompletionChunk](t, events[1])
	require.Equal(t, "thinking", reasoning.Choices[0].Delta.ReasoningContent)

	final := parseSSE[ChatCompletionChunk](t, events[2])
	require.Equal(t, "stop", final.Choices[0].FinishReason)
	require.Equal(t, &Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3}, final.Usage)
	require.Equal(t, "data: [DONE]", events[3])
}

func TestSSEWriter_ToolCallStreamingIndexes(t *testing.T) {
	buf := &flushBuffer{}
	w := NewSSEWriter(buf, buf, "id", "model", 456)

	w.WriteToolCallStart("tool-2", "second")
	w.WriteToolCallStart("tool-1", "first")
	w.WriteToolCallArgsDelta("tool-2", `{"city"`)
	w.WriteToolCallArgsDelta("tool-1", `{"q"`)
	w.WriteToolCallStop("tool-2")
	w.WriteToolCallArgsDelta("tool-2", `:"Paris"}`)

	events := splitEvents(buf.String())
	require.Len(t, events, 5)

	start2 := parseSSE[ChatCompletionChunk](t, events[0]).Choices[0].Delta.ToolCalls[0]
	require.Equal(t, 0, start2.Index)
	require.Equal(t, "tool-2", start2.ID)
	require.Equal(t, "function", start2.Type)
	require.Equal(t, "second", start2.Function.Name)
	require.Empty(t, start2.Function.Arguments)

	start1 := parseSSE[ChatCompletionChunk](t, events[1]).Choices[0].Delta.ToolCalls[0]
	require.Equal(t, 1, start1.Index)
	require.Equal(t, "tool-1", start1.ID)
	require.Equal(t, "first", start1.Function.Name)

	delta2 := parseSSE[ChatCompletionChunk](t, events[2]).Choices[0].Delta.ToolCalls[0]
	require.Equal(t, 0, delta2.Index)
	require.Empty(t, delta2.ID)
	require.Empty(t, delta2.Type)
	require.Empty(t, delta2.Function.Name)
	require.Equal(t, `{"city"`, delta2.Function.Arguments)

	delta1 := parseSSE[ChatCompletionChunk](t, events[3]).Choices[0].Delta.ToolCalls[0]
	require.Equal(t, 1, delta1.Index)
	require.Equal(t, `{"q"`, delta1.Function.Arguments)

	delta2Again := parseSSE[ChatCompletionChunk](t, events[4]).Choices[0].Delta.ToolCalls[0]
	require.Equal(t, 0, delta2Again.Index)
	require.Equal(t, `:"Paris"}`, delta2Again.Function.Arguments)
}

func TestSSEWriter_WriteErrorThenDone(t *testing.T) {
	buf := &flushBuffer{}
	w := NewSSEWriter(buf, buf, "id", "model", 789)

	w.WriteError(&errs.Error{Class: errs.ClassRateLimited, Code: "rate_limit_exceeded", HTTPStatus: 429, Message: "slow down"})

	events := splitEvents(buf.String())
	require.Len(t, events, 2)
	require.Equal(t, 2, buf.flushes)

	var body struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
			Status  int    `json:"status"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimPrefix(events[0], "data: ")), &body))
	require.Equal(t, "slow down", body.Error.Message)
	require.Equal(t, "rate_limit_exceeded", body.Error.Type)
	require.Equal(t, "rate_limit_exceeded", body.Error.Code)
	require.Equal(t, 429, body.Error.Status)
	require.Equal(t, "data: [DONE]", events[1])
}

func TestSSEWriter_LogsWriteError(t *testing.T) {
	logs := captureDefaultLogs(t)
	w := NewSSEWriter(&errWriter{err: errors.New("boom")}, nil, "id", "model", 1)

	w.WriteDone()

	require.Contains(t, logs.String(), "failed to write SSE chunk")
	require.Contains(t, logs.String(), "boom")
}

func TestSSEWriter_SuppressesExpectedDisconnectLogs(t *testing.T) {
	logs := captureDefaultLogs(t)
	w := NewSSEWriter(&errWriter{err: io.ErrClosedPipe}, nil, "id", "model", 1)

	w.WriteDone()

	require.Empty(t, logs.String())

	logs.Reset()
	w = NewSSEWriter(&errWriter{err: context.Canceled}, nil, "id", "model", 1)
	w.WriteDone()
	require.Empty(t, logs.String())
}

func TestSSEWriter_LogsFlushError(t *testing.T) {
	logs := captureDefaultLogs(t)
	rw := &flushErrorResponseWriter{flushErr: errors.New("flush boom")}
	w := NewSSEWriter(rw, rw, "id", "model", 1)

	w.WriteDone()

	require.Contains(t, logs.String(), "failed to flush SSE chunk")
	require.Contains(t, logs.String(), "flush boom")
}
