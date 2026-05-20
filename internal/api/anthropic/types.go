package anthropic

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// MessagesRequest is the request body for the Anthropic Messages API.
type MessagesRequest struct {
	Model         string          `json:"model"`
	Messages      []Message       `json:"messages"`
	System        *SystemField    `json:"system,omitempty"`
	MaxTokens     int             `json:"max_tokens"`
	Stream        bool            `json:"stream,omitempty"`
	Temperature   *float64        `json:"temperature,omitempty"`
	TopP          *float64        `json:"top_p,omitempty"`
	TopK          *int            `json:"top_k,omitempty"`
	StopSequences []string        `json:"stop_sequences,omitempty"`
	Tools         []Tool          `json:"tools,omitempty"`
	ToolChoice    *ToolChoice     `json:"tool_choice,omitempty"`
	Thinking      *ThinkingConfig `json:"thinking,omitempty"`
	Metadata      *Metadata       `json:"metadata,omitempty"`
}

// Validate checks that the request meets basic requirements.
func (r *MessagesRequest) Validate() error {
	if r.Model == "" {
		return errors.New("model is required")
	}
	if !strings.Contains(strings.ToLower(r.Model), "claude") {
		return fmt.Errorf("model must be a Claude model (got: %s)", r.Model)
	}
	if r.MaxTokens <= 0 {
		return errors.New("max_tokens must be greater than 0")
	}
	if len(r.Messages) == 0 {
		return errors.New("messages is required")
	}
	for i, m := range r.Messages {
		if m.Role != "user" && m.Role != "assistant" {
			return fmt.Errorf("messages[%d].role must be user or assistant", i)
		}
	}
	return nil
}

// SystemField accepts either a plain string or an array of content blocks.
type SystemField struct {
	String  string
	Blocks  []ContentBlock
	IsArray bool
}

func (s *SystemField) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		s.String = str
		s.IsArray = false
		s.Blocks = nil
		return nil
	}
	var blocks []rawContentBlock
	if err := json.Unmarshal(data, &blocks); err != nil {
		return fmt.Errorf("system must be a string or an array of content blocks: %w", err)
	}
	s.Blocks = make([]ContentBlock, len(blocks))
	for i, rb := range blocks {
		block, err := unmarshalContentBlock(rb.Raw)
		if err != nil {
			return fmt.Errorf("system block %d: %w", i, err)
		}
		s.Blocks[i] = block
	}
	s.IsArray = true
	s.String = ""
	return nil
}

// MarshalJSON implements polymorphic marshaling for SystemField.
func (s SystemField) MarshalJSON() ([]byte, error) {
	if s.IsArray {
		return json.Marshal(s.Blocks)
	}
	return json.Marshal(s.String)
}

// Message represents a single message in the conversation.
type Message struct {
	Role    string          `json:"role"`
	Content *MessageContent `json:"content"`
}

// MessageContent accepts either a plain string or an array of content blocks.
type MessageContent struct {
	String  string
	Blocks  []ContentBlock
	IsArray bool
}

func (mc *MessageContent) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		mc.String = str
		mc.IsArray = false
		mc.Blocks = nil
		return nil
	}
	var blocks []rawContentBlock
	if err := json.Unmarshal(data, &blocks); err != nil {
		return fmt.Errorf("content must be a string or an array of content blocks: %w", err)
	}
	mc.Blocks = make([]ContentBlock, len(blocks))
	for i, rb := range blocks {
		block, err := unmarshalContentBlock(rb.Raw)
		if err != nil {
			return fmt.Errorf("content block %d: %w", i, err)
		}
		mc.Blocks[i] = block
	}
	mc.IsArray = true
	mc.String = ""
	return nil
}

// MarshalJSON implements polymorphic marshaling for MessageContent.
func (mc MessageContent) MarshalJSON() ([]byte, error) {
	if mc.IsArray {
		return json.Marshal(mc.Blocks)
	}
	return json.Marshal(mc.String)
}

// ContentBlock is a sum type representing different kinds of content blocks.
type ContentBlock interface {
	blockType() string
}

type rawContentBlock struct {
	Raw json.RawMessage `json:"-"`
}

func (r *rawContentBlock) UnmarshalJSON(data []byte) error {
	r.Raw = data
	return nil
}

