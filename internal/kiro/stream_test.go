package kiro

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

type closeTrackingReader struct {
	*bytes.Reader
	closed bool
}

func (r *closeTrackingReader) Close() error {
	r.closed = true
	return nil
}

func TestStreamDecoderDecodeTranslatesEvents(t *testing.T) {
	stream := appendFrames(t,
		[]byte(`{"content":"<thinking>plan</thinking>Hi "}`), EventAssistantResponse,
		[]byte(`{"content":"there"}`), EventAssistantResponse,
		[]byte(`{"name":"shell","toolUseId":"tu_1"}`), EventToolUse,
		[]byte(`{"toolUseId":"tu_1","input":"{\"cmd\":"}`), EventToolUse,
		[]byte(`{"toolUseId":"tu_1","input":"\"ls\"}"}`), EventToolUse,
		[]byte(`{"toolUseId":"tu_1","stop":true}`), EventToolUse,
		[]byte(`{"contextUsagePercentage":42.5}`), EventContextUsage,
		[]byte(`{"tokens":123}`), EventMetering,
	)
	body := &closeTrackingReader{Reader: bytes.NewReader(stream)}

	events := collectEvents(NewStreamDecoder(nil).Decode(context.Background(), body, []byte(`{"prompt":"hello"}`)))

	require.True(t, body.closed)
	require.Len(t, events, 10)
	require.Equal(t, ThinkingDelta{Text: "plan"}, events[0])
	require.Equal(t, TextDelta{Text: "Hi "}, events[1])
	require.Equal(t, TextDelta{Text: "there"}, events[2])
	require.Equal(t, ToolUseStart{ID: "tu_1", Name: "shell"}, events[3])
	require.Equal(t, ToolUseDelta{ID: "tu_1", InputDelta: `{"cmd":`}, events[4])
	require.Equal(t, ToolUseDelta{ID: "tu_1", InputDelta: `"ls"}`}, events[5])
	require.Equal(t, ToolUseStop{ID: "tu_1"}, events[6])
	require.Equal(t, ContextUsage{Pct: 42.5}, events[7])
	usage, ok := events[8].(Usage)
	require.True(t, ok)
	require.Positive(t, usage.InputTokens)
	require.Positive(t, usage.OutputTokens)
	require.Equal(t, Stop{Reason: "end_turn"}, events[9])
}

func TestStreamDecoderDecodeEmitsStopOnCleanEOF(t *testing.T) {
	stream := appendFrames(t, []byte(`{"content":"done"}`), EventAssistantResponse)
	events := collectEvents(NewStreamDecoder(nil).Decode(context.Background(), io.NopCloser(bytes.NewReader(stream))))

	require.Len(t, events, 3)
	require.Equal(t, TextDelta{Text: "done"}, events[0])
	require.IsType(t, Usage{}, events[1])
	require.Equal(t, Stop{Reason: "end_turn"}, events[2])
}

func TestStreamDecoderDecodeCancelClosesBodyAndStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	body := &closeTrackingReader{Reader: bytes.NewReader(nil)}

	events := collectEvents(NewStreamDecoder(nil).Decode(ctx, body))

	require.True(t, body.closed)
	require.Equal(t, []StreamEvent{Stop{Reason: "client_canceled"}}, events)
}

func TestStreamDecoderDecodeParserError(t *testing.T) {
	bad := buildEventStreamFrame(t, map[string]string{
		":message-type": "event",
		":event-type":   "assistantResponseEvent",
	}, []byte(`{"content":"bad"}`))
	bad[len(bad)-1] ^= 0xff

	events := collectEvents(NewStreamDecoder(nil).Decode(context.Background(), io.NopCloser(bytes.NewReader(bad))))

	require.Len(t, events, 1)
	_, ok := events[0].(ErrorEvent)
	require.True(t, ok)
}

func TestEstimatorCountTokens(t *testing.T) {
	var estimator Estimator
	require.Zero(t, estimator.CountTokens(""))
	require.Positive(t, estimator.CountTokens("hello world"))
}

func collectEvents(ch <-chan StreamEvent) []StreamEvent {
	var events []StreamEvent
	for event := range ch {
		events = append(events, event)
	}
	return events
}

func appendFrames(t *testing.T, args ...any) []byte {
	t.Helper()
	require.Zero(t, len(args)%2)
	var stream []byte
	for i := 0; i < len(args); i += 2 {
		payload, ok := args[i].([]byte)
		require.True(t, ok)
		kind, ok := args[i+1].(EventType)
		require.True(t, ok)
		stream = append(stream, buildEventStreamFrame(t, map[string]string{
			":message-type": "event",
			":event-type":   string(kind),
		}, payload)...)
	}
	return stream
}
