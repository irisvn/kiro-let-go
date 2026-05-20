package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func ProxyAuthMiddleware(proxyAPIKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := extractAPIKey(c)
		if key == "" || key != proxyAPIKey {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{
					"type":    "authentication_error",
					"message": "invalid or missing proxy API key",
				},
			})
			return
		}
		c.Next()
	}
}

func AdminAuthMiddleware(adminAPIKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		key, _ := strings.CutPrefix(auth, "Bearer ")
		if key == "" || key != adminAPIKey {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{
					"type":    "authentication_error",
					"message": "invalid or missing admin API key",
				},
			})
			return
		}
		c.Next()
	}
}

func extractAPIKey(c *gin.Context) string {
	auth := c.GetHeader("Authorization")
	if key, ok := strings.CutPrefix(auth, "Bearer "); ok {
		return key
	}
	return c.GetHeader("x-api-key")
}
