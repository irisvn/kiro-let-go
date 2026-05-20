package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
)

func RecoverMiddleware(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				msg := fmt.Sprintf("%v", r)
				requestID := GetRequestID(c)
				attrs := []slog.Attr{
					slog.String("panic", msg),
					slog.String("stack", string(debug.Stack())),
				}
				if requestID != "" {
					attrs = append(attrs, slog.String("request_id", requestID))
				}
				logger.LogAttrs(c.Request.Context(), slog.LevelError, "panic recovered", attrs...)

				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"error": gin.H{
						"type":    "internal_error",
						"message": "internal server error",
					},
				})
			}
		}()

		c.Next()
	}
}
