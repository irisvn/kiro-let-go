package converter

import "github.com/irisvn/kiro-let-go/internal/kiro"

const MaxToolNameLength = kiro.MaxToolNameLength

type ToolNameMapper = kiro.ToolNameMapper

func NewToolNameMapper() *ToolNameMapper {
	return kiro.NewToolNameMapper()
}
