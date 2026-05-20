package thinking

import "testing"

func TestParserRoutesLeadingThinking(t *testing.T) {
	p := NewParser(ModeRouteThinking)

	visible, thought := p.Feed("<thinking>working</thinking>Hello")
	assertEqual(t, visible, "Hello")
	assertEqual(t, thought, "working")
	assertEqual(t, p.State(), Streaming)
}

func TestParserSupportsThinkingAliasesAndMatchingClose(t *testing.T) {
	tests := []struct {
		name  string
		input string
		think string
		view  string
	}{
		{name: "think", input: "<think>a</think>b", think: "a", view: "b"},
		{name: "reasoning", input: "<reasoning>a</reasoning>b", think: "a", view: "b"},
		{name: "thought", input: "<thought>a</thought>b", think: "a", view: "b"},
		{name: "mismatch", input: "<think>a</thinking>b", think: "a</thinking>b", view: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewParser(ModeRouteThinking)
			visible, thought := p.Feed(tt.input)
			closeVisible, closeThought := p.Close()
			assertEqual(t, visible+closeVisible, tt.view)
			assertEqual(t, thought+closeThought, tt.think)
		})
	}
}

func TestParserRecognizesOpeningOnlyBeforeContent(t *testing.T) {
	p := NewParser(ModeRouteThinking)

	visible, thought := p.Feed("hello <thinking>not routed</thinking>")
	assertEqual(t, visible, "hello <thinking>not routed</thinking>")
	assertEqual(t, thought, "")
	assertEqual(t, p.State(), Streaming)
}

func TestParserAllowsLeadingWhitespaceBeforeOpening(t *testing.T) {
	p := NewParser(ModeRouteThinking)

	visible, thought := p.Feed(" \n<thinking>x</thinking>y")
	assertEqual(t, visible, " \ny")
	assertEqual(t, thought, "x")
}

func TestParserCautiouslyBuffersSplitOpeningTag(t *testing.T) {
	p := NewParser(ModeRouteThinking)

	visible, thought := p.Feed("<thin")
	assertEqual(t, visible, "")
	assertEqual(t, thought, "")

	visible, thought = p.Feed("king>x</thinking>y")
	assertEqual(t, visible, "y")
	assertEqual(t, thought, "x")
}

func TestParserCautiouslyBuffersSplitClosingTag(t *testing.T) {
	p := NewParser(ModeRouteThinking)

	visible, thought := p.Feed("<thinking>abc</thi")
	assertEqual(t, visible, "")
	assertEqual(t, thought, "")

	visible, thought = p.Feed("nking>done")
	assertEqual(t, visible, "done")
	assertEqual(t, thought, "abc")
}

func TestParserNeverSplitsPossibleClosingTagAcrossEmits(t *testing.T) {
	p := NewParser(ModeRouteThinking)

	visible, thought := p.Feed("<thinking>abcdefghijklmnop</thi")
	assertEqual(t, visible, "")
	assertEqual(t, thought, "abcdefg")

	visible, thought = p.Feed("nking>z")
	assertEqual(t, visible, "z")
	assertEqual(t, thought, "hijklmnop")
}

func TestParserStripTagsLeavesContentVisible(t *testing.T) {
	p := NewParser(ModeStripTags)

	visible, thought := p.Feed("<thinking>internal</thinking>external")
	assertEqual(t, visible, "internalexternal")
	assertEqual(t, thought, "")
}

func TestParserPassThroughDoesNotParse(t *testing.T) {
	p := NewParser(ModePassThrough)

	visible, thought := p.Feed("<thinking>x</thinking>y")
	assertEqual(t, visible, "<thinking>x</thinking>y")
	assertEqual(t, thought, "")
}

func TestParserCloseFlushesBufferedBytes(t *testing.T) {
	p := NewParser(ModeRouteThinking)

	visible, thought := p.Feed("<thinking>unfinished")
	assertEqual(t, visible, "")
	assertEqual(t, thought, "")

	visible, thought = p.Close()
	assertEqual(t, visible, "")
	assertEqual(t, thought, "unfinished")
}

func TestParserSkipsQuoteWrappedFakeClosingTag(t *testing.T) {
	p := NewParser(ModeRouteThinking)

	visible, thought := p.Feed("<thinking>say \"</thinking>\" done</thinking>ok")
	assertEqual(t, visible, "ok")
	assertEqual(t, thought, "say \"</thinking>\" done")
}

func assertEqual[T comparable](t *testing.T, got, want T) {
	t.Helper()
	if got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}
