package normalized

import "github.com/irisvn/kiro-let-go/internal/converter/chunks"

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

type NormalizedMessage struct {
	Role  string
	Parts []NormalizedPart
}

type NormalizedPart interface {
	normalizedPart()
	Kind() string
}

type Text struct{ Text string }

func (Text) normalizedPart() {}
func (Text) Kind() string    { return "text" }

type Image struct {
	MediaType string
	DataB64   string
}

func (Image) normalizedPart() {}
func (Image) Kind() string    { return "image" }

type ToolUse struct {
	ID        string
	Name      string
	InputJSON string
}

func (ToolUse) normalizedPart() {}
func (ToolUse) Kind() string    { return "tool_use" }

type ToolResult struct {
	ToolUseID   string
	ContentText string
	IsError     bool
}

func (ToolResult) normalizedPart() {}
func (ToolResult) Kind() string    { return "tool_result" }

type Thinking struct {
	Text      string
	Signature string
}

func (Thinking) normalizedPart() {}
func (Thinking) Kind() string    { return "thinking" }

type NormalizedTool struct {
	Name        string
	Description string
	SchemaJSON  string
}

type NormalizedToolChoice struct {
	Mode string
	Name string
}

type NormalizedChunk interface{ Kind() string }

type TextDelta = chunks.TextDelta
type ThinkingDelta = chunks.ThinkingDelta
type ToolUseStart = chunks.ToolUseStart
type ToolUseDelta = chunks.ToolUseDelta
type ToolUseStop = chunks.ToolUseStop
type Usage = chunks.Usage
type ContextUsage = chunks.ContextUsage
type Stop = chunks.Stop
