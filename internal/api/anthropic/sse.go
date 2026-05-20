package anthropic

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/irisvn/kiro-let-go/internal/errs"
)

// SSEWriter serializes Anthropic Messages API server-sent events and enforces
// the per-message stream state machine. It does not auto-close content blocks;
// callers must explicitly stop every block they start.
type SSEWriter struct {
	w                io.Writer
	flusher          http.Flusher
	mu               sync.Mutex
	messageID        string
	model            string
	sentMessageStart bool
	openBlocks       map[int]string
	outputTokens     int
}

// NewSSEWriter creates an Anthropic stream writer. If w also implements
// http.Flusher, every event is flushed immediately after it is written.
func NewSSEWriter(w io.Writer, messageID, model string) *SSEWriter {
	flusher, _ := w.(http.Flusher)
	return &SSEWriter{
		w:          w,
		flusher:    flusher,
		messageID:  messageID,
		model:      model,
		openBlocks: make(map[int]string),
	}
}

// WriteMessageStart emits the initial message_start event. It succeeds once.
func (s *SSEWriter) WriteMessageStart(inputTokens int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.sentMessageStart {
		return fmt.Errorf("anthropic sse: message_start already sent")
	}
	s.sentMessageStart = true

	return s.writeEvent("message_start", MessageStartEvent{
		Type: "message_start",
		Message: MessagesResponse{
			ID:      s.messageID,
			Type:    "message",
			Role:    "assistant",
			Content: []ContentBlock{},
			Model:   s.model,
			Usage:   Usage{InputTokens: inputTokens, OutputTokens: 0},
		},
	})
}

// WriteContentBlockStart emits content_block_start and marks the block open.
// blockType supports text, thinking, and tool_use.
func (s *SSEWriter) WriteContentBlockStart(index int, blockType, optionalToolID, optionalToolName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.sentMessageStart {
		return fmt.Errorf("anthropic sse: message_start must be sent before content blocks")
	}
	if _, ok := s.openBlocks[index]; ok {
		return fmt.Errorf("anthropic sse: content block %d already open", index)
	}

	block, err := contentBlockForStart(blockType, optionalToolID, optionalToolName)
	if err != nil {
		return err
	}
	s.openBlocks[index] = blockType

	return s.writeEvent("content_block_start", ContentBlockStartEvent{
		Type:         "content_block_start",
		Index:        index,
		ContentBlock: block,
	})
}

// WriteContentBlockDelta emits content_block_delta. It rejects deltas for
// blocks that are not currently open, including blocks that were already
// stopped.
func (s *SSEWriter) WriteContentBlockDelta(index int, delta Delta) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	blockType, ok := s.openBlocks[index]
	if !ok {
		return fmt.Errorf("anthropic sse: content block %d is not open or was already stopped", index)
	}
	if err := validateDeltaForBlock(blockType, delta); err != nil {
		return err
	}
	s.outputTokens++

	return s.writeEvent("content_block_delta", ContentBlockDeltaEvent{
		Type:  "content_block_delta",
		Index: index,
		Delta: delta,
	})
}

// WriteContentBlockStop emits content_block_stop and marks the block stopped.
func (s *SSEWriter) WriteContentBlockStop(index int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.openBlocks[index]; !ok {
		return fmt.Errorf("anthropic sse: content block %d is not open or was already stopped", index)
	}
	delete(s.openBlocks, index)

	return s.writeEvent("content_block_stop", ContentBlockStopEvent{Type: "content_block_stop", Index: index})
}

// WriteMessageDelta emits message_delta with the accumulated output token
// count. stopReason is omitted when empty.
func (s *SSEWriter) WriteMessageDelta(stopReason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var reason *string
	if stopReason != "" {
		reason = &stopReason
	}
	return s.writeEvent("message_delta", MessageDeltaEvent{
		Type:  "message_delta",
		Delta: MessageDelta{StopReason: reason},
		Usage: Usage{OutputTokens: s.outputTokens},
	})
}

// WriteMessageStop emits message_stop.
func (s *SSEWriter) WriteMessageStop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.writeEvent("message_stop", MessageStopEvent{Type: "message_stop"})
}

// WritePing emits a ping keep-alive event.
func (s *SSEWriter) WritePing() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.writeEvent("ping", PingEvent{Type: "ping"})
}

// WriteError emits an Anthropic-format error event.
func (s *SSEWriter) WriteError(err *errs.Error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.writeEvent("error", ErrorEvent{
		Type: "error",
		Error: ErrorDetail{
			Type:    errorClass(err),
			Message: errorMessage(err),
		},
	})
}

func (s *SSEWriter) writeEvent(name string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", name, data); err != nil {
		return err
	}
	if s.flusher != nil {
		s.flusher.Flush()
	}
	return nil
}

func contentBlockForStart(blockType, toolID, toolName string) (ContentBlock, error) {
	switch blockType {
	case "text":
		return TextBlock{Type: "text", Text: ""}, nil
	case "thinking":
		return ThinkingBlock{Type: "thinking", Thinking: "", Signature: ""}, nil
	case "tool_use":
		return ToolUseBlock{Type: "tool_use", ID: toolID, Name: toolName, Input: json.RawMessage(`{}`)}, nil
	default:
		return nil, fmt.Errorf("anthropic sse: unsupported content block type %q", blockType)
	}
}

func validateDeltaForBlock(blockType string, delta Delta) error {
	if delta == nil {
		return fmt.Errorf("anthropic sse: delta is nil")
	}
	switch blockType {
	case "text":
		if _, ok := delta.(TextDelta); !ok {
			return fmt.Errorf("anthropic sse: text block requires TextDelta")
		}
	case "thinking":
		if _, ok := delta.(ThinkingDelta); !ok {
			return fmt.Errorf("anthropic sse: thinking block requires ThinkingDelta")
		}
	case "tool_use":
		if _, ok := delta.(InputJSONDelta); !ok {
			return fmt.Errorf("anthropic sse: tool_use block requires InputJSONDelta")
		}
	}
	return nil
}

func errorClass(err *errs.Error) string {
	if err == nil {
		return "fatal"
	}
	if err.Code != "" {
		return err.Code
	}
	switch err.Class {
	case errs.ClassRecoverable:
		return "recoverable"
	case errs.ClassFatal:
		return "fatal"
	case errs.ClassQuotaExhausted:
		return "quota_exhausted"
	case errs.ClassAuthExpired:
		return "auth_expired"
	case errs.ClassRateLimited:
		return "rate_limited"
	case errs.ClassContentTooLong:
		return "content_too_long"
	case errs.ClassNetwork:
		return "network"
	case errs.ClassClientCanceled:
		return "client_canceled"
	default:
		return "fatal"
	}
}

func errorMessage(err *errs.Error) string {
	if err == nil || err.Message == "" {
		return "stream error"
	}
	return err.Message
}
