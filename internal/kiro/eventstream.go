package kiro

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"strconv"
	"time"
)

const (
	eventStreamPreludeLen = 12
	eventStreamMinLen     = 16
	maxEventStreamFrame   = 16 << 20
)

var crc32cTable = crc32.IEEETable

// Parser incrementally decodes AWS Event Stream frames from chunked input.
type Parser struct {
	buf []byte
}

// Frame is one decoded AWS Event Stream message.
type Frame struct {
	Headers map[string]string
	Payload []byte
}

// Feed appends data and returns all complete frames currently available.
func (p *Parser) Feed(data []byte) ([]Frame, error) {
	if len(data) > 0 {
		p.buf = append(p.buf, data...)
	}

	var frames []Frame
	for {
		if len(p.buf) < eventStreamPreludeLen {
			return frames, nil
		}

		totalLength := int(binary.BigEndian.Uint32(p.buf[0:4]))
		headersLength := int(binary.BigEndian.Uint32(p.buf[4:8]))
		if totalLength < eventStreamMinLen || totalLength > maxEventStreamFrame || headersLength > totalLength-eventStreamMinLen {
			p.recoverBuffer()
			return frames, fmt.Errorf("invalid event stream prelude: total_length=%d headers_length=%d", totalLength, headersLength)
		}
		if len(p.buf) < totalLength {
			return frames, nil
		}

		frameBytes := p.buf[:totalLength]
		wantPreludeCRC := binary.BigEndian.Uint32(frameBytes[8:12])
		if got := crc32.Checksum(frameBytes[:8], crc32cTable); got != wantPreludeCRC {
			p.recoverBuffer()
			return frames, fmt.Errorf("event stream prelude crc mismatch: got %08x want %08x", got, wantPreludeCRC)
		}

		wantMessageCRC := binary.BigEndian.Uint32(frameBytes[totalLength-4:])
		if got := crc32.Checksum(frameBytes[:totalLength-4], crc32cTable); got != wantMessageCRC {
			p.buf = p.buf[totalLength:]
			p.recoverBuffer()
			return frames, fmt.Errorf("event stream message crc mismatch: got %08x want %08x", got, wantMessageCRC)
		}

		headersEnd := eventStreamPreludeLen + headersLength
		headers, err := parseEventStreamHeaders(frameBytes[eventStreamPreludeLen:headersEnd])
		if err != nil {
			p.buf = p.buf[totalLength:]
			p.recoverBuffer()
			return frames, err
		}

		payload := append([]byte(nil), frameBytes[headersEnd:totalLength-4]...)
		frames = append(frames, Frame{Headers: headers, Payload: payload})
		p.buf = p.buf[totalLength:]
	}
}

func (p *Parser) recoverBuffer() {
	idx := findValidPrelude(p.buf)
	if idx < 0 {
		if len(p.buf) > eventStreamPreludeLen-1 {
			p.buf = append([]byte(nil), p.buf[len(p.buf)-(eventStreamPreludeLen-1):]...)
		}
		return
	}
	p.buf = p.buf[idx:]
}

func findValidPrelude(buf []byte) int {
	for i := 0; i+eventStreamPreludeLen <= len(buf); i++ {
		totalLength := int(binary.BigEndian.Uint32(buf[i : i+4]))
		headersLength := int(binary.BigEndian.Uint32(buf[i+4 : i+8]))
		if totalLength < eventStreamMinLen || totalLength > maxEventStreamFrame || headersLength > totalLength-eventStreamMinLen {
			continue
		}
		want := binary.BigEndian.Uint32(buf[i+8 : i+12])
		if crc32.Checksum(buf[i:i+8], crc32cTable) == want {
			return i
		}
	}
	return -1
}

func parseEventStreamHeaders(data []byte) (map[string]string, error) {
	headers := make(map[string]string)
	for len(data) > 0 {
		nameLen := int(data[0])
		data = data[1:]
		if nameLen == 0 || len(data) < nameLen+1 {
			return nil, errors.New("malformed event stream header")
		}
		name := string(data[:nameLen])
		valueType := data[nameLen]
		data = data[nameLen+1:]

		value, rest, err := parseHeaderValue(valueType, data)
		if err != nil {
			return nil, fmt.Errorf("header %q: %w", name, err)
		}
		headers[name] = value
		data = rest
	}
	return headers, nil
}

