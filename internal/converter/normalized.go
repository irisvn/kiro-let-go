package converter

import "github.com/irisvn/kiro-let-go/internal/converter/chunks"

// NormalizedRequest is a unified intermediate format for both Anthropic and OpenAI.
type NormalizedRequest struct {
	Model           string
	SystemPrompt    string
	Messages        []NormalizedMessage
	Tools           []NormalizedTool
	ToolChoice      NormalizedToolChoice
	MaxOutputTokens int
	Temperature     float64
	TopP            float64
	TopK            int
	Stop            []string
	ThinkingBudget  int
	Stream          bool
	ReasoningEffort string
}

// NormalizedMessage represents a single message in the conversation.
type NormalizedMessage struct {
	Role  string
	Parts []NormalizedPart
}

// NormalizedPart is a sum type for message content parts.
// The concrete variants are Text, Image, ToolUse, ToolResult, and Thinking.
type NormalizedPart interface {
	normalizedPart()
	Kind() string
}

// Text is a plain text content part.
type Text struct {
	Text string
}

func (Text) normalizedPart() {}

// Kind returns the discriminator for Text.
func (Text) Kind() string { return "text" }

// Image is a base64-encoded image content part.
type Image struct {
	MediaType string
	DataB64   string
}

func (Image) normalizedPart() {}

// Kind returns the discriminator for Image.
func (Image) Kind() string { return "image" }

// ToolUse represents a request to invoke a tool.
type ToolUse struct {
	ID        string
	Name      string
	InputJSON string
}

func (ToolUse) normalizedPart() {}

// Kind returns the discriminator for ToolUse.
func (ToolUse) Kind() string { return "tool_use" }

// ToolResult carries the output of a tool invocation.
type ToolResult struct {
	ToolUseID   string
	ContentText string
	IsError     bool
}

func (ToolResult) normalizedPart() {}

// Kind returns the discriminator for ToolResult.
func (ToolResult) Kind() string { return "tool_result" }

// Thinking is a reasoning/thinking block.
type Thinking struct {
	Text      string
	Signature string
}

func (Thinking) normalizedPart() {}

// Kind returns the discriminator for Thinking.
func (Thinking) Kind() string { return "thinking" }

// NormalizedTool describes a tool available to the model.
type NormalizedTool struct {
	Name        string
	Description string
	SchemaJSON  string
}

// NormalizedToolChoice controls how the model should use tools.
type NormalizedToolChoice struct {
	Mode string
	Name string
}

// NormalizedChunk is a sum type for streaming response chunks.
// The concrete variants are TextDelta, ThinkingDelta, ToolUseStart, ToolUseDelta,
// ToolUseStop, Usage, ContextUsage, and Stop.
type NormalizedChunk interface {
	Kind() string
}

type TextDelta = chunks.TextDelta
type ThinkingDelta = chunks.ThinkingDelta
type ToolUseStart = chunks.ToolUseStart
type ToolUseDelta = chunks.ToolUseDelta
type ToolUseStop = chunks.ToolUseStop
type Usage = chunks.Usage
type ContextUsage = chunks.ContextUsage
type Stop = chunks.Stop
