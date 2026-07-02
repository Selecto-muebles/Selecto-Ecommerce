package middleware

import (
	"net/http"
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
		mu.Unlock()

		if !allowed {
			apperrors.JSON(c, http.StatusTooManyRequests, apperrors.CodeRateLimited, "too many requests", nil)
			c.Abort()
			return
		}

		c.Next()
	}
}
