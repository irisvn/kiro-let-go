package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const requestIDKey = "request_id"

func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader("X-Request-ID")
		if requestID == "" {
			requestID = uuid.New().String()
		}

		c.Set(requestIDKey, requestID)
		c.Writer.Header().Set("X-Request-ID", requestID)

		c.Next()
	}
}

func GetRequestID(c *gin.Context) string {
	if val, exists := c.Get(requestIDKey); exists {
		if s, ok := val.(string); ok {
			return s
		}
	}
	return ""
}
