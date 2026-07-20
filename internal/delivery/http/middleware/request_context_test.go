package middleware

import (
	"net/http"
	"testing"
	"time"
)

func TestRequestLogSampler(t *testing.T) {
	sampler := &requestLogSampler{}
	now := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)

	if allowed, _ := sampler.allow("/health", http.StatusOK, now); allowed {
		t.Fatal("successful health checks must not be logged")
	}
	if allowed, _ := sampler.allow("/ready", http.StatusServiceUnavailable, now); !allowed {
		t.Fatal("failed readiness checks must be logged")
	}
	if allowed, suppressed := sampler.allow("/products", http.StatusTooManyRequests, now); !allowed || suppressed != 0 {
		t.Fatalf("first rate-limit response must be logged without suppressed requests: allowed=%v suppressed=%d", allowed, suppressed)
	}
	if allowed, _ := sampler.allow("/products", http.StatusTooManyRequests, now.Add(100*time.Millisecond)); allowed {
		t.Fatal("repeated rate-limit responses inside the sampling interval must be suppressed")
	}
	if allowed, suppressed := sampler.allow("/products", http.StatusTooManyRequests, now.Add(rateLimitLogInterval)); !allowed || suppressed != 1 {
		t.Fatalf("next sampled rate-limit response must report the suppressed count: allowed=%v suppressed=%d", allowed, suppressed)
	}
	if allowed, _ := sampler.allow("/products", http.StatusInternalServerError, now); !allowed {
		t.Fatal("server errors must always be logged")
	}
}
