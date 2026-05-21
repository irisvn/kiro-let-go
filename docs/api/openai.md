# OpenAI API Surface

## Endpoints

| Method | Path | Mo ta |
|--------|------|-------|
| POST | `/v1/chat/completions` | Chat completions, ho tro streaming va non-streaming |
| GET | `/v1/models` | Tra danh sach model duoc ho tro |

## Authentication

Chi chap nhan:

```
Authorization: Bearer <ProxyAPIKey>
```

Neu thieu hoac sai key, tra 401.

## Request shape

`ChatCompletionRequest` — body cua `POST /v1/chat/completions`:

| Field | Type | Bat buoc | Mo ta |
|-------|------|----------|-------|
| `model` | string | Co | Phai chua "claude" (Claude-only proxy) |
| `messages` | array | Co | Danh sach message |
| `max_tokens` | int | Khong | Max output tokens |
| `temperature` | float | Khong | Sampling temperature |
| `top_p` | float | Khong | Nucleus sampling |
| `stream` | bool | Khong | Mac dinh `false` |
| `stop` | array string | Khong | Stop sequences |
| `tools` | array | Khong | Danh sach tool (OpenAI format) |
| `tool_choice` | string | Khong | `"auto"`, `"none"`, hoac ten function |
| `reasoning_effort` | string | Khong | `"none"`, `"minimal"`, `"low"`, `"medium"`, `"high"`, `"xhigh"` |
| `response_format` | object | Khong | `{ "type": "json_object" }` hoac JSON schema |
| `user` | string | Khong | User identifier |

## Polymorphic content

`MessageContent` chap nhan hai dang:

1. **String** — plain text:
```json
{"role": "user", "content": "Hello"}
```

2. **Array of content_part** — text hoac image_url:
```json
{
  "role": "user",
  "content": [
    {"type": "text", "text": "Hello"},
    {"type": "image_url", "image_url": {"url": "data:image/png;base64,..."}}
  ]
}
```

## Image URLs

Chi chap nhan **data URL** (base64). HTTP/HTTPS URLs bi reject voi loi `IMAGE_URL_UNSUPPORTED`.

## Validation

- `model` khong duoc de trong va phai chua "claude" (case-insensitive). Cac model khong phai Claude se bi reject.
- `messages` khong duoc rong.
- Valid roles: `"system"`, `"user"`, `"assistant"`, `"tool"`.
- Message co `role: "tool"` bat buoc phai co `tool_call_id`.

## Smart model normalization

Truoc khi gui den Kiro, model name duoc normalize tu dong (giong nhu Anthropic handler):

- Strip prefix sau `/`: `kiro/claude-sonnet-4-6` → `claude-sonnet-4.6`.
- Fix separators: `4-6` → `4.6`, `_` → `-`.
- Case insensitive.

## Streaming response

Khi `stream: true`, server tra `text/event-stream`. Moi chunk co `object: "chat.completion.chunk"`:

```
data: {"id":"chatcmpl-...","object":"chat.completion.chunk","created":...,"model":"...","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":""}]}

data: {"id":"chatcmpl-...","object":"chat.completion.chunk","created":...,"model":"...","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":""}]}

...

data: {"id":"chatcmpl-...","object":"chat.completion.chunk","created":...,"model":"...","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]
```

Cac field trong `delta`:
- `content` — text fragment
- `reasoning_content` — thinking fragment (chi khi `reasoning_effort` duoc set)
- `tool_calls` — tool call metadata va arguments

## Tool calling

- Moi tool call duoc gan mot `index` duy nhat, map tu `toolUseID`.
- `arguments` trong streaming la JSON-encoded string, duoc gui ting phan qua nhieu chunk.
- Final response co `tool_calls` day du voi `id`, `type: "function"`, `function.name`, va `function.arguments`.

## Reasoning effort mapping

`reasoning_effort` duoc map sang `thinking_budget_tokens`:

| `reasoning_effort` | Budget tokens |
|-------------------|---------------|
| `none`, `minimal` | 0 |
| `low` | 2000 |
| `medium` | 4000 |
| `high` | 8000 |
| `xhigh` | 10000 |

## Models endpoint

`GET /v1/models` tra hardcoded list 6 Claude models:

```json
{
  "object": "list",
  "data": [
    {"id": "claude-sonnet-4.5", "object": "model", "created": 1735689600, "owned_by": "kiro"},
    {"id": "claude-sonnet-4.6", "object": "model", "created": 1735689600, "owned_by": "kiro"},
    {"id": "claude-opus-4.5", "object": "model", "created": 1735689600, "owned_by": "kiro"},
    {"id": "claude-opus-4.6", "object": "model", "created": 1735689600, "owned_by": "kiro"},
    {"id": "claude-opus-4.7", "object": "model", "created": 1735689600, "owned_by": "kiro"},
    {"id": "claude-haiku-4.5", "object": "model", "created": 1735689600, "owned_by": "kiro"}
  ]
}
```

## Finish reason mapping

| Kiro stop reason | OpenAI `finish_reason` |
|------------------|------------------------|
| `end_turn` | `stop` |
| `tool_use` | `tool_calls` |
| `max_tokens` | `length` |

## Curl examples

### Non-streaming

```bash
curl http://localhost:8765/v1/chat/completions \
  -H "Authorization: Bearer <ProxyAPIKey>" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-sonnet-4.6",
    "messages": [{"role": "user", "content": "Hello"}]
  }'
```

### Streaming

```bash
curl http://localhost:8765/v1/chat/completions \
  -H "Authorization: Bearer <ProxyAPIKey>" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-sonnet-4.6",
    "messages": [{"role": "user", "content": "Hello"}],
    "stream": true
  }'
```
