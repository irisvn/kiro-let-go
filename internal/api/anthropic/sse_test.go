package anthropic

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/irisvn/kiro-let-go/internal/errs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type flushBuffer struct {
	bytes.Buffer
	flushes int
}

func (b *flushBuffer) Flush() { b.flushes++ }

func TestSSEWriter_WritesAnthropicEventsAndFlushes(t *testing.T) {
	buf := &flushBuffer{}
	w := NewSSEWriter(buf, "msg_01", "claude-3-sonnet")

	require.NoError(t, w.WriteMessageStart(11))
	require.NoError(t, w.WriteContentBlockStart(0, "thinking", "", ""))
	require.NoError(t, w.WriteContentBlockDelta(0, ThinkingDelta{Type: "thinking_delta", Thinking: "plan"}))
	require.NoError(t, w.WriteContentBlockStop(0))
	require.NoError(t, w.WriteContentBlockStart(1, "text", "", ""))
	require.NoError(t, w.WriteContentBlockDelta(1, TextDelta{Type: "text_delta", Text: "hello"}))
	require.NoError(t, w.WriteContentBlockStop(1))
	require.NoError(t, w.WriteContentBlockStart(2, "tool_use", "toolu_01", "lookup"))
	require.NoError(t, w.WriteContentBlockDelta(2, InputJSONDelta{Type: "input_json_delta", PartialJSON: `{"q":`}))
	require.NoError(t, w.WriteContentBlockStop(2))
	require.NoError(t, w.WriteMessageDelta("end_turn"))
	require.NoError(t, w.WriteMessageStop())
	require.NoError(t, w.WritePing())
	require.NoError(t, w.WriteError(errs.New(errs.ClassRateLimited, "", "slow down")))

	events := parseSSE(t, buf.String())
	require.Len(t, events, 14)
	assert.Equal(t, 14, buf.flushes)

	assert.Equal(t, "message_start", events[0].name)
	var messageStart MessageStartEvent
	require.NoError(t, json.Unmarshal(events[0].data, &messageStart))
	assert.Equal(t, "msg_01", messageStart.Message.ID)
	assert.Equal(t, "claude-3-sonnet", messageStart.Message.Model)
	assert.Equal(t, 11, messageStart.Message.Usage.InputTokens)

	assert.Equal(t, "content_block_start", events[1].name)
	var thinkingStart ContentBlockStartEvent
	require.NoError(t, json.Unmarshal(events[1].data, &thinkingStart))
	assert.Equal(t, 0, thinkingStart.Index)
	assert.Equal(t, "thinking", thinkingStart.ContentBlock.(ThinkingBlock).Type)

	var thinkingDelta ContentBlockDeltaEvent
	require.NoError(t, json.Unmarshal(events[2].data, &thinkingDelta))
	assert.Equal(t, "plan", thinkingDelta.Delta.(ThinkingDelta).Thinking)

	var textDelta ContentBlockDeltaEvent
	require.NoError(t, json.Unmarshal(events[5].data, &textDelta))
	assert.Equal(t, "hello", textDelta.Delta.(TextDelta).Text)

	var toolStart ContentBlockStartEvent
	require.NoError(t, json.Unmarshal(events[7].data, &toolStart))
	tool := toolStart.ContentBlock.(ToolUseBlock)
	assert.Equal(t, "toolu_01", tool.ID)
	assert.Equal(t, "lookup", tool.Name)
	assert.JSONEq(t, `{}`, string(tool.Input))

	var toolDelta ContentBlockDeltaEvent
	require.NoError(t, json.Unmarshal(events[8].data, &toolDelta))
	assert.Equal(t, `{"q":`, toolDelta.Delta.(InputJSONDelta).PartialJSON)

	var messageDelta MessageDeltaEvent
	require.NoError(t, json.Unmarshal(events[10].data, &messageDelta))
	require.NotNil(t, messageDelta.Delta.StopReason)
	assert.Equal(t, "end_turn", *messageDelta.Delta.StopReason)
	assert.Equal(t, 3, messageDelta.Usage.OutputTokens)

	assert.Equal(t, "message_stop", events[11].name)
	assert.Equal(t, "ping", events[12].name)
	var errorEvent ErrorEvent
	require.NoError(t, json.Unmarshal(events[13].data, &errorEvent))
	assert.Equal(t, "rate_limited", errorEvent.Error.Type)
	assert.Equal(t, "slow down", errorEvent.Error.Message)
}

func TestSSEWriter_StateEnforcement(t *testing.T) {
	w := NewSSEWriter(&flushBuffer{}, "msg_01", "claude")

	require.NoError(t, w.WriteMessageStart(1))
	require.ErrorContains(t, w.WriteMessageStart(1), "already sent")
	require.ErrorContains(t, w.WriteContentBlockDelta(0, TextDelta{Type: "text_delta", Text: "x"}), "not open")
	require.NoError(t, w.WriteContentBlockStart(1, "text", "", ""))
	require.ErrorContains(t, w.WriteContentBlockStart(1, "text", "", ""), "already open")
	require.ErrorContains(t, w.WriteContentBlockDelta(1, ThinkingDelta{Type: "thinking_delta", Thinking: "x"}), "TextDelta")
	require.NoError(t, w.WriteContentBlockDelta(1, TextDelta{Type: "text_delta", Text: "x"}))
	require.NoError(t, w.WriteContentBlockStop(1))
	require.ErrorContains(t, w.WriteContentBlockDelta(1, TextDelta{Type: "text_delta", Text: "y"}), "already stopped")
	require.ErrorContains(t, w.WriteContentBlockStop(1), "already stopped")
	require.ErrorContains(t, w.WriteContentBlockStart(2, "image", "", ""), "unsupported")
}

func TestSSEWriter_SerializesConcurrentWrites(t *testing.T) {
	buf := &flushBuffer{}
	w := NewSSEWriter(buf, "msg_01", "claude")
	require.NoError(t, w.WriteMessageStart(1))

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			assert.NoError(t, w.WritePing())
		}()
	}
	wg.Wait()

	events := parseSSE(t, buf.String())
	require.Len(t, events, 21)
	assert.Equal(t, 21, buf.flushes)
	for _, event := range events[1:] {
		assert.Equal(t, "ping", event.name)
		assert.JSONEq(t, `{"type":"ping"}`, string(event.data))
	}
}

func TestSSEWriter_DoesNotRequireFlusher(t *testing.T) {
	buf := &bytes.Buffer{}
	w := NewSSEWriter(buf, "msg_01", "claude")
	require.NoError(t, w.WritePing())
	assert.Contains(t, buf.String(), "event: ping\n")
}

type sseEvent struct {
	name string
	data json.RawMessage
}

func parseSSE(t *testing.T, stream string) []sseEvent {
	t.Helper()
	chunks := strings.Split(strings.TrimSpace(stream), "\n\n")
	events := make([]sseEvent, 0, len(chunks))
	for _, chunk := range chunks {
		lines := strings.Split(chunk, "\n")
		require.Len(t, lines, 2, chunk)
		require.True(t, strings.HasPrefix(lines[0], "event: "), chunk)
		require.True(t, strings.HasPrefix(lines[1], "data: "), chunk)
		events = append(events, sseEvent{
			name: strings.TrimPrefix(lines[0], "event: "),
			data: json.RawMessage(strings.TrimPrefix(lines[1], "data: ")),
		})
	}
	return events
}
