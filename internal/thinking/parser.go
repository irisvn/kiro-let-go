package thinking

import "strings"

// State is the parser's finite-state-machine state.
type State int

const (
	PreContent State = iota
	InThinking
	Streaming
)

// Mode controls how thinking tags and their contents are emitted.
type Mode int

const (
	ModeRouteThinking Mode = iota
	ModeStripTags
	ModePassThrough
)

const retainBytes = len("</reasoning>") + 2

var thinkingTags = []string{"thinking", "think", "reasoning", "thought"}

// Parser incrementally separates leading thinking blocks from streamed text.
type Parser struct {
	mode     Mode
	state    State
	pending  string
	closeTag string
}

// NewParser creates a parser in PreContent state.
func NewParser(mode Mode) *Parser {
	return &Parser{mode: mode, state: PreContent}
}

// State returns the current FSM state.
func (p *Parser) State() State {
	return p.state
}

// Feed consumes a chunk and returns visible and thinking output fragments.
func (p *Parser) Feed(chunk string) (visible, thinking string) {
	if p.mode == ModePassThrough {
		return chunk, ""
	}

	data := p.pending + chunk
	p.pending = ""

	for data != "" {
		switch p.state {
		case PreContent:
			v, rest, wait := p.feedPreContent(data)
			visible += v
			if wait {
				return visible, thinking
			}
			data = rest
		case InThinking:
			v, t, rest, wait := p.feedInThinking(data)
			visible += v
			thinking += t
			if wait {
				return visible, thinking
			}
			data = rest
		case Streaming:
			visible += data
			return visible, thinking
		}
	}

	return visible, thinking
}

// Close flushes any buffered bytes that were held for tag-boundary safety.
func (p *Parser) Close() (visible, thinking string) {
	if p.pending == "" {
		return "", ""
	}

	data := p.pending
	p.pending = ""

	if p.mode == ModePassThrough || p.state == PreContent || p.state == Streaming {
		return data, ""
	}
	if p.mode == ModeStripTags {
		return data, ""
	}
	return "", data
}

func (p *Parser) feedPreContent(data string) (visible, rest string, wait bool) {
	first := firstNonWhitespace(data)
	if first == len(data) {
		return data, "", false
	}

	if data[first] != '<' {
		p.state = Streaming
		return data, "", false
	}

	if tag, ok := completeOpeningTag(data[first:]); ok {
		p.state = InThinking
		p.closeTag = "</" + tag + ">"
		return data[:first], data[first+len(tag)+2:], false
	}

	if partialOpeningTag(data[first:]) {
		p.pending = data[first:]
		return data[:first], "", true
	}

	p.state = Streaming
	return data, "", false
}

func (p *Parser) feedInThinking(data string) (visible, thinking, rest string, wait bool) {
	searchFrom := 0
	for {
		idx := strings.Index(data[searchFrom:], p.closeTag)
		if idx < 0 {
			break
		}
		idx += searchFrom
		if quotedFakeTag(data, idx, len(p.closeTag)) {
			searchFrom = idx + len(p.closeTag)
			continue
		}

		v, t := p.thinkingContent(data[:idx])
		closeLen := len(p.closeTag)
		p.state = Streaming
		p.closeTag = ""
		return v, t, data[idx+closeLen:], false
	}

	if len(data) <= retainBytes {
		p.pending = data
		return "", "", "", true
	}

	emitLen := len(data) - retainBytes
	v, t := p.thinkingContent(data[:emitLen])
	p.pending = data[emitLen:]
	return v, t, "", true
}

func (p *Parser) thinkingContent(data string) (visible, thinking string) {
	if p.mode == ModeStripTags {
		return data, ""
	}
	return "", data
}

func completeOpeningTag(data string) (string, bool) {
	for _, tag := range thinkingTags {
		open := "<" + tag + ">"
		if strings.HasPrefix(data, open) {
			return tag, true
		}
	}
	return "", false
}

func partialOpeningTag(data string) bool {
	for _, tag := range thinkingTags {
		open := "<" + tag + ">"
		if len(data) < len(open) && strings.HasPrefix(open, data) {
			return true
		}
	}
	return false
}

func firstNonWhitespace(data string) int {
	for i := 0; i < len(data); i++ {
		if !isWhitespace(data[i]) {
			return i
		}
	}
	return len(data)
}

func isWhitespace(b byte) bool {
	return b == ' ' || b == '\n' || b == '\r' || b == '\t'
}

func quotedFakeTag(data string, idx, tagLen int) bool {
	if idx > 0 && isQuote(data[idx-1]) {
		return true
	}
	end := idx + tagLen
	return end < len(data) && isQuote(data[end])
}

func isQuote(b byte) bool {
	return b == '\'' || b == '"'
}
