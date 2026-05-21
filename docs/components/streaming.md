# Streaming

## Tổng quan

Module streaming cung cấp 2 SSE writer (Anthropic format và OpenAI format) cùng 1 FSM parser để xử lý thinking tag. Các writer chịu trách nhiệm serialize `NormalizedChunk` thành server-sent events đúng định dạng từng provider.

---

## Anthropic SSE Writer

`SSEWriter` trong `internal/api/anthropic/sse.go` serialize event theo Anthropic Messages API streaming format. Nó enforce state machine per-message:

```
message_start → content_block_start → content_block_delta (xN) → content_block_stop → message_delta → message_stop
```

### State enforcement

- `WriteMessageStart` chỉ thành công đúng 1 lần. Gọi lần thứ hai trả về lỗi.
- `WriteContentBlockDelta(index)` reject nếu block chưa được `WriteContentBlockStart` hoặc đã bị `WriteContentBlockStop`.
- `WriteContentBlockStop` reject nếu block không đang mở.
- Mỗi delta cũng được validate type: text block cần `TextDelta`, thinking block cần `ThinkingDelta`, tool_use block cần `InputJSONDelta`.

### Thread safety

Toàn bộ public methods lock `sync.Mutex`. Sau mỗi event, writer gọi `http.Flusher.Flush()` để đảm bảo client nhận ngay.

### Methods

| Method | Event emitted | Ghi chú |
|--------|---------------|---------|
| `WriteMessageStart(inputTokens)` | `message_start` | Kèm `Usage{InputTokens}` |
| `WriteContentBlockStart(index, blockType, toolID, toolName)` | `content_block_start` | `blockType`: `text`, `thinking`, `tool_use` |
| `WriteContentBlockDelta(index, delta)` | `content_block_delta` | `TextDelta`, `ThinkingDelta`, hoặc `InputJSONDelta` |
| `WriteContentBlockStop(index)` | `content_block_stop` | Đánh dấu block đóng |
| `WriteMessageDelta(stopReason)` | `message_delta` | Kèm accumulated `outputTokens` |
| `WriteMessageStop()` | `message_stop` | Kết thúc message |
| `WritePing()` | `ping` | Keep-alive event |
| `WriteError(err)` | `error` | Map `errs.Error` sang `ErrorDetail` |

### Required headers

Handler set các header sau trước khi stream:

```go
headers.Set("Content-Type", "text/event-stream")
headers.Set("Cache-Control", "no-cache")
headers.Set("Connection", "keep-alive")
headers.Set("X-Accel-Buffering", "no")
```

---

## OpenAI SSE Writer

`SSEWriter` trong `internal/api/openai/sse.go` serialize event theo OpenAI `chat.completion.chunk` format.

### Format

Mỗi chunk là một SSE event với prefix `data: `, body là JSON object có shape:

```json
{
  "id": "...",
  "object": "chat.completion.chunk",
  "created": 1234567890,
  "model": "...",
  "choices": [{"index": 0, "delta": {...}, "finish_reason": null}]
}
```

### Methods

| Method | Delta field | Ghi chú |
|--------|-------------|---------|
| `WriteContentDelta(text)` | `delta.content` | Text fragment |
| `WriteReasoningDelta(text)` | `delta.reasoning_content` | Reasoning fragment |
| `WriteToolCallStart(toolUseID, name)` | `delta.tool_calls[{index, id, type:"function", function:{name, arguments:""}}]` | Tạo index mới |
| `WriteToolCallArgsDelta(toolUseID, argsDelta)` | `delta.tool_calls[{index, function:{arguments}}]` | Chỉ gửi arguments delta |
| `WriteToolCallStop(toolUseID)` | (no-op) | OpenAI không có per-call stop event |
| `WriteFinalChunk(finishReason, usage)` | `finish_reason` + optional `usage` | Chunk terminal |
| `WriteDone()` | `[DONE]` | Sentinel bắt buộc cuối stream |
| `WriteError(err)` | `error` object + `[DONE]` | |

### Tool call indexing

Writer duy trì `toolCallIndex map[string]int` để gán index ổn định cho mỗi `toolUseID`:

- Chunk đầu tiên của một tool call chứa đầy đủ metadata: `index`, `id`, `type`, `function.name`, `function.arguments`.
- Các chunk sau chỉ gửi `arguments` delta.

### Final chunk

`WriteFinalChunk` populate `finish_reason` (ví dụ `"stop"`, `"tool_calls"`, `"length"`). Sau đó `WriteDone()` gửi sentinel `[DONE]`.

Tất cả chunks chia sẻ cùng `id`, `created`, `model`, và `object="chat.completion.chunk"`.

---

## Thinking Tag Parser (FSM)

`Parser` trong `internal/thinking/parser.go` là finite-state machine tách thinking block khỏi visible text trong stream response từ Kiro.

### Modes

| Mode | Hành vi |
|------|---------|
| `ModeRouteThinking` | Tách thinking và visible thành 2 luồng riêng biệt |
| `ModeStripTags` | Bỏ tag nhưng giữ nội dung thinking như visible text |
| `ModePassThrough` | Không xử lý, trả về nguyên chunk |

### States

| State | Ý nghĩa |
|-------|---------|
| `PreContent` | Chưa thấy nội dung thực sự (chỉ whitespace) |
| `InThinking` | Đang bên trong `<thinking>...</thinking>` |
| `Streaming` | Đã qua thinking block, phát visible text trực tiếp |

### Tag recognition

Các opening tag được nhận diện: `<thinking>`, `<think>`, `<reasoning>`, `<thought>`. Chỉ được chấp nhận ở very start của response (sau khi bỏ whitespace đầu). Closing tag phải khớp với opening tag (ví dụ mở bằng `<thinking>` thì đóng bằng `</thinking>`).

### Cautious buffering

Khi output kết thúc giữa chừng một tag, parser giữ tối đa `retainBytes` (bằng `len("</reasoning>") + 2`) trong buffer. Điều này tránh cắt nhầm tag khi chunk boundary rơi giữa tag:

```go
const retainBytes = len("</reasoning>") + 2
```

### Methods

| Method | Return | Mô tả |
|--------|--------|-------|
| `Feed(chunk string) (visible, thinking string)` | visible text + thinking text | Xử lý một chunk, có thể emit một phần |
| `Close() (visible, thinking string)` | flush cuối | Trả về bytes còn trong buffer |

### Routing output

- Thinking text → `index=0` thinking blocks (Anthropic SSE) hoặc `delta.reasoning_content` (OpenAI SSE).
- Visible text → text blocks / content delta thông thường.

---

## Periodic ping (keep-alive)

Anthropic handler chạy một goroutine riêng gửi `ping` event mỗi 25 giây khi không có event nào được gửi. Day la co che keep-alive de tranh client timeout khi Kiro khong gui event trong thoi gian dai.

```go
const anthropicPingInterval = 25 * time.Second
```

Cơ chế hoạt động:
- Mỗi khi có event từ Kiro, `activity` channel nhận signal và timer được reset.
- Nếu timer hết hạn mà không có activity, `WritePing()` được gọi.
- Goroutine dừng khi nhận signal từ `done` channel hoặc `context.Done()`.

```go
go h.runPingLoop(c.Request.Context(), writer, activity, done, pingDone)
```

## CRC32-IEEE trong Event Stream

Event stream parser dung CRC32-IEEE (da fix tu CRC32C Castagnoli). Xem chi tiet tai [kiro-protocol.md](kiro-protocol.md).
