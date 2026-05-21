# Converters

## Tổng quan

Module converter triển khai pipeline 2 bước để tách rời định dạng input (Anthropic Messages API hoặc OpenAI Chat Completions API) khỏi giao thức Kiro:

```
Input: Anthropic Messages OR OpenAI ChatCompletion
       ↓
   NormalizedRequest
       ↓
   KiroPayload
       ↓
Input: Kiro StreamEvents
       ↓
   NormalizedChunk
       ↓
Output: Anthropic SSE OR OpenAI chunk
```

`NormalizedRequest` đóng vai trò intermediate format duy nhất. Nhờ vậy, code xử lý Kiro chỉ cần hiểu một schema thay vì phải hỗ trợ trực tiếp cả Anthropic lẫn OpenAI.

---

## NormalizedRequest schema

| Field | Type | Mô tả |
|-------|------|-------|
| `Model` | `string` | Model ID do client gửi lên, sẽ được ánh xạ qua `MapModel` trước khi gửi đến Kiro |
| `SystemPrompt` | `string` | Prompt hệ thống, đã được concat từ các system message |
| `Messages` | `[]NormalizedMessage` | Toàn bộ conversation history |
| `Tools` | `[]NormalizedTool` | Danh sách tool khả dụng |
| `ToolChoice` | `NormalizedToolChoice` | Chế độ chọn tool (`auto`, `any`, `tool`, `none`) |
| `MaxOutputTokens` | `int` | Giới hạn token output |
| `Temperature` | `float64` | Độ ngẫu nhiên |
| `TopP` | `float64` | Nucleus sampling |
| `TopK` | `int` | Top-k sampling |
| `Stop` | `[]string` | Các chuỗi dừng |
| `ThinkingBudget` | `int` | Số token tối đa cho thinking (0 = tắt) |
| `Stream` | `bool` | Có stream response hay không |
| `ReasoningEffort` | `string` | Mức độ reasoning (OpenAI), ánh xạ sang `ThinkingBudget` |

---

## NormalizedPart sum types

`NormalizedPart` là interface sum type với 5 variant:

| Variant | Fields | Ý nghĩa |
|---------|--------|---------|
| `Text` | `Text string` | Nội dung text thuần |
| `Image` | `MediaType string`, `DataB64 string` | Ảnh base64 |
| `ToolUse` | `ID string`, `Name string`, `InputJSON string` | Yêu cầu gọi tool |
| `ToolResult` | `ToolUseID string`, `ContentText string`, `IsError bool` | Kết quả trả về từ tool |
| `Thinking` | `Text string`, `Signature string` | Block reasoning/thinking |

Mỗi variant implement `normalizedPart()` (unexported marker) và `Kind() string` để phân biệt.

---

## NormalizedChunk sum types

`NormalizedChunk` là interface cho các chunk streaming response:

| Variant | Fields | Ý nghĩa |
|---------|--------|---------|
| `TextDelta` | `Text string` | Fragment text mới |
| `ThinkingDelta` | `Text string` | Fragment thinking mới |
| `ToolUseStart` | `ID string`, `Name string` | Bắt đầu một tool call |
| `ToolUseDelta` | `ID string`, `InputDelta string` | Cập nhật arguments của tool call |
| `ToolUseStop` | `ID string` | Kết thúc một tool call |
| `Usage` | `InputTokens int`, `OutputTokens int` | Token usage |
| `ContextUsage` | `Pct float64` | Phần trăm context đã dùng |
| `Stop` | `Reason string` | Lý do dừng stream |

---

## Anthropic ↔ Normalized

### System prompt

Anthropic cho phép `system` là string hoặc `[]ContentBlock`. Nếu là array, converter concat tất cả `TextBlock` lại thành một string duy nhất:

```go
func systemText(system *anthropic.SystemField) string {
    if !system.IsArray {
        return system.String
    }
    var b strings.Builder
    for _, block := range system.Blocks {
        if text, ok := block.(anthropic.TextBlock); ok {
            b.WriteString(text.Text)
        }
    }
    return b.String()
}
```

### Message content

Message content cũng có thể là string hoặc `[]ContentBlock`. Các block được ánh xạ:

