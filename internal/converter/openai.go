package converter

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/irisvn/kiro-let-go/internal/api/openai"
	"github.com/irisvn/kiro-let-go/internal/errs"
)

// NormalizedResponse is the non-streaming normalized assistant response shape.
type NormalizedResponse struct {
	ID         string
	Model      string
	Message    NormalizedMessage
	StopReason string
	Usage      Usage
	Created    int64
}

// OpenAIToNormalized converts an OpenAI chat completion request into the
// provider-neutral request representation.
func OpenAIToNormalized(req *openai.ChatCompletionRequest) (*NormalizedRequest, error) {
	if req == nil {
		return nil, errs.New(errs.ClassFatal, "INVALID_REQUEST", "openai request is nil")
	}

	out := &NormalizedRequest{
		Model:           req.Model,
		MaxOutputTokens: req.MaxTokens,
		Temperature:     req.Temperature,
		TopP:            req.TopP,
		Stop:            append([]string(nil), req.Stop...),
		Stream:          req.Stream,
		ReasoningEffort: req.ReasoningEffort,
		ThinkingBudget:  thinkingBudget(req.ReasoningEffort),
	}

	if req.ToolChoice != "" {
		out.ToolChoice = NormalizedToolChoice{Mode: req.ToolChoice}
	}

	for _, tool := range req.Tools {
		if tool.Type != "" && tool.Type != "function" {
			continue
		}
		if len(tool.Function.Parameters) > 0 && !json.Valid(tool.Function.Parameters) {
			return nil, errs.New(errs.ClassFatal, "INVALID_TOOL_SCHEMA", "tool parameters must be valid JSON")
		}
		out.Tools = append(out.Tools, NormalizedTool{
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			SchemaJSON:  string(tool.Function.Parameters),
		})
	}

	var systems []string
	for _, msg := range req.Messages {
		parts, err := openAIMessageParts(msg)
		if err != nil {
			return nil, err
		}

		if msg.Role == "system" {
			for _, part := range parts {
				if text, ok := part.(Text); ok {
					systems = append(systems, text.Text)
				}
			}
			continue
		}

		appendNormalizedMessage(out, NormalizedMessage{Role: msg.Role, Parts: parts})
	}

	out.SystemPrompt = strings.Join(systems, "\n")
	return out, nil
}

// NormalizedToOpenAI converts a non-streaming normalized assistant response to
// an OpenAI chat completion response.
func NormalizedToOpenAI(resp *NormalizedResponse) (*openai.ChatCompletionResponse, error) {
	if resp == nil {
		return nil, errs.New(errs.ClassFatal, "INVALID_RESPONSE", "normalized response is nil")
	}

	msg := openai.ChatMessage{Role: resp.Message.Role}
	if msg.Role == "" {
		msg.Role = "assistant"
	}

	var text strings.Builder
	for _, part := range resp.Message.Parts {
		switch p := part.(type) {
		case Text:
			text.WriteString(p.Text)
		case ToolUse:
			msg.ToolCalls = append(msg.ToolCalls, openai.ToolCall{
				ID:   p.ID,
				Type: "function",
				Function: openai.ToolCallFunction{
					Name:      p.Name,
					Arguments: p.InputJSON,
				},
			})
		case Thinking:
			continue
		}
	}
	msg.Content = openai.MessageContent{Text: text.String()}

	created := resp.Created
	if created == 0 {
		created = time.Now().Unix()
	}
	finishReason := resp.StopReason
	if finishReason == "" {
		finishReason = "stop"
	}

	return &openai.ChatCompletionResponse{
		ID:      resp.ID,
		Object:  "chat.completion",
		Created: created,
		Model:   resp.Model,
		Choices: []openai.Choice{{
			Index:        0,
			Message:      msg,
			FinishReason: finishReason,
		}},
		Usage: openai.Usage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
		},
	}, nil
}

func openAIMessageParts(msg openai.ChatMessage) ([]NormalizedPart, error) {
	var parts []NormalizedPart
	if msg.Content.Parts != nil {
		for _, part := range msg.Content.Parts {
			switch part.Type {
			case "text":
				parts = append(parts, Text{Text: part.Text})
			case "image_url":
				image, err := openAIImagePart(part)
				if err != nil {
					return nil, err
				}
				parts = append(parts, image)
			}
		}
	} else if msg.Content.Text != "" || msg.Role == "tool" {
		if msg.Role == "tool" {
			parts = append(parts, ToolResult{ToolUseID: msg.ToolCallID, ContentText: msg.Content.Text})
		} else {
			parts = append(parts, Text{Text: msg.Content.Text})
		}
	}

	if msg.Role == "assistant" {
		for _, call := range msg.ToolCalls {
			parts = append(parts, ToolUse{
				ID:        call.ID,
				Name:      call.Function.Name,
				InputJSON: call.Function.Arguments,
			})
		}
	}

	return parts, nil
}

func openAIImagePart(part openai.ContentPart) (Image, error) {
	if part.ImageURL == nil {
		return Image{}, nil
	}
	url := part.ImageURL.URL
	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		return Image{}, errs.New(errs.ClassFatal, "IMAGE_URL_UNSUPPORTED", "http image URLs are unsupported")
	}
	const prefix = "data:"
	if !strings.HasPrefix(url, prefix) {
		return Image{}, errs.New(errs.ClassFatal, "IMAGE_URL_UNSUPPORTED", "image URL must be a data URL")
	}
	metaAndData := strings.TrimPrefix(url, prefix)
	meta, data, ok := strings.Cut(metaAndData, ",")
	if !ok {
		return Image{}, errs.New(errs.ClassFatal, "IMAGE_URL_UNSUPPORTED", "malformed data URL")
	}
	mediaType := strings.TrimSuffix(meta, ";base64")
	if mediaType == meta || mediaType == "" {
		return Image{}, errs.New(errs.ClassFatal, "IMAGE_URL_UNSUPPORTED", "image data URL must be base64 encoded")
	}
	return Image{MediaType: mediaType, DataB64: data}, nil
}

func appendNormalizedMessage(req *NormalizedRequest, msg NormalizedMessage) {
	if len(req.Messages) > 0 && req.Messages[len(req.Messages)-1].Role == msg.Role {
		req.Messages[len(req.Messages)-1].Parts = append(req.Messages[len(req.Messages)-1].Parts, msg.Parts...)
		return
	}
	req.Messages = append(req.Messages, msg)
}

func thinkingBudget(effort string) int {
	switch effort {
	case "none", "minimal", "":
		return 0
	case "low":
		return 2000
	case "medium":
		return 4000
	case "high":
		return 8000
	case "xhigh":
		return 10000
	default:
		return 0
	}
}
