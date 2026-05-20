# Kiro Protocol Client

## Tổng quan

Kiro su dung mot proprietary AWS Event Stream binary protocol de streaming responses tu upstream. Proxy nay phai parse tung binary frame, decode payload JSON ben trong, roi chuyen doi thanh cac event streaming thong nhat (TextDelta, ToolUse, v.v.) ma cac client OpenAI/Anthropic co the hieu duoc.

Toan bo pipeline bao gom: build `KiroPayload` → gui HTTP POST den AWS endpoint → nhan response chunked binary → parse AWS Event Stream frames → decode thanh `StreamEvent` → forward den client.

---

## Authentication methods

Proxy ho tro hai phuong thuc xac thuc voi Kiro upstream.

| | Social (refresh token) | API Key (`ksk_*`) |
|---|---|---|
| **Token refresh** | Co, goi endpoint `https://prod.{region}.auth.desktop.kiro.dev/refreshToken` | No-op (khong can refresh) |
| **Token lifetime** | ~1 gio (expiresIn tu response) | ~100 nam (hardcoded) |
| **Error classification** | `INVALID_REFRESH_TOKEN` hoac `invalid_grant` → `ClassFatal` | Shape khong dung `ksk_*` → `ClassFatal` |
| | 401/403 → `ClassFatal` | |
| | 429/5xx → `ClassRecoverable` | |
| **Header dac biet** | Khong | `tokentype: API_KEY` |

Social refresh request body chi chua mot field duy nhat:

```json
{"refreshToken": "..."}
```

Response tra ve `accessToken`, `refreshToken` (optional), va `expiresIn`.

API Key duoc validate bang prefix `ksk_`. Neu khong khop, `APIKeyAuth.Refresh` tra ve loai fatal ngay lap tuc.

---

## Request payload structure

`KiroPayload` la root object gui len Kiro:

```go
type KiroPayload struct {
    ConversationState ConversationState `json:"conversationState"`
    ProfileArn        string            `json:"profileArn,omitempty"`
}
```

`ConversationState` chua day du context cua cuoc hoi thoai:

```go
type ConversationState struct {
    ConversationID      string         `json:"conversationId"`
    AgentContinuationID string         `json:"agentContinuationId,omitempty"`
    AgentTaskType       string         `json:"agentTaskType,omitempty"`
    ChatTriggerType     string         `json:"chatTriggerType,omitempty"`
    CurrentMessage      CurrentMessage `json:"currentMessage"`
    History             []HistoryItem  `json:"history,omitempty"`
}
```

Cac truong quan trong:

- `conversationId`: dinh danh cuoc hoi thoai, Kiro dung de tracking context.
- `currentMessage.userInputMessage.content`: prompt cua user.
- `currentMessage.userInputMessage.modelId`: model Kiro (vi du `claude-sonnet-4.6`).
- `history`: mang cac turn truoc do, co the la `UserInputMessage` hoac `AssistantResponseMessage`.
- `profileArn`: optional, neu account co gia tri nay thi se duoc inject vao payload.

---

## AWS Event Stream parser

### Frame layout

Moi message trong AWS Event Stream co cau truc nhi phan nhu sau:

```
[total_length      u32]  // 4 bytes, big-endian
[headers_length    u32]  // 4 bytes, big-endian
[prelude_crc       u32]  // 4 bytes, CRC32C cua 8 bytes tren
[headers...            ]  // chieu dai = headers_length
[payload...            ]  // chieu dai = total_length - headers_length - 16
[message_crc       u32]  // 4 bytes, CRC32C cua toan bo frame tru 4 bytes cuoi
```

Prelude co dinh 12 bytes (`eventStreamPreludeLen`). Toi thieu mot frame can 16 bytes (`eventStreamMinLen`). Max frame size la 16 MiB (`maxEventStreamFrame`).

### Header value types

Parser ho tro day du cac header value types theo AWS Event Stream spec:

| Type | Mo ta |
|------|-------|
| 0 | boolean true |
| 1 | boolean false |
| 2 | signed byte |
| 3 | signed int16 |
| 4 | signed int32 |
| 5 | signed int64 |
| 6 | byte array (length-prefixed, hex-encoded) |
| 7 | string (length-prefixed) |
| 8 | timestamp (int64 millis, RFC3339Nano) |
| 9 | UUID (16 bytes) |

### CRC32C validation

Ca prelude CRC va message CRC deu duoc validate bang CRC32C (Castagnoli table):

