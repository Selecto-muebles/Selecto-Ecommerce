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

type rateBucketKey struct {
	client string
	policy string
}

func RateLimit(limitPerMinute int) gin.HandlerFunc {
	return rateLimitWithClock(limitPerMinute, time.Now)
}

func rateLimitWithClock(limitPerMinute int, clock func() time.Time) gin.HandlerFunc {
	var mu sync.Mutex
	buckets := make(map[rateBucketKey]rateBucket)
	window := time.Minute
	cleanupInterval := window
	retention := 2 * window
	nextCleanup := clock().Add(cleanupInterval)

	return func(c *gin.Context) {
		if c.Request.URL.Path == "/health" || c.Request.URL.Path == "/ready" {
			c.Next()
			return
		}
		policy, limit := rateLimitPolicy(c, limitPerMinute)
		key := rateBucketKey{client: rateLimitClient(c), policy: policy}
		now := clock()

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
		allowed := bucket.count <= limit
		remaining := limit - bucket.count
		if remaining < 0 {
			remaining = 0
		}
		resetAt := bucket.windowStart.Add(window)
		mu.Unlock()
		c.Set("rate_limit_policy", policy)
		c.Header("X-RateLimit-Limit", strconv.Itoa(limit))
		c.Header("X-RateLimit-Remaining", strconv.Itoa(remaining))
		c.Header("X-RateLimit-Reset", strconv.FormatInt(resetAt.Unix(), 10))

		if !allowed {
			// Round up: the browser must not retry before the window expires.
			retryAfter := (resetAt.Sub(now) + time.Second - 1) / time.Second
			c.Header("Retry-After", strconv.Itoa(max(1, int(retryAfter))))
			c.Header("Cache-Control", "no-store")
			apperrors.JSON(c, http.StatusTooManyRequests, apperrors.CodeRateLimited, "too many requests", nil)
			c.Abort()
			return
		}

		c.Next()
	}
}
