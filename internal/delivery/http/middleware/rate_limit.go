package middleware

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"Selecto-Ecommerce/internal/shared/apperrors"

	"github.com/gin-gonic/gin"
)

type rateBucket struct {
	windowStart time.Time
	count       int
}

func RateLimit(limitPerMinute int) gin.HandlerFunc {
	var mu sync.Mutex
	buckets := make(map[string]rateBucket)
	window := time.Minute
	cleanupInterval := window
	retention := 2 * window
	nextCleanup := time.Now().Add(cleanupInterval)

	return func(c *gin.Context) {
		key := c.ClientIP()
		now := time.Now()

		mu.Lock()
		if !now.Before(nextCleanup) {
			for bucketKey, bucket := range buckets {
				if now.Sub(bucket.windowStart) >= retention {
					delete(buckets, bucketKey)
				}
			}
			nextCleanup = now.Add(cleanupInterval)
		}
		bucket := buckets[key]
		if bucket.windowStart.IsZero() || now.Sub(bucket.windowStart) >= window {
			bucket = rateBucket{windowStart: now}
		}
		bucket.count++
		buckets[key] = bucket
		allowed := bucket.count <= limitPerMinute
		remaining := limitPerMinute - bucket.count
		if remaining < 0 {
			remaining = 0
		}
		resetAt := bucket.windowStart.Add(window)
		mu.Unlock()
		c.Header("X-RateLimit-Limit", strconv.Itoa(limitPerMinute))
		c.Header("X-RateLimit-Remaining", strconv.Itoa(remaining))
		c.Header("X-RateLimit-Reset", strconv.FormatInt(resetAt.Unix(), 10))

		if !allowed {
			c.Header("Retry-After", strconv.Itoa(max(1, int(time.Until(resetAt).Seconds()))))
			apperrors.JSON(c, http.StatusTooManyRequests, apperrors.CodeRateLimited, "too many requests", nil)
			c.Abort()
			return
		}

		c.Next()
	}
}
