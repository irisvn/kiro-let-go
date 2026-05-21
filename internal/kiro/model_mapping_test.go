package kiro

import (
	"testing"

	"github.com/irisvn/kiro-let-go/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestModelMapperResolveRuleTypes(t *testing.T) {
	tests := []struct {
		name  string
		rules []config.ModelMappingRule
		input string
		want  string
	}{
		{
			name:  "replace",
			input: "gpt-4",
			want:  ModelClaudeSonnet46,
			rules: []config.ModelMappingRule{{Enabled: true, RuleType: "replace", SourceModel: "gpt-4", TargetModels: []string{"sonnet"}}},
		},
		{
			name:  "alias",
			input: "openai/gpt-4o-mini",
			want:  ModelClaudeHaiku45,
			rules: []config.ModelMappingRule{{Enabled: true, RuleType: "alias", SourceModel: "gpt-4o-mini", TargetModels: []string{"haiku"}}},
		},
		{
			name:  "disabled rule ignored",
			input: "gpt-4",
			want:  "gpt-4",
			rules: []config.ModelMappingRule{{Enabled: false, RuleType: "replace", SourceModel: "gpt-4", TargetModels: []string{"sonnet"}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, NewModelMapper(tt.rules).Resolve(tt.input))
		})
	}
}

func TestModelMapperLoadBalance(t *testing.T) {
	mapper := NewModelMapper([]config.ModelMappingRule{{
		Enabled:      true,
		RuleType:     "loadbalance",
		SourceModel:  "gpt-4",
		TargetModels: []string{ModelClaudeSonnet45, ModelClaudeSonnet46},
		Weights:      []int{1, 2},
	}})
	assert.Equal(t, ModelClaudeSonnet45, mapper.Resolve("gpt-4"))
	assert.Equal(t, ModelClaudeSonnet46, mapper.Resolve("gpt-4"))
	assert.Equal(t, ModelClaudeSonnet46, mapper.Resolve("gpt-4"))
}

func TestModelMapperResolveWithFallback(t *testing.T) {
	mapper := NewModelMapper(nil)
	assert.Equal(t, ModelClaudeSonnet46, mapper.ResolveWithFallback(ModelClaudeOpus47, []string{ModelClaudeSonnet46, ModelClaudeHaiku45}))
	assert.Equal(t, ModelClaudeOpus47, mapper.ResolveWithFallback(ModelClaudeOpus47, []string{ModelClaudeOpus47}))
	assert.Equal(t, ModelClaudeOpus47, mapper.ResolveWithFallback(ModelClaudeOpus47, nil))
}
