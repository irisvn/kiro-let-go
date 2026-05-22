package converter

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

// NormalizedToKiro converts a normalized request into Kiro's request payload.
func NormalizedToKiro(req *NormalizedRequest, profileArn string) (*kiro.KiroPayload, error) {
	if req == nil {
		return nil, fmt.Errorf("normalized request is nil")
	}

	n := len(req.Messages)
	if n == 0 {
		return nil, fmt.Errorf("normalized request has no messages")
	}

	var history []kiro.HistoryItem
	var current kiro.CurrentMessage
	var err error

	// Nếu tin nhắn cuối cùng là tool, chúng ta gộp tất cả các tin nhắn tool liên tiếp ở cuối vào CurrentMessage
	if req.Messages[n-1].Role == "tool" {
		firstToolIdx := n - 1
		for firstToolIdx >= 0 && req.Messages[firstToolIdx].Role == "tool" {
			firstToolIdx--
		}
		firstToolIdx++ // trỏ vào tin nhắn tool đầu tiên trong chuỗi tool liên tiếp ở cuối

		history, err = buildKiroHistory(req.Messages[:firstToolIdx], req.Model)
		if err != nil {
			return nil, err
		}

		current, err = buildCurrentKiroMessageFromTools(req, req.Messages[firstToolIdx:], req.Model)
		if err != nil {
			return nil, err
		}
	} else {
		lastUser := lastUserMessageIndex(req.Messages)
		if lastUser == -1 {
			return nil, fmt.Errorf("normalized request has no user message")
		}

		history, err = buildKiroHistory(req.Messages[:lastUser], req.Model)
		if err != nil {
			return nil, err
		}
		current, err = buildCurrentKiroMessage(req, req.Messages[lastUser], req.Model)
		if err != nil {
			return nil, err
		}
	}

	cleanHistory, cleanCurrent, droppedIDs := validatePairing(history, current)
	for _, id := range droppedIDs {
		slog.Warn("dropping unpaired tool_use/tool_result", "tool_use_id", id)
	}

	prependSystemPrompt(cleanHistory, &cleanCurrent, req.SystemPrompt)

	maxOutput := req.MaxOutputTokens
	if maxOutput <= 0 {
		maxOutput = 64000 // sensible default matching Kiro's standard model limit
	}

	return &kiro.KiroPayload{
		ConversationState: kiro.ConversationState{
			ConversationID:  uuid.NewString(),
			ChatTriggerType: "MANUAL",
			CurrentMessage:  cleanCurrent,
			History:         cleanHistory,
			InferenceConfig: &kiro.InferenceConfig{
				MaxOutputTokens: maxOutput,
			},
		},
		ProfileArn: profileArn,
	}, nil
}

func buildCurrentKiroMessageFromTools(req *NormalizedRequest, toolMsgs []NormalizedMessage, model string) (kiro.CurrentMessage, error) {
	var results []kiro.ToolResult
	for _, msg := range toolMsgs {
		for _, part := range msg.Parts {
			if p, ok := part.(ToolResult); ok {
				status := "success"
				if p.IsError {
					status = "error"
				}
				results = append(results, kiro.ToolResult{
					ToolUseID: p.ToolUseID,
					Content:   []kiro.ToolResultContent{{Text: p.ContentText}},
					Status:    status,
				})
			}
		}
	}

	tools, err := kiroTools(req.Tools)
	if err != nil {
		return kiro.CurrentMessage{}, err
	}

	var ctx *kiro.UserInputMessageContext
	if len(tools) > 0 || len(results) > 0 {
		ctx = &kiro.UserInputMessageContext{
			Tools:       tools,
			ToolResults: results,
		}
	}

	return kiro.CurrentMessage{UserInputMessage: kiro.UserInputMessage{
		Content:                 "",
		ModelID:                 model,
		Origin:                  "AI_EDITOR",
		UserInputMessageContext: ctx,
	}}, nil
}

func prependSystemPrompt(history []kiro.HistoryItem, current *kiro.CurrentMessage, systemPrompt string) {
	if systemPrompt == "" {
		return
	}
	for i := range history {
		if history[i].UserInputMessage != nil {
			original := history[i].UserInputMessage.Content
			if original == "" {
				history[i].UserInputMessage.Content = systemPrompt
			} else {
				history[i].UserInputMessage.Content = systemPrompt + "\n\n" + original
			}
			return
		}
	}
	original := current.UserInputMessage.Content
	if original == "" {
		current.UserInputMessage.Content = systemPrompt
	} else {
		current.UserInputMessage.Content = systemPrompt + "\n\n" + original
	}
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
			results = append(results, kiro.ToolResult{
				ToolUseID: p.ToolUseID,
				Content:   []kiro.ToolResultContent{{Text: p.ContentText}},
				Status:    status,
			})
		}
	}
	if len(results) == 0 {
		return nil
	}
	return &kiro.UserInputMessage{Content: "", UserInputMessageContext: &kiro.UserInputMessageContext{ToolResults: results}}
}

