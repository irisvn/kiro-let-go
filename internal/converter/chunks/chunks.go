package chunks

type TextDelta struct{ Text string }

func (TextDelta) Kind() string { return "text_delta" }

type ThinkingDelta struct{ Text string }

func (ThinkingDelta) Kind() string { return "thinking_delta" }

type ToolUseStart struct {
	ID   string
	Name string
}

func (ToolUseStart) Kind() string { return "tool_use_start" }

type ToolUseDelta struct {
	ID         string
	InputDelta string
}

func (ToolUseDelta) Kind() string { return "tool_use_delta" }

type ToolUseStop struct{ ID string }

func (ToolUseStop) Kind() string { return "tool_use_stop" }

type Usage struct {
	InputTokens  int
	OutputTokens int
}

func (Usage) Kind() string { return "usage" }

type ContextUsage struct{ Pct float64 }

func (ContextUsage) Kind() string { return "context_usage" }

type Stop struct{ Reason string }

func (Stop) Kind() string { return "stop" }