func unmarshalContentBlock(data []byte) (ContentBlock, error) {
	var discriminator struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &discriminator); err != nil {
		return nil, fmt.Errorf("content block missing type field: %w", err)
	}
	switch discriminator.Type {
	case "text":
		var b TextBlock
		if err := json.Unmarshal(data, &b); err != nil {
			return nil, err
		}
		return b, nil
	case "image":
		var b ImageBlock
		if err := json.Unmarshal(data, &b); err != nil {
			return nil, err
		}
		return b, nil
	case "tool_use":
		var b ToolUseBlock
		if err := json.Unmarshal(data, &b); err != nil {
			return nil, err
		}
		return b, nil
	case "tool_result":
		var b ToolResultBlock
		if err := json.Unmarshal(data, &b); err != nil {
			return nil, err
		}
		return b, nil
	case "thinking":
		var b ThinkingBlock
		if err := json.Unmarshal(data, &b); err != nil {
			return nil, err
		}
		return b, nil
	case "redacted_thinking":
		var b RedactedThinkingBlock
		if err := json.Unmarshal(data, &b); err != nil {
			return nil, err
		}
		return b, nil
	default:
		return nil, fmt.Errorf("unknown content block type: %s", discriminator.Type)
	}
}

// TextBlock represents a text content block.
type TextBlock struct {
	Type         string        `json:"type"`
	Text         string        `json:"text"`
	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

func (TextBlock) blockType() string { return "text" }

// ImageBlock represents an image content block.
type ImageBlock struct {
	Type         string        `json:"type"`
	Source       ImageSource   `json:"source"`
	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

func (ImageBlock) blockType() string { return "image" }

// ImageSource describes the image data.
type ImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

// ToolUseBlock represents a tool use content block.
type ToolUseBlock struct {
	Type         string          `json:"type"`
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Input        json.RawMessage `json:"input"`
	CacheControl *CacheControl   `json:"cache_control,omitempty"`
}

func (ToolUseBlock) blockType() string { return "tool_use" }

// ToolResultBlock represents a tool result content block.
type ToolResultBlock struct {
	Type         string             `json:"type"`
	ToolUseID    string             `json:"tool_use_id"`
	Content      *ToolResultContent `json:"content,omitempty"`
	IsError      *bool              `json:"is_error,omitempty"`
	CacheControl *CacheControl      `json:"cache_control,omitempty"`
}

func (ToolResultBlock) blockType() string { return "tool_result" }

// ToolResultContent is the content of a tool result, which can be a string or array of blocks.
type ToolResultContent struct {
	String  string
	Blocks  []ContentBlock
	IsArray bool
}

// UnmarshalJSON implements polymorphic unmarshaling for ToolResultContent.
func (trc *ToolResultContent) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		trc.String = str
		trc.IsArray = false
		trc.Blocks = nil
		return nil
	}
	var blocks []rawContentBlock
	if err := json.Unmarshal(data, &blocks); err != nil {
		return fmt.Errorf("tool_result content must be a string or an array of content blocks: %w", err)
	}
	trc.Blocks = make([]ContentBlock, len(blocks))
	for i, rb := range blocks {
		block, err := unmarshalContentBlock(rb.Raw)
		if err != nil {
			return fmt.Errorf("tool_result content block %d: %w", i, err)
		}
		trc.Blocks[i] = block
	}
	trc.IsArray = true
	trc.String = ""
	return nil
}

// MarshalJSON implements polymorphic marshaling for ToolResultContent.
func (trc ToolResultContent) MarshalJSON() ([]byte, error) {
	if trc.IsArray {
		return json.Marshal(trc.Blocks)
	}
	return json.Marshal(trc.String)
}

