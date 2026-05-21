# Bug Post-mortem: "Fatal Client Error" với Multi-turn Conversations

## Ngày: 2026-05-21

## Tóm tắt

Proxy trả về lỗi "fatal client error" (400) khi gửi multi-turn conversations có tool calls đến Kiro upstream.

## Nguyên nhân Gốc (Root Causes)

Có **3 lỗi** liên quan đến nhau:

### 1. `applyModelMapping` Set `modelId` Trên Tool Result Messages

**Vị trí**: `internal/kiro/dispatcher.go:351`

**Lỗi**: Logic loop set `msg.ModelID = current` cho **tất cả** history messages, bao gồm cả tool result messages.

```go
// LỖI - Set modelId cho tất cả history items
for _, msg := range stateCopy.History {
    if msg.UserInputMessage != nil {
        msg.UserInputMessage.ModelID = current  // ❌ Set cả cho tool results
    }
}
```

**Hậu quả**: Kiro API từ chối payload khi tool result message có `modelId`.

**Fix**: Chỉ set `modelId` cho user messages thông thường, không phải tool result messages:

```go
// ĐÚNG - Kiểm tra kỹ trước khi set modelId
if msg.UserInputMessage != nil && 
    (msg.UserInputMessage.UserInputMessageContext == nil || 
     len(msg.UserInputMessage.UserInputMessageContext.ToolResults) == 0) {
    msg.UserInputMessage.ModelID = current
}
```

### 2. `HistoryItem.MarshalJSON` Thiếu Wrapper Keys

**Vị trí**: `internal/kiro/types.go:65`

**Lỗi**: Marshal trả về bare struct thay vì wrapped object.

```go
// LỖI - Marshal bare struct
func (h HistoryItem) MarshalJSON() ([]byte, error) {
    if h.UserInputMessage != nil {
        return json.Marshal(h.UserInputMessage)  // ❌ Trả về {"content":"..."}
    }
    // ...
}
```

**Hậu quả**: Kiro expect format:
```json
{"userInputMessage": {"content": "..."}}
```

Nhưng proxy gửi:
```json
{"content": "..."}
```

**Fix**: Thêm wrapper keys:

```go
// ĐÚNG - Wrap với proper keys
func (h HistoryItem) MarshalJSON() ([]byte, error) {
    if h.UserInputMessage != nil {
        return json.Marshal(map[string]interface{}{
            "userInputMessage": h.UserInputMessage,  // ✅ Đúng format
        })
    }
    // ...
}
```

### 3. `ToolUseEntry.Input` Sai Type

**Vị trí**: `internal/kiro/types.go:157`

**Lỗi**: `Input` là `string`, nhưng Kiro expect JSON object.

```go
// LỖI - Input là string
type ToolUseEntry struct {
    ToolUseID string `json:"toolUseId"`
    Name      string `json:"name"`
    Input     string `json:"input,omitempty"`  // ❌ Sai type
}
```

**Hậu quả**: Proxy gửi:
```json
{"toolUses": [{"input": "{\"location\":\"Paris\"}", ...}]}
```

Nhưng Kiro expect:
```json
{"toolUses": [{"input": {"location": "Paris"}, ...}]}
```

**Fix**: Đổi sang `json.RawMessage`:

```go
// ĐÚNG - Input là json.RawMessage
type ToolUseEntry struct {
    ToolUseID string          `json:"toolUseId"`
    Name      string          `json:"name"`
    Input     json.RawMessage `json:"input,omitempty"`  // ✅ Đúng type
}
```

### 4. (Bonus) Go Loop Variable Pointer Bug

**Vị trí**: `internal/converter/to_kiro.go:validatePairing`

**Lỗi**: Dùng pointer của loop variable gây lỗi memory:

```go
// LỖI - Dùng pointer của loop variable
for _, user := range ... {
    clean.UserInputMessage = &user  // ❌ &user trỏ đến cùng address
}
```

**Fix**: Explicit heap allocation:

```go
// ĐÚNG - Clone object
for _, user := range ... {
    userCopy := user
    clean.UserInputMessage = &userCopy  // ✅ Mỗi iteration có address riêng
}
```

## Bài học

1. **Đọc API spec kỹ**: Kiro API có yêu cầu rất cụ thể về format payload
2. **Kiểm tra type**: `string` vs `json.RawMessage` có semantic khác nhau trong JSON
3. **Chú ý Go loop variable**: Luôn clone nếu cần pointer trong loop
4. **Test với tool chain**: Tool calls là case phức tạp, cần test riêng

## Files Thay đổi

- `internal/kiro/types.go` - Sửa `HistoryItem.MarshalJSON` và `ToolUseEntry.Input`
- `internal/kiro/dispatcher.go` - Fix `applyModelMapping`
- `internal/converter/to_kiro.go` - Fix loop variable pointer và placeholder logic
- `internal/api/anthropic/handler.go` - Cập nhật sử dụng `json.RawMessage`
- `internal/api/openai/handler.go` - Cập nhật sử dụng `json.RawMessage`
- `internal/server/requestlog.go` - Thêm file persistence
- `internal/api/admin/proxy.go` - Thêm `RequestID`

## Tham khảo

- `kiro-gateway` Python codebase cho format chuẩn của Kiro API
- Kiro API docs (internal) cho validation rules