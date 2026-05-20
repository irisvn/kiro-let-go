package kiro

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"

	"github.com/irisvn/kiro-let-go/internal/converter/chunks"
	"github.com/irisvn/kiro-let-go/internal/thinking"
)

const streamChannelCapacity = 64

// StreamEvent is the unified streaming event channel payload.
type StreamEvent any

type TextDelta = chunks.TextDelta
type ThinkingDelta = chunks.ThinkingDelta
type ToolUseStart = chunks.ToolUseStart
type ToolUseDelta = chunks.ToolUseDelta
type ToolUseStop = chunks.ToolUseStop
type Usage = chunks.Usage
type ContextUsage = chunks.ContextUsage
type Stop = chunks.Stop

// ErrorEvent reports a stream decoding failure.
type ErrorEvent struct {
	Err error
}

type toolBuilder struct {
	id      string
	name    string
	started bool
}

// StreamDecoder decodes Kiro AWS Event Stream responses into normalized events.
type StreamDecoder struct {
	parser       *Parser
	toolBuilders map[string]*toolBuilder
	logger       *slog.Logger
	estimator    *Estimator
}

// NewStreamDecoder creates a decoder with default parser and token estimator.
func NewStreamDecoder(logger *slog.Logger) *StreamDecoder {
	return &StreamDecoder{
		parser:       &Parser{},
		toolBuilders: make(map[string]*toolBuilder),
		logger:       logger,
		estimator:    &Estimator{},
	}
}

// Decode reads an AWS Event Stream body and emits normalized Kiro stream events.
// requestPayload is optional and, when present, is used to estimate input tokens.
func (d *StreamDecoder) Decode(ctx context.Context, body io.ReadCloser, requestPayload ...[]byte) <-chan StreamEvent {
	out := make(chan StreamEvent, streamChannelCapacity)
	if ctx == nil {
		ctx = context.Background()
	}

	go d.decode(ctx, body, out, requestPayload...)
	return out
}

func (d *StreamDecoder) decode(ctx context.Context, body io.ReadCloser, out chan<- StreamEvent, requestPayload ...[]byte) {
	var closeOnce sync.Once
	closeBody := func() {
		closeOnce.Do(func() { _ = body.Close() })
	}

	done := make(chan struct{})
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		select {
		case <-ctx.Done():
			closeBody()
		case <-done:
		}
	}()
	defer func() {
		close(done)
		<-watcherDone
		closeBody()
		close(out)
	}()

	parser := &Parser{}
	toolBuilders := make(map[string]*toolBuilder)
	estimator := d.estimator
	if estimator == nil {
		estimator = &Estimator{}
	}

	inputTokens := 0
	if len(requestPayload) > 0 {
		inputTokens = estimator.CountTokens(string(requestPayload[0]))
	}
	outputText := ""
	thinkingParser := thinking.NewParser(thinking.ModeRouteThinking)
	buf := make([]byte, 32*1024)

	for {
		select {
		case <-ctx.Done():
			closeBody()
			d.emit(out, Stop{Reason: "client_canceled"})
			return
		default:
		}

		n, err := body.Read(buf)
		if n > 0 {
			frames, feedErr := parser.Feed(buf[:n])
			if feedErr != nil {
				d.emit(out, ErrorEvent{Err: feedErr})
				return
			}
			for _, frame := range frames {
				if !d.handleFrame(out, frame, thinkingParser, &outputText, toolBuilders) {
					return
				}
			}
		}

		if err != nil {
			if errors.Is(err, io.EOF) {
				d.flushThinking(out, thinkingParser, &outputText)
				d.emit(out, Usage{InputTokens: inputTokens, OutputTokens: estimator.CountTokens(outputText)})
				d.emit(out, Stop{Reason: "end_turn"})
				return
			}
			select {
			case <-ctx.Done():
				d.emit(out, Stop{Reason: "client_canceled"})
			default:
				d.emit(out, ErrorEvent{Err: err})
			}
			return
		}
	}
}

func (d *StreamDecoder) handleFrame(out chan<- StreamEvent, frame Frame, p *thinking.Parser, outputText *string, toolBuilders map[string]*toolBuilder) bool {
	kind, value, err := DecodeEvent(frame.Headers, frame.Payload)
	if err != nil {
		_ = d.emit(out, ErrorEvent{Err: err})
		return false
	}

	switch kind {
	case EventAssistantResponse:
		event, ok := value.(AssistantResponseEvent)
		if !ok || event.Content == "" {
			return true
		}
		visible, thought := p.Feed(event.Content)
		if thought != "" {
			if !d.emit(out, ThinkingDelta{Text: thought}) {
				return false
			}
		}
		if visible != "" {
			*outputText += visible
			if !d.emit(out, TextDelta{Text: visible}) {
				return false
			}
		}
	case EventToolUse:
		event, ok := value.(ToolUseEvent)
		if !ok {
			return true
		}
		return d.handleToolUse(out, event, toolBuilders)
	case EventContextUsage:
		event, ok := value.(ContextUsageEvent)
		if ok {
			return d.emit(out, ContextUsage{Pct: event.ContextUsagePercentage})
		}
	case EventMetering:
		return true
	}
	return true
}

func (d *StreamDecoder) handleToolUse(out chan<- StreamEvent, event ToolUseEvent, toolBuilders map[string]*toolBuilder) bool {
	id := event.ToolUseID
	if id == "" {
		id = event.Name
	}
	b := toolBuilders[id]
	if b == nil {
		b = &toolBuilder{id: id}
		toolBuilders[id] = b
	}
	if event.Name != "" {
		b.name = event.Name
	}

	if event.Stop {
		ok := d.emit(out, ToolUseStop{ID: id})
		delete(toolBuilders, id)
		return ok
	}
	if !b.started && event.Name != "" {
		b.started = true
		return d.emit(out, ToolUseStart{ID: id, Name: event.Name})
	}
	if event.Input != "" {
		return d.emit(out, ToolUseDelta{ID: id, InputDelta: event.Input})
	}
	return true
}

func (d *StreamDecoder) flushThinking(out chan<- StreamEvent, p *thinking.Parser, outputText *string) {
	visible, thought := p.Close()
	if thought != "" {
		d.emit(out, ThinkingDelta{Text: thought})
	}
	if visible != "" {
		*outputText += visible
		d.emit(out, TextDelta{Text: visible})
	}
}

func (d *StreamDecoder) emit(out chan<- StreamEvent, event StreamEvent) bool {
	select {
	case out <- event:
		return true
	default:
		if d.logger != nil {
			d.logger.Warn("dropping kiro stream event because consumer channel is full")
		}
		return false
	}
}