func parseHeaderValue(valueType byte, data []byte) (string, []byte, error) {
	switch valueType {
	case 0:
		return "true", data, nil
	case 1:
		return "false", data, nil
	case 2:
		if len(data) < 1 {
			return "", nil, errors.New("short byte value")
		}
		return strconv.Itoa(int(int8(data[0]))), data[1:], nil
	case 3:
		if len(data) < 2 {
			return "", nil, errors.New("short int16 value")
		}
		return strconv.Itoa(int(int16(binary.BigEndian.Uint16(data[:2])))), data[2:], nil
	case 4:
		if len(data) < 4 {
			return "", nil, errors.New("short int32 value")
		}
		return strconv.FormatInt(int64(int32(binary.BigEndian.Uint32(data[:4]))), 10), data[4:], nil
	case 5:
		if len(data) < 8 {
			return "", nil, errors.New("short int64 value")
		}
		return strconv.FormatInt(int64(binary.BigEndian.Uint64(data[:8])), 10), data[8:], nil
	case 6:
		b, rest, err := readLenPrefixed(data)
		if err != nil {
			return "", nil, err
		}
		return hex.EncodeToString(b), rest, nil
	case 7:
		b, rest, err := readLenPrefixed(data)
		if err != nil {
			return "", nil, err
		}
		return string(b), rest, nil
	case 8:
		if len(data) < 8 {
			return "", nil, errors.New("short timestamp value")
		}
		millis := int64(binary.BigEndian.Uint64(data[:8]))
		return time.UnixMilli(millis).UTC().Format(time.RFC3339Nano), data[8:], nil
	case 9:
		if len(data) < 16 {
			return "", nil, errors.New("short uuid value")
		}
		b := data[:16]
		return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), data[16:], nil
	default:
		return "", nil, fmt.Errorf("unknown header value type %d", valueType)
	}
}

func readLenPrefixed(data []byte) ([]byte, []byte, error) {
	if len(data) < 2 {
		return nil, nil, errors.New("short length-prefixed value")
	}
	n := int(binary.BigEndian.Uint16(data[:2]))
	if len(data) < 2+n {
		return nil, nil, errors.New("truncated length-prefixed value")
	}
	return data[2 : 2+n], data[2+n:], nil
}

// DecodeEvent decodes a Kiro event frame using AWS Event Stream headers.
func DecodeEvent(headers map[string]string, payload []byte) (kind EventType, value any, err error) {
	messageType := headers[":message-type"]
	if messageType == "error" || messageType == "exception" {
		return EventType(headers[":event-type"]), nil, fmt.Errorf("kiro stream %s: %s %s", messageType, headers[":error-code"], headers[":error-message"])
	}

	kind = EventType(headers[":event-type"])
	switch kind {
	case EventAssistantResponse:
		var event AssistantResponseEvent
		if err := decodeEventPayload(payload, string(kind), &event); err != nil {
			return kind, nil, err
		}
		return kind, event, nil
	case EventToolUse:
		var event ToolUseEvent
		if err := decodeEventPayload(payload, string(kind), &event); err != nil {
			return kind, nil, err
		}
		return kind, event, nil
	case EventContextUsage:
		var event ContextUsageEvent
		if err := decodeEventPayload(payload, string(kind), &event); err != nil {
			return kind, nil, err
		}
		return kind, event, nil
	case EventMetering:
		var event map[string]any
		if len(bytes.TrimSpace(payload)) > 0 {
			if err := json.Unmarshal(payload, &event); err != nil {
				return kind, nil, fmt.Errorf("decode %s: %w", kind, err)
			}
		}
		return kind, event, nil
	default:
		return kind, append([]byte(nil), payload...), nil
	}
}

func decodeEventPayload(payload []byte, wrapper string, dst any) error {
	wrapped := map[string]json.RawMessage{}
	if err := json.Unmarshal(payload, &wrapped); err == nil {
		if raw, ok := wrapped[wrapper]; ok {
			if err := json.Unmarshal(raw, dst); err != nil {
				return fmt.Errorf("decode %s: %w", wrapper, err)
			}
			return nil
		}
	}
	if err := json.Unmarshal(payload, dst); err != nil {
		return fmt.Errorf("decode %s: %w", wrapper, err)
	}
	return nil
}
