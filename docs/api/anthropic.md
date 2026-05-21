# Anthropic API Surface

## Endpoints

| Method | Path | Mo ta |
|--------|------|-------|
| POST | `/v1/messages` | Gui message den Claude, ho tro streaming va non-streaming |
| POST | `/v1/messages/count_tokens` | Uoc luong so token input (local, khong goi Kiro upstream) |

## Authentication

Proxy chap nhan mot trong hai header:

```
Authorization: Bearer <ProxyAPIKey>
x-api-key: <ProxyAPIKey>
```

Neu thieu hoac sai key, tra 401 voi `authentication_error`.

## Request shape

`MessagesRequest` — body cua `POST /v1/messages`:

| Field | Type | Bat buoc | Mo ta |
|-------|------|----------|-------|
| `model` | string | Co | Ten model, phai chua "claude" (case-insensitive) |
| `messages` | array | Co | Danh sach message, moi message co `role` va `content` |
| `system` | string hoac array | Khong | System prompt — co the la string hoac array content blocks |
| `max_tokens` | int | Co | > 0 |
| `stream` | bool | Khong | Mac dinh `false`. Neu `true`, tra SSE stream |
| `temperature` | float | Khong | Sampling temperature |
| `top_p` | float | Khong | Nucleus sampling |
| `top_k` | int | Khong | Top-k sampling |
| `stop_sequences` | array string | Khong | Cac chuoi dung generation |
| `tools` | array | Khong | Danh sach tool dinh nghia |
| `tool_choice` | object | Khong | Dieu khien cach model dung tool |
| `thinking` | object | Khong | `{ "type": "enabled", "budget_tokens": <int> }` |
| `metadata` | object | Khong | `{ "user_id": "..." }` |

## Polymorphic content

`MessageContent` chap nhan hai dang:

1. **String** — plain text:
```json
{"role": "user", "content": "Hello"}
```

2. **Array of content blocks** — text, image, tool_use, tool_result, thinking:
```json
{
  "role": "user",
  "content": [
    {"type": "text", "text": "Hello"},
    {"type": "image", "source": {"type": "base64", "media_type": "image/png", "data": "..."}}
  ]
}
```

`SystemField` cung ho tro ca string va array content blocks tuong tu.

## Validation

- `model` khong duoc de trong va phai chua "claude" (case-insensitive). Cac model khong phai Claude se bi reject.
- `max_tokens` phai > 0.
- `messages` khong duoc rong.
- Moi message phai co `role` la `"user"` hoac `"assistant"`.

Neu validation that bai, tra 400 voi `invalid_request_error`.

## Smart model normalization

Truoc khi gui den Kiro, model name duoc normalize tu dong:

- Strip prefix sau `/`: `kiro/claude-sonnet-4-6` → `claude-sonnet-4.6`.
- Fix separators: `4-6` → `4.6`, `_` → `-`.
- Case insensitive.

Client co the gui `kiro/claude-sonnet-4-6` va server se tu dong chuyen thanh `claude-sonnet-4.6` truoc khi goi Kiro.

## Streaming response (SSE)

Khi `stream: true`, server tra `text/event-stream`. State machine cua stream:

```
message_start
  -> content_block_start
  -> content_block_delta (co the lap lai nhieu lan)
  -> content_block_stop
  -> ... (lap lai cho moi block)
  -> message_delta
  -> message_stop
```

Cac event co the xuat hien them:
- `ping` — keep-alive event (moi 25 giay neu khong co activity).
- `error` — loi trong qua trinh stream.

Vi du event:

```
event: message_start
data: {"type":"message_start","message":{"id":"msg_...","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4.6","usage":{"input_tokens":10,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}

event: message_stop
data: {"type":"message_stop"}
```

## Non-streaming response

`MessagesResponse`:

```json
{
  "id": "msg_...",
  "type": "message",
  "role": "assistant",
  "content": [
    {"type": "text", "text": "..."}
  ],
  "model": "claude-sonnet-4.6",
  "stop_reason": "end_turn",
  "usage": {"input_tokens": 10, "output_tokens": 5}
}
```

`content` la mang cac `ContentBlock` (text, thinking, tool_use, ...).

## Stop reasons

| Stop reason | Y nghia |
|-------------|---------|
| `end_turn` | Model tu ket thuc turn |
| `tool_use` | Model chon goi tool |
| `max_tokens` | Dat gioi han `max_tokens` |

## Token counting endpoint

`POST /v1/messages/count_tokens` tra:

```json
{"input_tokens": 42}
```

Day la uoc luong local (dung `kiro.Estimator`), khong goi len Kiro upstream.

## Curl examples

### Non-streaming

```bash
curl http://localhost:8765/v1/messages \
  -H "Authorization: Bearer <ProxyAPIKey>" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-sonnet-4.6",
    "messages": [{"role": "user", "content": "Hello"}],
    "max_tokens": 1024
  }'
```

### Streaming

```bash
curl http://localhost:8765/v1/messages \
  -H "Authorization: Bearer <ProxyAPIKey>" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-sonnet-4.6",
    "messages": [{"role": "user", "content": "Hello"}],
    "max_tokens": 1024,
    "stream": true
  }'
```
