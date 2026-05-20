package kiro

import (
	"math"
	"sync"
	"unicode/utf8"

	tiktoken "github.com/pkoukk/tiktoken-go"
)

const tokenCorrectionFactor = 1.15

// Estimator counts model tokens using cl100k_base with Kiro's correction factor.
type Estimator struct {
	once sync.Once
	enc  *tiktoken.Tiktoken
	err  error
}

// CountTokens returns an estimated token count for text.
func (e *Estimator) CountTokens(text string) int {
	if text == "" {
		return 0
	}

	if e == nil {
		return correctedTokenCount(fallbackTokenCount(text))
	}

	e.once.Do(func() {
		e.enc, e.err = tiktoken.GetEncoding("cl100k_base")
	})
	if e.err != nil || e.enc == nil {
		return correctedTokenCount(fallbackTokenCount(text))
	}

	return correctedTokenCount(len(e.enc.Encode(text, nil, nil)))
}

func correctedTokenCount(tokens int) int {
	if tokens <= 0 {
		return 0
	}
	return int(math.Ceil(float64(tokens) * tokenCorrectionFactor))
}

func fallbackTokenCount(text string) int {
	runes := utf8.RuneCountInString(text)
	if runes == 0 {
		return 0
	}
	return int(math.Ceil(float64(runes) / 4.0))
}
