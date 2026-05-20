package normalized

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/irisvn/kiro-let-go/internal/kiro"
)

func NormalizedToKiro(req *NormalizedRequest, profileArn string) (*kiro.KiroPayload, error) {
	if req == nil {
		return nil, fmt.Errorf("normalized request is nil")
	}

	mappedModel := kiro.MapModel(req.Model)
	lastUser := lastUserMessageIndex(req.Messages)
	if lastUser == -1 {
		return nil, fmt.Errorf("normalized request has no user message")
	}

	history, err := buildKiroHistory(req.Messages[:lastUser], mappedModel)
	if err != nil {
		return nil, err
	}
	current, err := buildCurrentKiroMessage(req, req.Messages[lastUser], mappedModel)
	if err != nil {
		return nil, err
	}

	cleanHistory, cleanCurrent, droppedIDs := validatePairing(history, current)
	for _, id := range droppedIDs {
		slog.Warn("dropping unpaired tool_use/tool_result", "tool_use_id", id)
	}

	return &kiro.KiroPayload{
		ConversationState: kiro.ConversationState{
			ConversationID:      uuid.NewString(),
			AgentContinuationID: uuid.NewString(),
			AgentTaskType:       "vibe",
			ChatTriggerType:     "MANUAL",
			CurrentMessage:      cleanCurrent,
			History:             cleanHistory,
		},
		ProfileArn: profileArn,
	}, nil
}

func lastUserMessageIndex(messages []NormalizedMessage) int {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return i
		}
	}
	return -1
}

func buildKiroHistory(messages []NormalizedMessage, model string) ([]kiro.HistoryItem, error) {
	history := make([]kiro.HistoryItem, 0, len(messages))
	for _, msg := range messages {
		switch msg.Role {
		case "user":
			user, err := buildHistoryUserInput(msg, model)
			if err != nil {
				return nil, err
			}
			history = append(history, kiro.HistoryItem{UserInputMessage: user})
		case "tool":
			user := buildToolResultUserInput(msg)
			if user != nil {
				history = append(history, kiro.HistoryItem{UserInputMessage: user})
			}
		case "assistant":
			assistant := buildAssistantResponse(msg)
			if assistant == nil {
				continue
			}
			if len(history) > 0 && history[len(history)-1].AssistantResponseMessage != nil && len(assistant.ToolUses) > 0 {
				prev := history[len(history)-1].AssistantResponseMessage
				prev.Content += assistant.Content
				prev.ToolUses = append(prev.ToolUses, assistant.ToolUses...)
				continue
			}
			history = append(history, kiro.HistoryItem{AssistantResponseMessage: assistant})
		}
	}
	return history, nil
}

func buildHistoryUserInput(msg NormalizedMessage, model string) (*kiro.UserInputMessage, error) {
	var text strings.Builder
	images := make([]kiro.ImagePart, 0)
	for _, part := range msg.Parts {
		switch p := part.(type) {
		case Text:
			text.WriteString(p.Text)
		case Image:
			img, err := kiroImagePart(p)
			if err != nil {
				return nil, err
			}
			images = append(images, img)
		}
	}
	return &kiro.UserInputMessage{Content: text.String(), ModelID: model, Origin: "AI_EDITOR", Images: images}, nil
}

func buildToolResultUserInput(msg NormalizedMessage) *kiro.UserInputMessage {
	results := make([]kiro.ToolResult, 0)
	for _, part := range msg.Parts {
		if p, ok := part.(ToolResult); ok {
			status := "success"
			if p.IsError {
				status = "error"
			}
			results = append(results, kiro.ToolResult{ToolUseID: p.ToolUseID, Content: []kiro.ToolResultContent{{Text: p.ContentText}}, Status: status})
		}
	}
	if len(results) == 0 {
		return nil
	}
	return &kiro.UserInputMessage{UserInputMessageContext: &kiro.UserInputMessageContext{ToolResults: results}}
}

func buildAssistantResponse(msg NormalizedMessage) *kiro.AssistantResponseMessage {
	var text strings.Builder
	toolUses := make([]kiro.ToolUseEntry, 0)
	for _, part := range msg.Parts {
		switch p := part.(type) {
		case Text:
			text.WriteString(p.Text)
		case ToolUse:
			toolUses = append(toolUses, kiro.ToolUseEntry{ToolUseID: p.ID, Name: p.Name, Input: p.InputJSON})
		}
	}
	if text.Len() == 0 && len(toolUses) == 0 {
		return nil
	}
	return &kiro.AssistantResponseMessage{Content: text.String(), ToolUses: toolUses}
}