// ThinkingBlock represents an extended thinking content block.
type ThinkingBlock struct {
	Type         string        `json:"type"`
	Thinking     string        `json:"thinking"`
	Signature    string        `json:"signature"`
	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

func (ThinkingBlock) blockType() string { return "thinking" }

// RedactedThinkingBlock represents a redacted thinking content block.
type RedactedThinkingBlock struct {
	Type         string        `json:"type"`
	Data         string        `json:"data"`
	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

func (RedactedThinkingBlock) blockType() string { return "redacted_thinking" }

// CacheControl enables prompt caching for a content block.
type CacheControl struct {
	Type string `json:"type"`
}

// Tool describes a tool available to the model.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// ToolChoice controls how the model uses tools.
type ToolChoice struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

// ThinkingConfig configures extended thinking.
type ThinkingConfig struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens"`
}

// Metadata carries optional request metadata.
type Metadata struct {
	UserID string `json:"user_id,omitempty"`
}

// MessagesResponse is the non-streaming response from the Messages API.
type MessagesResponse struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"`
	Role         string         `json:"role"`
	Content      []ContentBlock `json:"content"`
	Model        string         `json:"model"`
	StopReason   *string        `json:"stop_reason,omitempty"`
	StopSequence *string        `json:"stop_sequence,omitempty"`
	Usage        Usage          `json:"usage"`
}

func (m *MessagesResponse) UnmarshalJSON(data []byte) error {
	type alias MessagesResponse
	var raw struct {
		*alias
		Content []json.RawMessage `json:"content"`
	}
	raw.alias = (*alias)(m)
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	m.Content = make([]ContentBlock, len(raw.Content))
	for i, rawBlock := range raw.Content {
		block, err := unmarshalContentBlock(rawBlock)
		if err != nil {
			return fmt.Errorf("content block %d: %w", i, err)
		}
		m.Content[i] = block
	}
	return nil
}

func (m MessagesResponse) MarshalJSON() ([]byte, error) {
	type alias MessagesResponse
	content, err := marshalContentBlocks(m.Content)
	if err != nil {
		return nil, fmt.Errorf("marshal response content: %w", err)
	}
	return json.Marshal(&struct {
		*alias
		Content []json.RawMessage `json:"content"`
	}{
		alias:   (*alias)(&m),
		Content: content,
	})
}

func marshalContentBlocks(blocks []ContentBlock) ([]json.RawMessage, error) {
	out := make([]json.RawMessage, len(blocks))
	for i, b := range blocks {
		data, err := mustMarshal(b)
		if err != nil {
			return nil, fmt.Errorf("content block %d: %w", i, err)
		}
		out[i] = data
	}
	return out, nil
}

// Usage reports token consumption.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// CountTokensRequest is the request body for the token counting endpoint.
type CountTokensRequest struct {
	Model    string       `json:"model"`
	Messages []Message    `json:"messages"`
	System   *SystemField `json:"system,omitempty"`
	Tools    []Tool       `json:"tools,omitempty"`
}

// Validate checks that the count-tokens request meets basic requirements.
func (r *CountTokensRequest) Validate() error {
	if r.Model == "" {
		return errors.New("model is required")
	}
	if len(r.Messages) == 0 {
		return errors.New("messages is required")
	}
	for i, m := range r.Messages {
		if m.Role != "user" && m.Role != "assistant" {
			return fmt.Errorf("messages[%d].role must be user or assistant", i)
		}
	}
	return nil
}

// CountTokensResponse is the response body for the token counting endpoint.
type CountTokensResponse struct {
	InputTokens int `json:"input_tokens"`
}

// MessageStartEvent is sent at the beginning of a streamed message.
type MessageStartEvent struct {
	Type    string           `json:"type"`
	Message MessagesResponse `json:"message"`
}

// ContentBlockStartEvent is sent when a new content block begins.
type ContentBlockStartEvent struct {
	Type         string       `json:"type"`
	Index        int          `json:"index"`
	ContentBlock ContentBlock `json:"content_block"`
}

func (e *ContentBlockStartEvent) UnmarshalJSON(data []byte) error {
	type alias ContentBlockStartEvent
	var raw struct {
		*alias
		ContentBlock json.RawMessage `json:"content_block"`
	}
	raw.alias = (*alias)(e)
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	block, err := unmarshalContentBlock(raw.ContentBlock)
	if err != nil {
		return err
	}
	e.ContentBlock = block
	return nil
}

func (e ContentBlockStartEvent) MarshalJSON() ([]byte, error) {
	type alias ContentBlockStartEvent
	contentBlock, err := mustMarshal(e.ContentBlock)
	if err != nil {
		return nil, fmt.Errorf("marshal content_block: %w", err)
	}
	return json.Marshal(&struct {
		*alias
		ContentBlock json.RawMessage `json:"content_block"`
	}{
		alias:        (*alias)(&e),
		ContentBlock: contentBlock,
	})
}