```go
var crc32cTable = crc32.MakeTable(crc32.Castagnoli)
```

Neu checksum khong khop, parser se skip frame hien tai va thu `recoverBuffer()`. Recovery tim kiem trong buffer con lai de tim mot prelude hop le (co total_length, headers_length hop ly va prelude CRC dung). Neu tim thay, parser tiep tuc tu vi tri do. Neu khong, giu lai toi da 11 bytes cuoi de cho them data.

### DecodeEvent

Sau khi parse duoc headers va payload, `DecodeEvent` xu ly theo `:message-type`:

- `:message-type` = `error` / `exception`: tra ve loi voi `:error-code` va `:error-message`.
- `:event-type` = `assistantResponseEvent`: decode thanh `AssistantResponseEvent`.
- `:event-type` = `toolUseEvent`: decode thanh `ToolUseEvent`.
- `:event-type` = `contextUsageEvent`: decode thanh `ContextUsageEvent`.
- `:event-type` = `meteringEvent`: tra ve raw JSON map.

---

## Stream decoder

`StreamDecoder` chuyen doi lien tuc cac binary frame thanh mot channel cac `StreamEvent` duy nhat. No su dung `Parser` de doc tung chunk tu `io.ReadCloser` body.

Cac loai `StreamEvent` duoc emit:

| Event | Nguon |
|-------|-------|
| `TextDelta` | `AssistantResponseEvent.Content` sau khi strip thinking tags |
| `ThinkingDelta` | Content nam trong `<thinking>...</thinking>` |
| `ToolUseStart` | `ToolUseEvent` dau tien voi `Name` (va `ToolUseID`) |
| `ToolUseDelta` | `ToolUseEvent.Input` (streaming JSON arguments) |
| `ToolUseStop` | `ToolUseEvent.Stop = true` |
| `ContextUsage` | `ContextUsageEvent.ContextUsagePercentage` |
| `Usage` | Tu dong tinh bang `Estimator` luc EOF |
| `Stop` | EOF hoac client cancel |
| `ErrorEvent` | Bat ky loi nao trong pipeline |

Decoder chay trong mot goroutine rieng. Channel capacity la 64 de giam blocking. Neu channel day, event se bi drop va logger ghi warning.

Context cancellation duoc ho tro: neu `ctx.Done()`, decoder dong body, emit `Stop{Reason: "client_canceled"}`, va thoat.

---

## HTTP client

`kiro.Client` duoc thiet ke rieng cho streaming:

- `Timeout: 0` — khong bao gio tu ngat mid-stream boi client timeout.
- `Connection: close` moi request — khong dung keep-alive.
- `DisableKeepAlives: true`, `MaxIdleConns: 0`, `IdleConnTimeout: 0` — khong cache connection.
- Per-account proxy: moi account co the co HTTP, HTTPS, hoac SOCKS5 proxy rieng. `clientForAccount` cache `*http.Client` trong `sync.Map` theo account ID.
- Retry logic: toi da 3 attempts voi backoff 1s, 2s, 4s. Chi retry tren 429 va 5xx. Network errors cung duoc retry.
- `Transport` force HTTP/2, TLS handshake timeout 10s.

Proxy isolation dam bao rang cac account khac nhau khong bao gio chia se connection pool, rat quan trong cho anti-ban.

---

## Endpoint URLs

| Endpoint | URL pattern |
|----------|-------------|
| Generate assistant response | `https://q.{region}.amazonaws.com/generateAssistantResponse` |
| Get usage limits | `https://q.{region}.amazonaws.com/getUsageLimits` |

`region` lay tu account field theo thu tu uu tien: `APIRegion` → `Region` → `us-east-1`.

---

## Token estimation

Proxy uoc tinh token de report `Usage.InputTokens` va `Usage.OutputTokens`:

- Su dung `tiktoken-go` voi encoding `cl100k_base` (cung encoding ma Claude su dung).
- Ap dung he so chinh sua `tokenCorrectionFactor = 1.15` — nhan voi so token raw roi ceiling.
- Neu `tiktoken-go` khoi tao that bai, fallback ve dem rune / 4.0.

Vi du:

```go
func correctedTokenCount(tokens int) int {
    if tokens <= 0 {
        return 0
    }
    return int(math.Ceil(float64(tokens) * tokenCorrectionFactor))
}
```

Input tokens duoc tinh mot lan tu `requestPayload` khi bat dau stream. Output tokens duoc tinh bang cach encode toan bo text visible thu duoc cho den khi EOF.
