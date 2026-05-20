package kiro

import (
	"encoding/json"
	"strings"
)

const (
	ModelClaudeSonnet45 = "claude-sonnet-4.5"
	ModelClaudeSonnet46 = "claude-sonnet-4.6"
	ModelClaudeOpus45   = "claude-opus-4.5"
	ModelClaudeOpus46   = "claude-opus-4.6"
	ModelClaudeOpus47   = "claude-opus-4.7"
	ModelClaudeHaiku45  = "claude-haiku-4.5"
)

func MapModel(input string) string {
	normalized := normalizeModelInput(input)

	switch normalized {
	case "claude-sonnet-4.5", "sonnet-4.5", "sonnet-4-5":
		return ModelClaudeSonnet45
	case "claude-sonnet-4.6", "sonnet-4.6", "sonnet-4-6":
		return ModelClaudeSonnet46
	case "claude-opus-4.5", "opus-4.5", "opus-4-5":
		return ModelClaudeOpus45
	case "claude-opus-4.6", "opus-4.6", "opus-4-6":
		return ModelClaudeOpus46
	case "claude-opus-4.7", "opus-4.7", "opus-4-7":
		return ModelClaudeOpus47
	case "claude-haiku-4.5", "haiku-4.5", "haiku-4-5":
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

func normalizeModelInput(input string) string {
	s := strings.ToLower(strings.TrimSpace(input))

	for _, prefix := range []string{"kiro/", "anthropic/", "openai/", "claude/", "aws/"} {
		s = strings.TrimPrefix(s, prefix)
	}

	s = strings.ReplaceAll(s, "_", "-")

	parts := strings.SplitN(s, "-", -1)
	if len(parts) >= 3 {
		last := parts[len(parts)-1]
		secondLast := parts[len(parts)-2]
		if isVersionPart(secondLast) && isVersionPart(last) {
			parts[len(parts)-2] = secondLast + "." + last
			parts = parts[:len(parts)-1]
			s = strings.Join(parts, "-")
		}
	}

	return s
}

func isVersionPart(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, c := range s {
		if c != '.' && (c < '0' || c > '9') {
			return false
		}
	}
	return true
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