| Anthropic block | NormalizedPart |
|-----------------|----------------|
| `TextBlock` | `Text` |
| `ImageBlock` | `Image` (MediaType + Data base64) |
| `ToolUseBlock` | `ToolUse` (ID, Name, InputJSON) |
| `ToolResultBlock` | `ToolResult` (ToolUseID, ContentText, IsError) |
| `ThinkingBlock` | `Thinking` (Text, Signature) |

### Tools

`Tool.InputSchema` (kiểu `json.RawMessage`) được marshal thành string và lưu vào `NormalizedTool.SchemaJSON`.

### Thinking config

Nếu `req.Thinking.Type == "enabled"`, `BudgetTokens` được copy sang `ThinkingBudget`. Nếu `BudgetTokens == 0`, default là `4000`.

---

## OpenAI ↔ Normalized

### System messages

Tất cả message có `role == "system"` được tách text part và concat bằng newline thành `SystemPrompt`. Các system message không được giữ lại trong `Messages`.

### Multimodal image

OpenAI hỗ trợ `image_url` trong content array. Converter chỉ chấp nhận data URL (`data:image/...;base64,...`). HTTP/HTTPS URL bị từ chối với lỗi `IMAGE_URL_UNSUPPORTED`:

```go
if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
    return Image{}, errs.New(errs.ClassFatal, "IMAGE_URL_UNSUPPORTED", "http image URLs are unsupported")
}
```

### Tool calls

- Assistant message có `tool_calls` → các `ToolUse` part được tạo từ `call.ID`, `call.Function.Name`, `call.Function.Arguments`.
- Message có `role == "tool"` → `ToolResult` part, ghép đôi qua `tool_call_id`.

### Reasoning effort → ThinkingBudget

| `reasoning_effort` | `ThinkingBudget` |
|--------------------|----------------|
| `"none"`, `"minimal"`, `""` | 0 |
| `"low"` | 2000 |
| `"medium"` | 4000 |
| `"high"` | 8000 |
| `"xhigh"` | 10000 |

### Merge adjacent same-role messages

Hàm `appendNormalizedMessage` merge message kế tiếp nếu cùng role, tránh vi phạm quy tắc alternating user/assistant của một số backend:

```go
func appendNormalizedMessage(req *NormalizedRequest, msg NormalizedMessage) {
    if len(req.Messages) > 0 && req.Messages[len(req.Messages)-1].Role == msg.Role {
        req.Messages[len(req.Messages)-1].Parts = append(req.Messages[len(req.Messages)-1].Parts, msg.Parts...)
        return
    }
    req.Messages = append(req.Messages, msg)
}
```

---

## Normalized → KiroPayload

### Metadata generation

Mỗi request được tạo:
- `conversationId`: UUID v4
- `agentContinuationId`: UUID v4
- `agentTaskType`: `"vibe"`
- `chatTriggerType`: `"MANUAL"`

### History split

Tất cả messages trừ message user cuối cùng được đưa vào `history`. Message user cuối trở thành `currentMessage`.

### Assistant response collapse

Nếu assistant message kế tiếp trong history đều chứa `ToolUse`, chúng được gộp thành một `AssistantResponseMessage` duy nhất (nối `Content` và append `ToolUses`):

```go
if len(history) > 0 && history[len(history)-1].AssistantResponseMessage != nil && len(assistant.ToolUses) > 0 {
    prev := history[len(history)-1].AssistantResponseMessage
    prev.Content += assistant.Content
    prev.ToolUses = append(prev.ToolUses, assistant.ToolUses...)
    continue
}
```

### Tool results

Các `ToolResult` part được chuyển thành `UserInputMessage` với `userInputMessageContext.toolResults`:

```go
results = append(results, kiro.ToolResult{
    ToolUseID: p.ToolUseID,
    Content:   []kiro.ToolResultContent{{Text: p.ContentText}},
    Status:    status,
})
```

### CurrentMessage

- Content: concat tất cả text part. Nếu `ThinkingBudget > 0`, prepend tag:
  ```
  <thinking_mode>extended</thinking_mode><max_thinking_length>{N}</max_thinking_length>{content}
  ```
- `ModelID`: qua `MapModel`
- `Origin`: `"AI_EDITOR"`
- Tools: chuyển thành `Tool{ToolSpecification{Name, Description, InputSchema:{JSON}}}`

