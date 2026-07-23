package middleware

import (
	"log/slog"
	"net/http"
	"sync"
	"time"

	"Selecto-Ecommerce/internal/shared/logging"
	"Selecto-Ecommerce/internal/shared/utils"

	"github.com/gin-gonic/gin"
)

const (
	rateLimitLogInterval = time.Second
	readLogInterval      = time.Second
)

type requestLogSampler struct {
	mu                   sync.Mutex
	lastRateLimitLog     time.Time
	suppressedRateLimits uint64
	lastReadLog          map[string]time.Time
	suppressedReads      map[string]uint64
}

func (s *requestLogSampler) allow(method, path string, status int, now time.Time) (bool, uint64) {
	if (path == "/health" || path == "/ready") && status < http.StatusInternalServerError {
		return false, 0
	}
	if status == http.StatusTooManyRequests {
		s.mu.Lock()
		defer s.mu.Unlock()

		if !s.lastRateLimitLog.IsZero() && now.Sub(s.lastRateLimitLog) < rateLimitLogInterval {
			s.suppressedRateLimits++
			return false, 0
		}

		suppressed := s.suppressedRateLimits
		s.suppressedRateLimits = 0
		s.lastRateLimitLog = now
		return true, suppressed
	}
	if method != http.MethodGet || status >= http.StatusBadRequest {
		return true, 0
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastReadLog == nil {
		s.lastReadLog = make(map[string]time.Time)
		s.suppressedReads = make(map[string]uint64)
	}
	key := method + " " + path
	if last := s.lastReadLog[key]; !last.IsZero() && now.Sub(last) < readLogInterval {
		s.suppressedReads[key]++
		return false, 0
	}

	suppressed := s.suppressedReads[key]
	s.suppressedReads[key] = 0
	s.lastReadLog[key] = now
	return true, suppressed
}

func RequestContext(logger *slog.Logger) gin.HandlerFunc {
	sampler := &requestLogSampler{}

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
		completedAt := time.Now()
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}
		shouldLog, suppressed := sampler.allow(c.Request.Method, path, c.Writer.Status(), completedAt)
		if !shouldLog {
			return
		}

		logger.Info(logging.EventHTTPRouteCompleted,
			"request_id", requestID,
			"correlation_id", correlationID,
			"method", c.Request.Method,
			"path", path,
			"status", c.Writer.Status(),
			"latency_ms", completedAt.Sub(start).Milliseconds(),
			"client_ip", c.ClientIP(),
			"suppressed_since_last", suppressed,
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