// ContentBlockDeltaEvent carries incremental updates within a content block.
type ContentBlockDeltaEvent struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
	Delta Delta  `json:"delta"`
}

// Delta is a sum type for different kinds of deltas.
type Delta interface {
	deltaType() string
}

func unmarshalDelta(data []byte) (Delta, error) {
	var discriminator struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &discriminator); err != nil {
		return nil, fmt.Errorf("delta missing type field: %w", err)
	}
	switch discriminator.Type {
	case "text_delta":
		var d TextDelta
		if err := json.Unmarshal(data, &d); err != nil {
			return nil, err
		}
		return d, nil
	case "input_json_delta":
		var d InputJSONDelta
		if err := json.Unmarshal(data, &d); err != nil {
			return nil, err
		}
		return d, nil
	case "thinking_delta":
		var d ThinkingDelta
		if err := json.Unmarshal(data, &d); err != nil {
			return nil, err
		}
		return d, nil
	case "signature_delta":
		var d SignatureDelta
		if err := json.Unmarshal(data, &d); err != nil {
			return nil, err
		}
		return d, nil
	default:
		return nil, fmt.Errorf("unknown delta type: %s", discriminator.Type)
	}
}

// TextDelta represents a text fragment delta.
type TextDelta struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (TextDelta) deltaType() string { return "text_delta" }

// InputJSONDelta represents a partial JSON input delta for tool_use.
type InputJSONDelta struct {
	Type        string `json:"type"`
	PartialJSON string `json:"partial_json"`
}

func (InputJSONDelta) deltaType() string { return "input_json_delta" }

// ThinkingDelta represents a thinking fragment delta.
type ThinkingDelta struct {
	Type     string `json:"type"`
	Thinking string `json:"thinking"`
}

func (ThinkingDelta) deltaType() string { return "thinking_delta" }

// SignatureDelta represents a signature fragment delta.
type SignatureDelta struct {
	Type      string `json:"type"`
	Signature string `json:"signature"`
}

func (SignatureDelta) deltaType() string { return "signature_delta" }

func (e *ContentBlockDeltaEvent) UnmarshalJSON(data []byte) error {
	type alias ContentBlockDeltaEvent
	var raw struct {
		*alias
		Delta json.RawMessage `json:"delta"`
	}
	raw.alias = (*alias)(e)
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	delta, err := unmarshalDelta(raw.Delta)
	if err != nil {
		return err
	}
	e.Delta = delta
	return nil
}

func (e ContentBlockDeltaEvent) MarshalJSON() ([]byte, error) {
	type alias ContentBlockDeltaEvent
	delta, err := mustMarshal(e.Delta)
	if err != nil {
		return nil, fmt.Errorf("marshal delta: %w", err)
	}
	return json.Marshal(&struct {
		*alias
		Delta json.RawMessage `json:"delta"`
	}{
		alias: (*alias)(&e),
		Delta: delta,
	})
}

// ContentBlockStopEvent is sent when a content block ends.
type ContentBlockStopEvent struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
}

// MessageDeltaEvent carries incremental updates at the message level.
type MessageDeltaEvent struct {
	Type  string       `json:"type"`
	Delta MessageDelta `json:"delta"`
	Usage Usage        `json:"usage"`
}

// MessageDelta contains fields that may update as the message completes.
type MessageDelta struct {
	StopReason   *string `json:"stop_reason,omitempty"`
	StopSequence *string `json:"stop_sequence,omitempty"`
}

// MessageStopEvent is sent at the end of a streamed message.
type MessageStopEvent struct {
	Type string `json:"type"`
}

// PingEvent is a keep-alive event.
type PingEvent struct {
	Type string `json:"type"`
}

// ErrorEvent represents an error in the stream.
type ErrorEvent struct {
	Type  string      `json:"type"`
	Error ErrorDetail `json:"error"`
}

// ErrorDetail describes an error returned by the API.
type ErrorDetail struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

func mustMarshal(v any) (json.RawMessage, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return data, nil
}
