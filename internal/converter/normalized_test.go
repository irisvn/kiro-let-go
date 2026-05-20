package converter

import (
	"testing"
)

func TestNormalizedPartKinds(t *testing.T) {
	tests := []struct {
		name     string
		part     NormalizedPart
		wantKind string
	}{
		{"Text", Text{Text: "hello"}, "text"},
		{"Image", Image{MediaType: "image/png", DataB64: "abc123"}, "image"},
		{"ToolUse", ToolUse{ID: "tu_1", Name: "calculator", InputJSON: `{}`}, "tool_use"},
		{"ToolResult", ToolResult{ToolUseID: "tu_1", ContentText: "42", IsError: false}, "tool_result"},
		{"Thinking", Thinking{Text: "reasoning...", Signature: "sig"}, "thinking"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.part.Kind(); got != tt.wantKind {
				t.Errorf("Kind() = %q, want %q", got, tt.wantKind)
			}
		})
	}
}

func TestNormalizedChunkKinds(t *testing.T) {
	tests := []struct {
		name     string
		chunk    NormalizedChunk
		wantKind string
	}{
		{"TextDelta", TextDelta{Text: "delta"}, "text_delta"},
		{"ThinkingDelta", ThinkingDelta{Text: "think"}, "thinking_delta"},
		{"ToolUseStart", ToolUseStart{ID: "tu_1", Name: "calc"}, "tool_use_start"},
		{"ToolUseDelta", ToolUseDelta{ID: "tu_1", InputDelta: "{"}, "tool_use_delta"},
		{"ToolUseStop", ToolUseStop{ID: "tu_1"}, "tool_use_stop"},
		{"Usage", Usage{InputTokens: 10, OutputTokens: 5}, "usage"},
		{"ContextUsage", ContextUsage{Pct: 0.75}, "context_usage"},
		{"Stop", Stop{Reason: "end_turn"}, "stop"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.chunk.Kind(); got != tt.wantKind {
				t.Errorf("Kind() = %q, want %q", got, tt.wantKind)
			}
		})
	}
}

func TestNormalizedRequestConstruction(t *testing.T) {
	req := NormalizedRequest{
		Model:           "claude-3-opus",
		SystemPrompt:    "You are helpful.",
		Messages:        []NormalizedMessage{{Role: "user", Parts: []NormalizedPart{Text{Text: "hi"}}}},
		Tools:           []NormalizedTool{{Name: "calc", Description: "A calculator", SchemaJSON: `{}`}},
		ToolChoice:      NormalizedToolChoice{Mode: "auto", Name: ""},
		MaxOutputTokens: 4096,
		Temperature:     0.7,
		TopP:            0.9,
		TopK:            50,
		Stop:            []string{"STOP"},
		ThinkingBudget:  1024,
		Stream:          true,
		ReasoningEffort: "medium",
	}

	if req.Model != "claude-3-opus" {
		t.Errorf("Model = %q, want claude-3-opus", req.Model)
	}
	if len(req.Messages) != 1 {
		t.Fatalf("len(Messages) = %d, want 1", len(req.Messages))
	}
	if req.Messages[0].Role != "user" {
		t.Errorf("Messages[0].Role = %q, want user", req.Messages[0].Role)
	}
	if len(req.Tools) != 1 {
		t.Fatalf("len(Tools) = %d, want 1", len(req.Tools))
	}
	if req.Tools[0].Name != "calc" {
		t.Errorf("Tools[0].Name = %q, want calc", req.Tools[0].Name)
	}
	if req.ToolChoice.Mode != "auto" {
		t.Errorf("ToolChoice.Mode = %q, want auto", req.ToolChoice.Mode)
	}
	if !req.Stream {
		t.Error("Stream = false, want true")
	}
	if req.ReasoningEffort != "medium" {
		t.Errorf("ReasoningEffort = %q, want medium", req.ReasoningEffort)
	}
}

func TestNormalizedPartExhaustiveSwitch(t *testing.T) {
	var k string
	switch p := (NormalizedPart)(Text{Text: "x"}).(type) {
	case Text:
		k = p.Kind()
	case Image:
		k = p.Kind()
	case ToolUse:
		k = p.Kind()
	case ToolResult:
		k = p.Kind()
	case Thinking:
		k = p.Kind()
	default:
		t.Fatal("unhandled NormalizedPart variant")
	}
	if k != "text" {
		t.Errorf("switch result = %q, want text", k)
	}
}

func TestNormalizedChunkExhaustiveSwitch(t *testing.T) {
	var k string
	switch c := (NormalizedChunk)(Usage{InputTokens: 1, OutputTokens: 2}).(type) {
	case TextDelta:
		k = c.Kind()
	case ThinkingDelta:
		k = c.Kind()
	case ToolUseStart:
		k = c.Kind()
	case ToolUseDelta:
		k = c.Kind()
	case ToolUseStop:
		k = c.Kind()
	case Usage:
		k = c.Kind()
	case ContextUsage:
		k = c.Kind()
	case Stop:
		k = c.Kind()
	default:
		t.Fatal("unhandled NormalizedChunk variant")
	}
	if k != "usage" {
		t.Errorf("switch result = %q, want usage", k)
	}
}

func TestNormalizedMessageWithMultipleParts(t *testing.T) {
	msg := NormalizedMessage{
		Role: "assistant",
		Parts: []NormalizedPart{
			Text{Text: "Let me think."},
			Thinking{Text: "analysis", Signature: "sig"},
			ToolUse{ID: "tu_1", Name: "search", InputJSON: `{"q":"x"}`},
			ToolResult{ToolUseID: "tu_1", ContentText: "result", IsError: false},
			Image{MediaType: "image/jpeg", DataB64: "data"},
		},
	}

	if len(msg.Parts) != 5 {
		t.Fatalf("len(Parts) = %d, want 5", len(msg.Parts))
	}

	expected := []string{"text", "thinking", "tool_use", "tool_result", "image"}
	for i, want := range expected {
		if got := msg.Parts[i].Kind(); got != want {
			t.Errorf("Parts[%d].Kind() = %q, want %q", i, got, want)
		}
	}
}