func buildCurrentKiroMessage(req *NormalizedRequest, msg NormalizedMessage, model string) (kiro.CurrentMessage, error) {
	var text strings.Builder
	images := make([]kiro.ImagePart, 0)
	for _, part := range msg.Parts {
		switch p := part.(type) {
		case Text:
			text.WriteString(p.Text)
		case Image:
			img, err := kiroImagePart(p)
			if err != nil {
				return kiro.CurrentMessage{}, err
			}
			images = append(images, img)
		}
	}

	content := text.String()
	if req.ThinkingBudget > 0 {
		content = fmt.Sprintf("<thinking_mode>extended</thinking_mode><max_thinking_length>%d</max_thinking_length>%s", req.ThinkingBudget, content)
	}

	tools, err := kiroTools(req.Tools)
	if err != nil {
		return kiro.CurrentMessage{}, err
	}

	var ctx *kiro.UserInputMessageContext
	if len(tools) > 0 {
		ctx = &kiro.UserInputMessageContext{Tools: tools}
	}

	return kiro.CurrentMessage{UserInputMessage: kiro.UserInputMessage{Content: content, ModelID: model, Origin: "AI_EDITOR", UserInputMessageContext: ctx, Images: images}}, nil
}

func kiroTools(tools []NormalizedTool) ([]kiro.Tool, error) {
	out := make([]kiro.Tool, 0, len(tools))
	for _, tool := range tools {
		schema := json.RawMessage(tool.SchemaJSON)
		if len(schema) == 0 {
			schema = json.RawMessage(`{}`)
		}
		if !json.Valid(schema) {
			return nil, fmt.Errorf("tool %q schema is invalid JSON", tool.Name)
		}
		copied := append(json.RawMessage(nil), schema...)
		out = append(out, kiro.Tool{ToolSpecification: kiro.ToolSpecification{Name: tool.Name, Description: tool.Description, InputSchema: kiro.InputSchema{JSON: copied}}})
	}
	return out, nil
}

func kiroImagePart(part Image) (kiro.ImagePart, error) {
	data, err := base64.StdEncoding.DecodeString(part.DataB64)
	if err != nil {
		return kiro.ImagePart{}, fmt.Errorf("decode image: %w", err)
	}
	format := part.MediaType
	if _, suffix, ok := strings.Cut(part.MediaType, "/"); ok {
		format = suffix
	}
	return kiro.ImagePart{Format: format, Source: kiro.ImageSource{Bytes: append([]byte(nil), data...)}}, nil
}

func validatePairing(history []kiro.HistoryItem, current kiro.CurrentMessage) ([]kiro.HistoryItem, kiro.CurrentMessage, []string) {
	uses := make(map[string]bool)
	results := make(map[string]bool)
	for _, item := range history {
		if item.AssistantResponseMessage != nil {
			for _, use := range item.AssistantResponseMessage.ToolUses {
				uses[use.ToolUseID] = true
			}
		}
		if item.UserInputMessage != nil && item.UserInputMessage.UserInputMessageContext != nil {
			for _, result := range item.UserInputMessage.UserInputMessageContext.ToolResults {
				results[result.ToolUseID] = true
			}
		}
	}
	if current.UserInputMessage.UserInputMessageContext != nil {
		for _, result := range current.UserInputMessage.UserInputMessageContext.ToolResults {
			results[result.ToolUseID] = true
		}
	}
	drop := make(map[string]bool)
	for id := range uses {
		if !results[id] {
			drop[id] = true
		}
	}
	for id := range results {
		if !uses[id] {
			drop[id] = true
		}
	}
	droppedIDs := make([]string, 0, len(drop))
	for id := range drop {
		droppedIDs = append(droppedIDs, id)
	}
	sort.Strings(droppedIDs)
	if len(drop) == 0 {
		return history, current, droppedIDs
	}
	filteredHistory := make([]kiro.HistoryItem, 0, len(history))
	for _, item := range history {
		if item.AssistantResponseMessage != nil {
			toolUses := item.AssistantResponseMessage.ToolUses[:0]
			for _, use := range item.AssistantResponseMessage.ToolUses {
				if !drop[use.ToolUseID] {
					toolUses = append(toolUses, use)
				}
			}
			item.AssistantResponseMessage.ToolUses = toolUses
			if item.AssistantResponseMessage.Content == "" && len(toolUses) == 0 {
				continue
			}
		}
		if item.UserInputMessage != nil && item.UserInputMessage.UserInputMessageContext != nil {
			toolResults := item.UserInputMessage.UserInputMessageContext.ToolResults[:0]
			for _, result := range item.UserInputMessage.UserInputMessageContext.ToolResults {
				if !drop[result.ToolUseID] {
					toolResults = append(toolResults, result)
				}
			}
			item.UserInputMessage.UserInputMessageContext.ToolResults = toolResults
			if item.UserInputMessage.Content == "" && len(toolResults) == 0 {
				continue
			}
		}
		filteredHistory = append(filteredHistory, item)
	}
	return filteredHistory, current, droppedIDs
}
