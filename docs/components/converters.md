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

### Phân tách Lịch sử (History split)

Để tương thích hoàn toàn với giao thức của Kiro (nơi request luôn kết thúc bằng một `CurrentMessage` đại diện cho lượt gửi hiện tại của client, và các lượt trước đó nằm trong `History`), converter thực hiện cơ chế phân tách thông minh dựa trên loại tin nhắn cuối cùng:

1. **Nếu tin nhắn cuối cùng là `user`**:
   - Tin nhắn `user` cuối cùng này sẽ được chọn làm `CurrentMessage`.
   - Toàn bộ các tin nhắn trước đó được đưa vào `History`.

2. **Nếu tin nhắn cuối cùng là `tool`** (để xử lý phản hồi từ việc gọi công cụ, ví dụ như của OpenCode Agent):
   - Converter sẽ gom toàn bộ các tin nhắn có role là `"tool"` liên tiếp ở cuối cuộc hội thoại (từ tin nhắn cuối cùng ngược lên cho đến khi gặp tin nhắn không phải role `"tool"`).
   - Chuỗi các tin nhắn `"tool"` liên tiếp này sẽ được gộp lại để tạo thành một `CurrentMessage` duy nhất chứa các `ToolResults`.
   - Phần hội thoại trước đó (bao gồm cả tin nhắn `assistant` chứa yêu cầu gọi công cụ tương ứng) được giữ nguyên trong `History`.
   - **Tác dụng**: Cơ chế này giúp khắc phục triệt để lỗi vòng lặp vô hạn (infinite exploration loop) của Agent khi không ghép cặp được tin nhắn phản hồi của công cụ với lượt truy vấn, giúp Agent tiếp tục suy luận chính xác trên kết quả công cụ nhận được.

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

## Chuẩn hóa Model thông minh (Smart Model Normalization)

Trước khi thực hiện ánh xạ (mapping), tên model nhận được từ client sẽ được chuẩn hóa để xử lý các định dạng không nhất quán:

- Loại bỏ tiền tố sau ký tự `/` (ví dụ `kiro/claude-sonnet-4-6` → `claude-sonnet-4-6`).
- Điều chỉnh dấu phân cách: chuyển `4-6` thành `4.6`, `_` thành `-`.
- Không phân biệt chữ hoa chữ thường: `Claude-Sonnet-4.6` → `claude-sonnet-4.6`.

Ví dụ cụ thể:

| Tên đầu vào từ Client | Sau khi chuẩn hóa |
|-----------------------|-------------------|
| `kiro/claude-sonnet-4-6` | `claude-sonnet-4.6` |
| `claude_sonnet_4_6` | `claude-sonnet-4.6` |
| `Claude-Opus-4-7` | `claude-opus-4.7` |

## Điểm ánh xạ duy nhất (Single mapping point)

Chỉ có `kiro.Dispatcher` chịu trách nhiệm thực hiện ánh xạ model (model mapping). Các handler API (Anthropic, OpenAI) và bộ converter trung gian không bị trùng lặp logic này. Điều này đảm bảo:

- Chỉ có một nơi duy nhất cần sửa đổi khi bổ sung model mới.
- Loại bỏ hoàn toàn sự không nhất quán giữa các giao diện API.
- Chuỗi dự phòng (fallback chains) được xử lý đồng bộ tại một đầu mối duy nhất.

## Ánh xạ Model (Model mapping)

Hàm `MapModel` được triển khai dưới dạng bảng (table-driven) trong tệp `internal/kiro/types.go`:

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

| Bí danh (Alias) | Kết quả ánh xạ |
|-----------------|-----------------|
| `sonnet` | `claude-sonnet-4.6` |
| `opus` | `claude-opus-4.7` |
| `haiku` | `claude-haiku-4.5` |
| `sonnet-4.5` / `claude-sonnet-4.5` | `claude-sonnet-4.5` |
| `sonnet-4.6` / `claude-sonnet-4.6` | `claude-sonnet-4.6` |
| `opus-4.5` / `claude-opus-4.5` | `claude-opus-4.5` |
| `opus-4.6` / `claude-opus-4.6` | `claude-opus-4.6` |
| `opus-4.7` / `claude-opus-4.7` | `claude-opus-4.7` |
| `haiku-4.5` / `claude-haiku-4.5` | `claude-haiku-4.5` |

Nếu tên model đầu vào không khớp bất kỳ điều kiện nào ở trên, nó sẽ được chuyển thẳng lên upstream mà không thay đổi (passthrough).

## Đường ống Độ tin cậy (Reliability Pipeline - Trước khi gửi lên Kiro)

Trước khi gửi dữ liệu payload cuối cùng tới Kiro, payload sẽ đi qua 3 bước bảo vệ độ tin cậy nghiêm ngặt:

### 1. Chuẩn hóa JSON Schema (JSON Schema Normalization)

- Sửa lỗi `required: null` → tự động xóa trường `required`.
- Sửa lỗi `properties: null` → thay thế bằng `{}`.
- Loại bỏ trường `additionalProperties` (do Kiro không hỗ trợ cấu trúc này).

### 2. Rút gọn tên Công cụ (Tool Name Shortening)

Các công cụ có tên dài vượt quá **63 ký tự** sẽ tự động được rút gọn thành định dạng: `54 ký tự đầu` + `_` + `8 ký tự mã hóa SHA256`. Tên gốc của công cụ sẽ được tự động khôi phục lại khi nhận phản hồi từ Kiro để trả về cho client.

*Ví dụ*: `very_long_tool_name_that_exceeds_sixty_three_characters_limit` → `very_long_tool_name_that_exceeds_sixty_three_char_abc123de`

### 3. Bộ lọc kích thước Payload (Payload Size Guard)

Nếu tổng dung lượng payload vượt quá **600KB**, hệ thống sẽ tự động lọc bỏ các cặp hội thoại cũ nhất (oldest user/assistant pairs) trong lịch sử cho tới khi dung lượng giảm xuống dưới ngưỡng. Điều này giúp ngăn ngừa việc yêu cầu bị từ chối từ máy chủ Kiro do vượt quá giới hạn băng thông mà vẫn bảo toàn tối đa ngữ cảnh hội thoại gần nhất.