func buildAssistantResponse(msg NormalizedMessage) *kiro.AssistantResponseMessage {
	var text strings.Builder
	toolUses := make([]kiro.ToolUseEntry, 0)
	for _, part := range msg.Parts {
		switch p := part.(type) {
		case Text:
			text.WriteString(p.Text)
		case ToolUse:
			input := json.RawMessage(p.InputJSON)
			toolUses = append(toolUses, kiro.ToolUseEntry{ToolUseID: p.ID, Name: p.Name, Input: input})
		}
	}
	if text.Len() == 0 && len(toolUses) == 0 {
		return nil
	}
	content := text.String()
	return &kiro.AssistantResponseMessage{Content: content, ToolUses: toolUses}
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

	return kiro.CurrentMessage{UserInputMessage: kiro.UserInputMessage{
		Content:                 content,
		ModelID:                 model,
		Origin:                  "AI_EDITOR",
		UserInputMessageContext: ctx,
		Images:                  images,
	}}, nil
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
		out = append(out, kiro.Tool{ToolSpecification: kiro.ToolSpecification{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: kiro.InputSchema{JSON: copied},
		}})
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
		return history, current, nil
	}

	cleanHistory := make([]kiro.HistoryItem, 0, len(history))
	for _, item := range history {
		clean := kiro.HistoryItem{}
		if item.AssistantResponseMessage != nil {
			assistant := *item.AssistantResponseMessage
			assistant.ToolUses = filterToolUses(assistant.ToolUses, drop)
			// Skip if content is empty or just a placeholder, and there are no tool uses
			if (assistant.Content == "" || assistant.Content == "(empty placeholder)") && len(assistant.ToolUses) == 0 {
				continue
			}
			// Ensure empty content has placeholder
			if assistant.Content == "" {
				assistant.Content = "(empty placeholder)"
			}
			clean.AssistantResponseMessage = &kiro.AssistantResponseMessage{
				Content:  assistant.Content,
				ToolUses: append([]kiro.ToolUseEntry(nil), assistant.ToolUses...),
			}
		}
		if item.UserInputMessage != nil {
			user := cloneUserInputMessage(*item.UserInputMessage)
			if user.UserInputMessageContext != nil {
				user.UserInputMessageContext.ToolResults = filterToolResults(user.UserInputMessageContext.ToolResults, drop)
				if len(user.UserInputMessageContext.Tools) == 0 && len(user.UserInputMessageContext.ToolResults) == 0 {
					user.UserInputMessageContext = nil
				}
			}
			if user.Content == "" && len(user.Images) == 0 && user.UserInputMessageContext == nil {
				continue
			}
			if user.Content == "" {
				user.Content = "(empty placeholder)"
			}
			clean.UserInputMessage = &kiro.UserInputMessage{
				Content:                 user.Content,
				ModelID:                 user.ModelID,
				Origin:                  user.Origin,
				UserInputMessageContext: user.UserInputMessageContext,
				Images:                  append([]kiro.ImagePart(nil), user.Images...),
			}
		}
		cleanHistory = append(cleanHistory, clean)
	}

	cleanCurrent := current
	cleanCurrent.UserInputMessage = cloneUserInputMessage(current.UserInputMessage)
	if cleanCurrent.UserInputMessage.UserInputMessageContext != nil {
		ctx := cleanCurrent.UserInputMessage.UserInputMessageContext
		ctx.ToolResults = filterToolResults(ctx.ToolResults, drop)
		if len(ctx.Tools) == 0 && len(ctx.ToolResults) == 0 {
			cleanCurrent.UserInputMessage.UserInputMessageContext = nil
		}
	}

	return cleanHistory, cleanCurrent, droppedIDs
}

func cloneUserInputMessage(in kiro.UserInputMessage) kiro.UserInputMessage {
	out := in
	out.Images = append([]kiro.ImagePart(nil), in.Images...)
	if in.UserInputMessageContext != nil {
		ctx := *in.UserInputMessageContext
		ctx.Tools = append([]kiro.Tool(nil), in.UserInputMessageContext.Tools...)
		ctx.ToolResults = append([]kiro.ToolResult(nil), in.UserInputMessageContext.ToolResults...)
		out.UserInputMessageContext = &ctx
	}
	return out
}

func filterToolUses(in []kiro.ToolUseEntry, drop map[string]bool) []kiro.ToolUseEntry {
	out := make([]kiro.ToolUseEntry, 0, len(in))
	for _, use := range in {
		if !drop[use.ToolUseID] {
			out = append(out, use)
		}
	}
	return out
}

func filterToolResults(in []kiro.ToolResult, drop map[string]bool) []kiro.ToolResult {
	out := make([]kiro.ToolResult, 0, len(in))
	for _, result := range in {
		if !drop[result.ToolUseID] {
			out = append(out, result)
		}
	}
	return out
}
