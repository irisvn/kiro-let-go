package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"syscall"

	"github.com/irisvn/kiro-let-go/internal/errs"
)

type flushErrorReporter interface {
	FlushError() error
}

// SSEWriter writes OpenAI-compatible chat.completion.chunk server-sent events.
type SSEWriter struct {
	w             io.Writer
	flusher       http.Flusher
	controller    *http.ResponseController
	mu            sync.Mutex
	id, model     string
	created       int64
	toolCallIndex map[string]int
	nextIndex     int
}

// NewSSEWriter creates a writer for OpenAI streaming responses.
func NewSSEWriter(w io.Writer, flusher http.Flusher, id, model string, created int64) *SSEWriter {
	if flusher == nil {
		if f, ok := w.(http.Flusher); ok {
			flusher = f
		}
	}

	var controller *http.ResponseController
	if rw, ok := w.(http.ResponseWriter); ok {
		controller = http.NewResponseController(rw)
	}

	return &SSEWriter{
		w:             w,
		flusher:       flusher,
		controller:    controller,
		id:            id,
		model:         model,
		created:       created,
		toolCallIndex: make(map[string]int),
	}
}

// WriteContentDelta emits a content delta chunk.
func (s *SSEWriter) WriteContentDelta(text string) {
	s.writeChunk(ChatCompletionChunk{
		Choices: []ChunkChoice{{Index: 0, Delta: Delta{Content: text}}},
	})
}

// WriteReasoningDelta emits a reasoning_content delta chunk.
func (s *SSEWriter) WriteReasoningDelta(text string) {
	s.writeChunk(ChatCompletionChunk{
		Choices: []ChunkChoice{{Index: 0, Delta: Delta{ReasoningContent: text}}},
	})
}

// WriteToolCallStart assigns an index and emits the initial tool call metadata.
func (s *SSEWriter) WriteToolCallStart(toolUseID, name string) {
	s.mu.Lock()
	index := s.indexForLocked(toolUseID)
	s.writeChunkLocked(ChatCompletionChunk{
		Choices: []ChunkChoice{{
			Index: 0,
			Delta: Delta{ToolCalls: []ChunkToolCall{{
				Index: index,
				ID:    toolUseID,
				Type:  "function",
				Function: ChunkFunction{
					Name:      name,
					Arguments: "",
				},
			}}},
		}},
	})
	s.mu.Unlock()
}

// WriteToolCallArgsDelta emits an incremental function arguments delta.
func (s *SSEWriter) WriteToolCallArgsDelta(toolUseID, argsDelta string) {
	s.mu.Lock()
	index := s.indexForLocked(toolUseID)
	s.writeChunkLocked(ChatCompletionChunk{
		Choices: []ChunkChoice{{
			Index: 0,
			Delta: Delta{ToolCalls: []ChunkToolCall{{
				Index:    index,
				Function: ChunkFunction{Arguments: argsDelta},
			}}},
		}},
	})
	s.mu.Unlock()
}

// WriteToolCallStop is intentionally a no-op; OpenAI has no per-call stop event.
func (s *SSEWriter) WriteToolCallStop(toolUseID string) {}

// WriteFinalChunk emits the terminal chunk before the DONE sentinel.
func (s *SSEWriter) WriteFinalChunk(finishReason string, usage *Usage) {
	s.writeChunk(ChatCompletionChunk{
		Choices: []ChunkChoice{{Index: 0, Delta: Delta{}, FinishReason: finishReason}},
		Usage:   usage,
	})
}

// WriteDone emits the OpenAI stream completion sentinel.
func (s *SSEWriter) WriteDone() {
	s.writeSSE([]byte("[DONE]"))
}

// WriteError emits an OpenAI-format error payload followed by DONE.
func (s *SSEWriter) WriteError(err *errs.Error) {
	type errorBody struct {
		Error openAIError `json:"error"`
	}

	msg := "error"
	code := "error"
	status := 0
	if err != nil {
		msg = err.Message
		code = err.Code
		status = err.HTTPStatus
		if code == "" {
			code = err.Class.String()
		}
	}

	b, marshalErr := json.Marshal(errorBody{Error: openAIError{
		Message: msg,
		Type:    code,
		Code:    code,
		Status:  status,
	}})
	if marshalErr == nil {
		s.writeSSE(b)
	}
	s.WriteDone()
}

type openAIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
	Status  int    `json:"status,omitempty"`
}

func (s *SSEWriter) writeChunk(chunk ChatCompletionChunk) {
	s.mu.Lock()
	s.writeChunkLocked(chunk)
	s.mu.Unlock()
}

func (s *SSEWriter) writeChunkLocked(chunk ChatCompletionChunk) {
	chunk.ID = s.id
	chunk.Object = "chat.completion.chunk"
	chunk.Created = s.created
	chunk.Model = s.model

	b, err := json.Marshal(chunk)
	if err != nil {
		return
	}
	s.writeSSELocked(b)
}

func (s *SSEWriter) writeSSE(payload []byte) {
	s.mu.Lock()
	s.writeSSELocked(payload)
	s.mu.Unlock()
}

func (s *SSEWriter) writeSSELocked(payload []byte) {
	if _, err := s.w.Write([]byte("data: ")); err != nil {
		logWriteError(payload, err)
		return
	}
	if _, err := s.w.Write(payload); err != nil {
		logWriteError(payload, err)
		return
	}
	if _, err := s.w.Write([]byte("\n\n")); err != nil {
		logWriteError(payload, err)
		return
	}
	if s.controller != nil {
		if err := s.controller.Flush(); err != nil {
			logWriteError(nil, err)
		}
		return
	}
	if s.flusher != nil {
		s.flusher.Flush()
		if reporter, ok := s.flusher.(flushErrorReporter); ok {
			if err := reporter.FlushError(); err != nil {
				logWriteError(nil, err)
			}
		}
	}
}

func logWriteError(payload []byte, err error) {
	if err == nil || isExpectedDisconnect(err) {
		return
	}
	message := "failed to write SSE chunk"
	attrs := []any{"error", err}
	if payload == nil {
		message = "failed to flush SSE chunk"
	} else {
		attrs = append(attrs, "payload_bytes", len(payload))
	}
	slog.Warn(message, attrs...)
}

func isExpectedDisconnect(err error) bool {
	return errors.Is(err, io.ErrClosedPipe) || errors.Is(err, context.Canceled) || errors.Is(err, syscall.EPIPE)
}

func (s *SSEWriter) indexForLocked(toolUseID string) int {
	if s.toolCallIndex == nil {
		s.toolCallIndex = make(map[string]int)
	}
	if index, ok := s.toolCallIndex[toolUseID]; ok {
		return index
	}
	index := s.nextIndex
	s.toolCallIndex[toolUseID] = index
	s.nextIndex++
	return index
}