### Pairing validation

Converter kiểm tra mọi `ToolUse` phải có `ToolResult` tương ứng (và ngược lại). Nếu không, cả hai đều bị drop với log WARN:

```go
for _, id := range droppedIDs {
    slog.Warn("dropping unpaired tool_use/tool_result", "tool_use_id", id)
}
```

---

## Smart Model Normalization

Truoc khi mapping, model name duoc normalize de xu ly cac dinh dang khong nhat quan tu client:

- Strip prefix sau `/` (vi du `kiro/claude-sonnet-4-6` → `claude-sonnet-4-6`).
- Fix separators: `4-6` → `4.6`, `_` → `-`.
- Case insensitive: `Claude-Sonnet-4.6` → `claude-sonnet-4.6`.

Vi du:

| Input | Sau normalize |
|-------|---------------|
| `kiro/claude-sonnet-4-6` | `claude-sonnet-4.6` |
| `claude_sonnet_4_6` | `claude-sonnet-4.6` |
| `Claude-Opus-4-7` | `claude-opus-4.7` |

## Single mapping point

Chi co `kiro.Dispatcher` thuc hien model mapping. Cac handler (Anthropic, OpenAI) va converter khong duplicate logic mapping. Dieu nay dam bao:

- Mot diem sua duy nhat khi them model moi.
- Khong co inconsistency giua cac API surfaces.
- Fallback chains duoc xu ly o mot noi.

## Model mapping

`MapModel` là hàm table-driven trong `internal/kiro/types.go`:

```go
func MapModel(input string) string {
    switch input {
    case "sonnet-4.5", "claude-sonnet-4.5":
        return ModelClaudeSonnet45
    case "sonnet-4.6", "claude-sonnet-4.6":
        return ModelClaudeSonnet46
    case "opus-4.5", "claude-opus-4.5":
        return ModelClaudeOpus45
    case "opus-4.6", "claude-opus-4.6":
        return ModelClaudeOpus46
    case "opus-4.7", "claude-opus-4.7":
        return ModelClaudeOpus47
    case "haiku-4.5", "claude-haiku-4.5":
        return ModelClaudeHaiku45
    case "sonnet":
        return ModelClaudeSonnet46
    case "opus":
        return ModelClaudeOpus47
    case "haiku":
        return ModelClaudeHaiku45
    default:
        return input
    }
}
```

| Alias | Kết quả |
|-------|---------|
| `sonnet` | `claude-sonnet-4.6` |
| `opus` | `claude-opus-4.7` |
| `haiku` | `claude-haiku-4.5` |
| `sonnet-4.5` / `claude-sonnet-4.5` | `claude-sonnet-4.5` |
| `sonnet-4.6` / `claude-sonnet-4.6` | `claude-sonnet-4.6` |
| `opus-4.5` / `claude-opus-4.5` | `claude-opus-4.5` |
| `opus-4.6` / `claude-opus-4.6` | `claude-opus-4.6` |
| `opus-4.7` / `claude-opus-4.7` | `claude-opus-4.7` |
| `haiku-4.5` / `claude-haiku-4.5` | `claude-haiku-4.5` |

Nếu input không khớp bất kỳ case nào, trả về nguyên input (passthrough).

## Reliability Pipeline (truoc khi gui Kiro)

Truoc khi gui request den Kiro, payload di qua 3 buoc reliability:

### 1. JSON Schema Normalization

- Fix `required: null` → xoa field `required`.
- Fix `properties: null` → thay bang `{}`.
- Xoa `additionalProperties` (Kiro khong ho tro).

### 2. Tool Name Shortening

Ten tool > 63 ky tu se bi rut ngan thanh 54-ky tu prefix + `_` + 8-ky tu SHA256 hash. Ten goc duoc restore khi nhan response.

Vi du: `very_long_tool_name_that_exceeds_sixty_three_characters_limit` → `very_long_tool_name_that_exceeds_sixty_three_char_abc123de`

### 3. Payload Size Guard

Neu payload > 600KB, he thong tu dong trim cac cap history cu nhat (oldest user/assistant pairs) cho den khi duoi nguong. Dieu nay ngan chan request bi reject vi qua lon ma khong mat context gan nhat.
