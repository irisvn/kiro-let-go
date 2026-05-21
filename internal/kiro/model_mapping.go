package kiro

import (
	"strings"
	"sync/atomic"

	"github.com/irisvn/kiro-let-go/internal/config"
)

// ModelMapper resolves incoming model names through normalization and optional rules.
type ModelMapper struct {
	rules   []config.ModelMappingRule
	counter atomic.Uint64
}

// NewModelMapper creates a mapper from config rules.
func NewModelMapper(rules []config.ModelMappingRule) *ModelMapper {
	return &ModelMapper{rules: rules}
}

// Resolve normalizes input and applies the first enabled matching mapping rule.
func (m *ModelMapper) Resolve(input string) string {
	normalized := MapModel(input)
	if m == nil {
		return normalized
	}
	for _, rule := range m.rules {
		if !rule.Enabled || normalizeModelInput(rule.SourceModel) != normalizeModelInput(normalized) || len(rule.TargetModels) == 0 {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(rule.RuleType)) {
		case "replace", "alias":
			return MapModel(rule.TargetModels[0])
		case "loadbalance":
			return MapModel(m.pickWeighted(rule))
		}
	}
	return normalized
}

func (m *ModelMapper) pickWeighted(rule config.ModelMappingRule) string {
	if len(rule.TargetModels) == 0 {
		return ""
	}
	if len(rule.Weights) == 0 || len(rule.Weights) != len(rule.TargetModels) {
		idx := (m.counter.Add(1) - 1) % uint64(len(rule.TargetModels))
		return rule.TargetModels[idx]
	}
	totalWeight := 0
	for _, w := range rule.Weights {
		if w > 0 {
			totalWeight += w
		}
	}
	if totalWeight == 0 {
		return rule.TargetModels[0]
	}
	tick := int((m.counter.Add(1) - 1) % uint64(totalWeight))
	cumulative := 0
	for i, w := range rule.Weights {
		if w <= 0 {
			continue
		}
		cumulative += w
		if tick < cumulative {
			return rule.TargetModels[i]
		}
	}
	return rule.TargetModels[0]
}

var fallbackChains = map[string][]string{
	ModelClaudeOpus47:   {ModelClaudeOpus46, ModelClaudeOpus45, ModelClaudeSonnet46},
	ModelClaudeOpus46:   {ModelClaudeOpus45, ModelClaudeSonnet46},
	ModelClaudeOpus45:   {ModelClaudeSonnet46, ModelClaudeSonnet45},
	ModelClaudeSonnet46: {ModelClaudeSonnet45},
}

// ResolveWithFallback resolves input and downgrades to the first available fallback when needed.
func (m *ModelMapper) ResolveWithFallback(input string, availableModels []string) string {
	resolved := m.Resolve(input)
	if len(availableModels) == 0 {
		return resolved
	}
	available := make(map[string]struct{}, len(availableModels))
	for _, model := range availableModels {
		available[MapModel(model)] = struct{}{}
	}
	if _, ok := available[resolved]; ok {
		return resolved
	}
	for _, fallback := range fallbackChains[resolved] {
		if _, ok := available[fallback]; ok {
			return fallback
		}
	}
	return resolved
}
