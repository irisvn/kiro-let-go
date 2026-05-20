package kiro

import "encoding/json"

const (
	ModelClaudeSonnet45 = "claude-sonnet-4.5"
	ModelClaudeSonnet46 = "claude-sonnet-4.6"
	ModelClaudeOpus45   = "claude-opus-4.5"
	ModelClaudeOpus46   = "claude-opus-4.6"
	ModelClaudeOpus47   = "claude-opus-4.7"
	ModelClaudeHaiku45  = "claude-haiku-4.5"
)

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

type KiroPayload struct {
	ConversationState ConversationState `json:"conversationState"`
	ProfileArn        string            `json:"profileArn,omitempty"`
}

type ConversationState struct {
	ConversationID      string         `json:"conversationId"`
	AgentContinuationID string         `json:"agentContinuationId,omitempty"`
	AgentTaskType       string         `json:"agentTaskType,omitempty"`
	ChatTriggerType     string         `json:"chatTriggerType,omitempty"`
	CurrentMessage      CurrentMessage `json:"currentMessage"`
	History             []HistoryItem  `json:"history,omitempty"`
}

type CurrentMessage struct {
	UserInputMessage UserInputMessage `json:"userInputMessage"`
}

type UserInputMessage struct {
	Content                 string                   `json:"content"`
	ModelID                 string                   `json:"modelId,omitempty"`
	Origin                  string                   `json:"origin,omitempty"`
	UserInputMessageContext *UserInputMessageContext `json:"userInputMessageContext,omitempty"`
	Images                  []ImagePart              `json:"images,omitempty"`
}

type UserInputMessageContext struct {
	Tools       []Tool       `json:"tools,omitempty"`
	ToolResults []ToolResult `json:"toolResults,omitempty"`
}

type Tool struct {
	ToolSpecification ToolSpecification `json:"toolSpecification"`
}

type ToolSpecification struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	InputSchema InputSchema `json:"inputSchema"`
}

type InputSchema struct {
	JSON json.RawMessage `json:"json,omitempty"`
}

type ToolResult struct {
	ToolUseID string              `json:"toolUseId"`
	Content   []ToolResultContent `json:"content,omitempty"`
	Status    string              `json:"status,omitempty"`
}

type ToolResultContent struct {
	Text string `json:"text,omitempty"`
}

type HistoryItem struct {
	UserInputMessage         *UserInputMessage
	AssistantResponseMessage *AssistantResponseMessage
}

func (h HistoryItem) MarshalJSON() ([]byte, error) {
	if h.UserInputMessage != nil {
		return json.Marshal(h.UserInputMessage)
	}
	if h.AssistantResponseMessage != nil {
		return json.Marshal(h.AssistantResponseMessage)
	}
	return []byte("null"), nil
}

type AssistantResponseMessage struct {
	Content  string         `json:"content"`
	ToolUses []ToolUseEntry `json:"toolUses,omitempty"`
}

type ToolUseEntry struct {
	ToolUseID string `json:"toolUseId"`
	Name      string `json:"name"`
	Input     string `json:"input,omitempty"`
}

type ImagePart struct {
	Format string      `json:"format,omitempty"`
	Source ImageSource `json:"source,omitempty"`
}

type ImageSource struct {
	Bytes []byte `json:"bytes,omitempty"`
}

type EventType string

const (
	EventAssistantResponse EventType = "assistantResponseEvent"
	EventToolUse           EventType = "toolUseEvent"
	EventContextUsage      EventType = "contextUsageEvent"
	EventMetering          EventType = "meteringEvent"
)

type AssistantResponseEvent struct {
	Content string `json:"content,omitempty"`
}

type ToolUseEvent struct {
	Name      string `json:"name,omitempty"`
	ToolUseID string `json:"toolUseId,omitempty"`
	Input     string `json:"input,omitempty"`
	Stop      bool   `json:"stop,omitempty"`
}

type ContextUsageEvent struct {
	ContextUsagePercentage float64 `json:"contextUsagePercentage,omitempty"`
}
