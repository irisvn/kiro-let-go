package openai

import (
	"encoding/json"
	"strings"

	"github.com/irisvn/kiro-let-go/internal/errs"
	"github.com/irisvn/kiro-let-go/internal/normalized"
)

func OpenAIToNormalized(req *ChatCompletionRequest) (*normalized.NormalizedRequest, error) {
	if req == nil {
		return nil, errs.New(errs.ClassFatal, "INVALID_REQUEST", "openai request is nil")
	}

	out := &normalized.NormalizedRequest{
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
		out.ToolChoice = normalized.NormalizedToolChoice{Mode: req.ToolChoice}
	}

	for _, tool := range req.Tools {
		if tool.Type != "" && tool.Type != "function" {
			continue
		}
		if len(tool.Function.Parameters) > 0 && !json.Valid(tool.Function.Parameters) {
			return nil, errs.New(errs.ClassFatal, "INVALID_TOOL_SCHEMA", "tool parameters must be valid JSON")
		}
		out.Tools = append(out.Tools, normalized.NormalizedTool{
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
				if text, ok := part.(normalized.Text); ok {
					systems = append(systems, text.Text)
				}
			}
			continue
		}
		appendNormalizedMessage(out, normalized.NormalizedMessage{Role: msg.Role, Parts: parts})
	}

	out.SystemPrompt = strings.Join(systems, "\n")
	return out, nil
}

func openAIMessageParts(msg ChatMessage) ([]normalized.NormalizedPart, error) {
	var parts []normalized.NormalizedPart
	if msg.Content.Parts != nil {
		for _, part := range msg.Content.Parts {
			switch part.Type {
			case "text":
				parts = append(parts, normalized.Text{Text: part.Text})
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
			parts = append(parts, normalized.ToolResult{ToolUseID: msg.ToolCallID, ContentText: msg.Content.Text})
		} else {
			parts = append(parts, normalized.Text{Text: msg.Content.Text})
		}
	}

	if msg.Role == "assistant" {
		for _, call := range msg.ToolCalls {
			parts = append(parts, normalized.ToolUse{ID: call.ID, Name: call.Function.Name, InputJSON: call.Function.Arguments})
		}
	}

	return parts, nil
}

func openAIImagePart(part ContentPart) (normalized.Image, error) {
	if part.ImageURL == nil {
		return normalized.Image{}, nil
	}
	url := part.ImageURL.URL
	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		return normalized.Image{}, errs.New(errs.ClassFatal, "IMAGE_URL_UNSUPPORTED", "http image URLs are unsupported")
	}
	const prefix = "data:"
	if !strings.HasPrefix(url, prefix) {
		return normalized.Image{}, errs.New(errs.ClassFatal, "IMAGE_URL_UNSUPPORTED", "image URL must be a data URL")
	}
	metaAndData := strings.TrimPrefix(url, prefix)
	meta, data, ok := strings.Cut(metaAndData, ",")
	if !ok {
		return normalized.Image{}, errs.New(errs.ClassFatal, "IMAGE_URL_UNSUPPORTED", "malformed data URL")
	}
	mediaType := strings.TrimSuffix(meta, ";base64")
	if mediaType == meta || mediaType == "" {
		return normalized.Image{}, errs.New(errs.ClassFatal, "IMAGE_URL_UNSUPPORTED", "image data URL must be base64 encoded")
	}
	return normalized.Image{MediaType: mediaType, DataB64: data}, nil
}

func appendNormalizedMessage(req *normalized.NormalizedRequest, msg normalized.NormalizedMessage) {
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
