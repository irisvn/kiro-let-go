package kiro

import (
	"encoding/binary"
	"hash/crc32"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParserFeedChunkedFixture(t *testing.T) {
	stream, err := os.ReadFile("testdata/sample_stream.bin")
	require.NoError(t, err)
	require.NotEmpty(t, stream)

	var parser Parser
	var frames []Frame
	for i := 0; i < len(stream); i += 5 {
		end := min(i+5, len(stream))
		got, err := parser.Feed(stream[i:end])
		require.NoError(t, err)
		frames = append(frames, got...)
	}

	require.Len(t, frames, 3)
	require.Equal(t, "event", frames[0].Headers[":message-type"])
	require.Equal(t, "assistantResponseEvent", frames[0].Headers[":event-type"])
	require.Equal(t, "toolUseEvent", frames[1].Headers[":event-type"])
	require.Equal(t, "contextUsageEvent", frames[2].Headers[":event-type"])
}

func TestParserValidatesMessageCRCAndRecovers(t *testing.T) {
	bad := buildEventStreamFrame(t, map[string]string{
		":message-type": "event",
		":event-type":   "assistantResponseEvent",
	}, []byte(`{"content":"bad"}`))
	bad[len(bad)-1] ^= 0xff
	good := buildEventStreamFrame(t, map[string]string{
		":message-type": "event",
		":event-type":   "assistantResponseEvent",
	}, []byte(`{"content":"ok"}`))

	var parser Parser
	frames, err := parser.Feed(append(bad, good...))
	require.Error(t, err)
	require.Empty(t, frames)

	frames, err = parser.Feed(nil)
	require.NoError(t, err)
	require.Len(t, frames, 1)
	require.JSONEq(t, `{"content":"ok"}`, string(frames[0].Payload))
}

func TestParserValidatesPreludeCRCAndScansForward(t *testing.T) {
	bad := buildEventStreamFrame(t, map[string]string{
		":message-type": "event",
		":event-type":   "assistantResponseEvent",
	}, []byte(`{"content":"bad"}`))
	bad[11] ^= 0xff
	good := buildEventStreamFrame(t, map[string]string{
		":message-type": "event",
		":event-type":   "assistantResponseEvent",
	}, []byte(`{"content":"ok"}`))

	var parser Parser
	frames, err := parser.Feed(append(bad, good...))
	require.Error(t, err)
	require.Empty(t, frames)

	frames, err = parser.Feed(nil)
	require.NoError(t, err)
	require.Len(t, frames, 1)
}

func TestDecodeEvent(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]string
		payload []byte
		kind    EventType
		want    any
		wantErr bool
	}{
		{
			name: "assistant direct payload",
			headers: map[string]string{
				":message-type": "event",
				":event-type":   "assistantResponseEvent",
			},
			payload: []byte(`{"content":"hello"}`),
			kind:    EventAssistantResponse,
			want:    AssistantResponseEvent{Content: "hello"},
		},
		{
			name: "tool wrapped payload",
			headers: map[string]string{
				":message-type": "event",
				":event-type":   "toolUseEvent",
			},
			payload: []byte(`{"toolUseEvent":{"name":"shell","toolUseId":"tu_1","input":"{}","stop":true}}`),
			kind:    EventToolUse,
			want:    ToolUseEvent{Name: "shell", ToolUseID: "tu_1", Input: "{}", Stop: true},
		},
		{
			name: "context usage",
			headers: map[string]string{
				":message-type": "event",
				":event-type":   "contextUsageEvent",
			},
			payload: []byte(`{"contextUsagePercentage":12.5}`),
			kind:    EventContextUsage,
			want:    ContextUsageEvent{ContextUsagePercentage: 12.5},
		},
		{
			name: "error message",
			headers: map[string]string{
				":message-type":  "error",
				":event-type":    "assistantResponseEvent",
				":error-code":    "BadRequest",
				":error-message": "nope",
			},
			kind:    EventAssistantResponse,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, got, err := DecodeEvent(tt.headers, tt.payload)
			require.Equal(t, tt.kind, kind)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func buildEventStreamFrame(t *testing.T, headers map[string]string, payload []byte) []byte {
	t.Helper()
	headerBytes := make([]byte, 0)
	for name, value := range headers {
		require.LessOrEqual(t, len(name), 255)
		require.LessOrEqual(t, len(value), 65535)
		headerBytes = append(headerBytes, byte(len(name)))
		headerBytes = append(headerBytes, name...)
		headerBytes = append(headerBytes, 7)
		headerBytes = binary.BigEndian.AppendUint16(headerBytes, uint16(len(value)))
		headerBytes = append(headerBytes, value...)
	}

	totalLength := 12 + len(headerBytes) + len(payload) + 4
	frame := make([]byte, 0, totalLength)
	frame = binary.BigEndian.AppendUint32(frame, uint32(totalLength))
	frame = binary.BigEndian.AppendUint32(frame, uint32(len(headerBytes)))
	frame = binary.BigEndian.AppendUint32(frame, crc32.Checksum(frame[:8], crc32cTable))
	frame = append(frame, headerBytes...)
	frame = append(frame, payload...)
	frame = binary.BigEndian.AppendUint32(frame, crc32.Checksum(frame, crc32cTable))
	return frame
}
