package converter

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/irisvn/kiro-let-go/internal/api/anthropic"
)

// AnthropicToNormalized converts an Anthropic Messages request into the shared
// normalized request format.
func AnthropicToNormalized(req *anthropic.MessagesRequest) (*NormalizedRequest, error) {
	if req == nil {
		return nil, fmt.Errorf("anthropic request is nil")
	}

	out := &NormalizedRequest{
		Model:           req.Model,
		Messages:        make([]NormalizedMessage, 0, len(req.Messages)),
		Tools:           make([]NormalizedTool, 0, len(req.Tools)),
		MaxOutputTokens: req.MaxTokens,
		Stop:            append([]string(nil), req.StopSequences...),
		Stream:          req.Stream,
	}
	if req.Temperature != nil {
		out.Temperature = *req.Temperature
	}
	if req.TopP != nil {
		out.TopP = *req.TopP
	}
	if req.TopK != nil {
		out.TopK = *req.TopK
	}
	if req.System != nil {
		out.SystemPrompt = systemText(req.System)
	}
	if req.ToolChoice != nil {
		out.ToolChoice = NormalizedToolChoice{Mode: req.ToolChoice.Type, Name: req.ToolChoice.Name}
	}
	if req.Thinking != nil && req.Thinking.Type == "enabled" {
		out.ThinkingBudget = req.Thinking.BudgetTokens
		if out.ThinkingBudget == 0 {
			out.ThinkingBudget = 4000
		}
	}

	for _, msg := range req.Messages {
		parts, err := messageContentToParts(msg.Content)
		if err != nil {
			return nil, err
		}
		out.Messages = append(out.Messages, NormalizedMessage{Role: msg.Role, Parts: parts})
	}
	for _, tool := range req.Tools {
		schema, err := json.Marshal(tool.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("marshal tool %q schema: %w", tool.Name, err)
		}
		out.Tools = append(out.Tools, NormalizedTool{
			Name:        tool.Name,
			Description: tool.Description,
			SchemaJSON:  string(schema),
		})
	}

	return out, nil
}

// NormalizedToAnthropic assembles normalized non-streaming content into an
// Anthropic Messages response. It preserves normalized parts in message order.
func NormalizedToAnthropic(resp *NormalizedRequest) (*anthropic.MessagesResponse, error) {
	if resp == nil {
		return nil, fmt.Errorf("normalized response is nil")
	}
	blocks := make([]anthropic.ContentBlock, 0)
	role := "assistant"
	for _, msg := range resp.Messages {
		if msg.Role != "" {
			role = msg.Role
		}
		for _, part := range msg.Parts {
			block, err := normalizedPartToAnthropicBlock(part)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, block)
		}
	}
	return &anthropic.MessagesResponse{
		Type:    "message",
		Role:    role,
		Content: blocks,
		Model:   resp.Model,
	}, nil
}

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

func messageContentToParts(content *anthropic.MessageContent) ([]NormalizedPart, error) {
	if content == nil {
		return nil, nil
	}
	if !content.IsArray {
		return []NormalizedPart{Text{Text: content.String}}, nil
	}
	parts := make([]NormalizedPart, 0, len(content.Blocks))
	for _, block := range content.Blocks {
		part, err := anthropicBlockToNormalizedPart(block)
		if err != nil {
			return nil, err
		}
		parts = append(parts, part)
	}
	return parts, nil
}

func anthropicBlockToNormalizedPart(block anthropic.ContentBlock) (NormalizedPart, error) {
	switch b := block.(type) {
	case anthropic.TextBlock:
		return Text{Text: b.Text}, nil
	case anthropic.ImageBlock:
		return Image{MediaType: b.Source.MediaType, DataB64: b.Source.Data}, nil
	case anthropic.ToolUseBlock:
		return ToolUse{ID: b.ID, Name: b.Name, InputJSON: string(b.Input)}, nil
	case anthropic.ToolResultBlock:
		isError := false
		if b.IsError != nil {
			isError = *b.IsError
		}
		return ToolResult{ToolUseID: b.ToolUseID, ContentText: toolResultText(b.Content), IsError: isError}, nil
	case anthropic.ThinkingBlock:
		return Thinking{Text: b.Thinking, Signature: b.Signature}, nil
	default:
		return nil, fmt.Errorf("unsupported anthropic content block %T", block)
	}
}

func toolResultText(content *anthropic.ToolResultContent) string {
	if content == nil {
		return ""
	}
	if !content.IsArray {
		return content.String
	}
	var b strings.Builder
	for _, block := range content.Blocks {
		if text, ok := block.(anthropic.TextBlock); ok {
			b.WriteString(text.Text)
		}
	}
	return b.String()
}

func normalizedPartToAnthropicBlock(part NormalizedPart) (anthropic.ContentBlock, error) {
	switch p := part.(type) {
	case Text:
		return anthropic.TextBlock{Type: "text", Text: p.Text}, nil
	case Image:
		return anthropic.ImageBlock{Type: "image", Source: anthropic.ImageSource{Type: "base64", MediaType: p.MediaType, Data: p.DataB64}}, nil
	case ToolUse:
		input := json.RawMessage(p.InputJSON)
		if len(input) == 0 {
			input = json.RawMessage(`{}`)
		}
		if !json.Valid(input) {
			return nil, fmt.Errorf("tool_use %q input is invalid JSON", p.ID)
		}
		return anthropic.ToolUseBlock{Type: "tool_use", ID: p.ID, Name: p.Name, Input: input}, nil
	case ToolResult:
		isError := p.IsError
		return anthropic.ToolResultBlock{Type: "tool_result", ToolUseID: p.ToolUseID, Content: &anthropic.ToolResultContent{String: p.ContentText, IsArray: false}, IsError: &isError}, nil
	case Thinking:
		return anthropic.ThinkingBlock{Type: "thinking", Thinking: p.Text, Signature: p.Signature}, nil
	default:
		return nil, fmt.Errorf("unsupported normalized part %T", part)
	}
}
