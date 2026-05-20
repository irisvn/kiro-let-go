package openai

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

const modelsCreatedAt int64 = 1735689600

// supportedModels is read-only after package init.
var supportedModels = []ModelInfo{
	{ID: "claude-sonnet-4.5", Object: "model", Created: modelsCreatedAt, OwnedBy: "kiro"},
	{ID: "claude-sonnet-4.6", Object: "model", Created: modelsCreatedAt, OwnedBy: "kiro"},
	{ID: "claude-opus-4.5", Object: "model", Created: modelsCreatedAt, OwnedBy: "kiro"},
	{ID: "claude-opus-4.6", Object: "model", Created: modelsCreatedAt, OwnedBy: "kiro"},
	{ID: "claude-opus-4.7", Object: "model", Created: modelsCreatedAt, OwnedBy: "kiro"},
	{ID: "claude-haiku-4.5", Object: "model", Created: modelsCreatedAt, OwnedBy: "kiro"},
}

// Models returns the supported Kiro models in OpenAI format.
func Models(c *gin.Context) {
	c.JSON(http.StatusOK, ModelsResponse{
		Object: "list",
		Data:   append([]ModelInfo(nil), supportedModels...),
	})
}
