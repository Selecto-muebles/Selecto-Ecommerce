package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"Selecto-Ecommerce/internal/shared/logging"
	"Selecto-Ecommerce/internal/shared/utils"

	"github.com/gin-gonic/gin"
)

func RequestContext(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader("X-Request-ID")
		if requestID == "" {
			requestID = utils.NewRequestID()
		}
		correlationID := c.GetHeader("X-Correlation-ID")
		if correlationID == "" {
			correlationID = requestID
		}

		c.Set("request_id", requestID)
		c.Set("correlation_id", correlationID)
		c.Writer.Header().Set("X-Request-ID", requestID)
		c.Writer.Header().Set("X-Correlation-ID", correlationID)

		start := time.Now()
		c.Next()

		logger.Info(logging.EventHTTPRouteCompleted,
			"request_id", requestID,
			"correlation_id", correlationID,
			"method", c.Request.Method,
			"path", c.FullPath(),
			"status", c.Writer.Status(),
			"latency_ms", time.Since(start).Milliseconds(),
			"client_ip", c.ClientIP(),
		)
	}
}

func RequestID(c *gin.Context) string {
	value, _ := c.Get("request_id")
	if id, ok := value.(string); ok {
		return id
	}
	return ""
}

func CorrelationID(c *gin.Context) string {
	value, _ := c.Get("correlation_id")
	if id, ok := value.(string); ok {
		return id
	}
	return ""
}

func NoCache(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusNoContent)
}
