package kiro

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

const MaxPayloadBytes = 600 * 1024 // 600KB

const MaxToolNameLength = 63

type ToolNameMapper struct {
	shortened map[string]string
	originals map[string]string
}

func NewToolNameMapper() *ToolNameMapper {
	return &ToolNameMapper{
		shortened: make(map[string]string),
		originals: make(map[string]string),
	}
}

func GuardPayloadSize(payload *KiroPayload) error {
	if payload == nil {
		return nil
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if len(data) <= MaxPayloadBytes {
		return nil
	}

	history := &payload.ConversationState.History
	for len(*history) > 2 {
		size, err := payloadSize(payload)
		if err != nil {
			return err
		}
		if size <= MaxPayloadBytes {
			return nil
		}
		*history = (*history)[2:]
	}

	size, err := payloadSize(payload)
	if err != nil {
		return err
	}
	if size > MaxPayloadBytes {
		return fmt.Errorf("payload too large (%d bytes) even after trimming history", size)
	}
	return nil
}

func payloadSize(payload *KiroPayload) (int, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	return len(data), nil
}

func NormalizeToolSchemas(payload *KiroPayload) {
	if payload == nil {
		return
	}
	msg := &payload.ConversationState.CurrentMessage.UserInputMessage
	if msg.UserInputMessageContext == nil {
		return
	}
	for i := range msg.UserInputMessageContext.Tools {
		tool := &msg.UserInputMessageContext.Tools[i]
		normalizeSchema(&tool.ToolSpecification.InputSchema)
	}
}

func normalizeSchema(schema *InputSchema) {
	if schema == nil || len(schema.JSON) == 0 {
		return
	}

	var obj map[string]any
	if err := json.Unmarshal(schema.JSON, &obj); err != nil {
		return
	}

	modified := false
	if _, ok := obj["type"]; !ok {
		obj["type"] = "object"
		modified = true
	}
	if props, ok := obj["properties"]; ok && props == nil {
		obj["properties"] = map[string]any{}
		modified = true
	}
	if req, ok := obj["required"]; ok && req == nil {
		obj["required"] = []any{}
		modified = true
	}
	if req, ok := obj["required"].([]any); ok && len(req) == 0 {
		delete(obj, "required")
		modified = true
	}
	if _, ok := obj["additionalProperties"]; ok {
		delete(obj, "additionalProperties")
		modified = true
	}

	if modified {
		data, err := json.Marshal(obj)
		if err == nil {
			schema.JSON = data
		}
	}
}

func (m *ToolNameMapper) ShortenNames(payload *KiroPayload) {
	if m == nil || payload == nil {
		return
	}
	msg := &payload.ConversationState.CurrentMessage.UserInputMessage
	if msg.UserInputMessageContext != nil {
		for i := range msg.UserInputMessageContext.Tools {
			tool := &msg.UserInputMessageContext.Tools[i]
			tool.ToolSpecification.Name = m.shorten(tool.ToolSpecification.Name)
		}
	}

	for i := range payload.ConversationState.History {
		item := &payload.ConversationState.History[i]
		if item.AssistantResponseMessage == nil {
			continue
		}
		for j := range item.AssistantResponseMessage.ToolUses {
			item.AssistantResponseMessage.ToolUses[j].Name = m.shorten(item.AssistantResponseMessage.ToolUses[j].Name)
		}
	}
}

func (m *ToolNameMapper) RestoreNames(toolUses []ToolUseEntry) {
	if m == nil {
		return
	}
	for i := range toolUses {
		if original, ok := m.shortened[toolUses[i].Name]; ok {
			toolUses[i].Name = original
		}
	}
}

func (m *ToolNameMapper) RestoreName(name string) string {
	if m == nil {
		return name
	}
	if original, ok := m.shortened[name]; ok {
		return original
	}
	return name
}

func (m *ToolNameMapper) shorten(name string) string {
	if len(name) <= MaxToolNameLength {
		return name
	}
	if short, ok := m.originals[name]; ok {
		return short
	}
	short := shortenToolName(name)
	m.shortened[short] = name
	m.originals[name] = short
	return short
}

func shortenToolName(name string) string {
	h := sha256.Sum256([]byte(name))
	hashSuffix := hex.EncodeToString(h[:4])
	prefix := name[:54]
	return prefix + "_" + hashSuffix
}
