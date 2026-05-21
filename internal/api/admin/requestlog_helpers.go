package admin

import (
	"encoding/json"

	"github.com/gin-gonic/gin"
	"github.com/irisvn/kiro-let-go/internal/kiro"
)

const (
	requestLogKeyAccountID    = "rl_account_id"
	requestLogKeyAccountLabel = "rl_account_label"
	requestLogKeyInputTokens  = "rl_input_tokens"
	requestLogKeyOutputTokens = "rl_output_tokens"
	requestLogKeyKiroPayload  = "rl_kiro_payload"
	requestLogPayloadLimit    = 50000 // Tăng từ 5000 lên 50000
)

func setRequestLogAccount(c *gin.Context, accountID, accountLabel string) {
	if c == nil {
		return
	}
	c.Set(requestLogKeyAccountID, accountID)
	c.Set(requestLogKeyAccountLabel, accountLabel)
}

func setRequestLogUsage(c *gin.Context, usage kiro.Usage) {
	if c == nil {
		return
	}
	c.Set(requestLogKeyInputTokens, usage.InputTokens)
	c.Set(requestLogKeyOutputTokens, usage.OutputTokens)
}

func setRequestLogKiroPayload(c *gin.Context, payload *kiro.KiroPayload) {
	if c == nil || payload == nil {
		return
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	c.Set(requestLogKeyKiroPayload, truncateRequestLogPayload(string(body)))
}

func setRequestLogKiroPayloadBytes(c *gin.Context, body []byte) {
	if c == nil || len(body) == 0 {
		return
	}
	c.Set(requestLogKeyKiroPayload, truncateRequestLogPayload(string(body)))
}

func truncateRequestLogPayload(value string) string {
	if len(value) > requestLogPayloadLimit {
		return value[:requestLogPayloadLimit] + "..."
	}
	return value
}
