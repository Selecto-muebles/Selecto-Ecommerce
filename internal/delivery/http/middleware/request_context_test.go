package middleware

import (
	"net/http"
	"testing"
	"time"
)

func TestRequestLogSampler(t *testing.T) {
	sampler := &requestLogSampler{}
	now := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)

	if allowed, _ := sampler.allow(http.MethodGet, "/health", http.StatusOK, now); allowed {
		t.Fatal("successful health checks must not be logged")
	}
	if allowed, _ := sampler.allow(http.MethodGet, "/ready", http.StatusServiceUnavailable, now); !allowed {
		t.Fatal("failed readiness checks must be logged")
	}
	if allowed, suppressed := sampler.allow(http.MethodGet, "/products", http.StatusTooManyRequests, now); !allowed || suppressed != 0 {
		t.Fatalf("first rate-limit response must be logged without suppressed requests: allowed=%v suppressed=%d", allowed, suppressed)
	}
	if allowed, _ := sampler.allow(http.MethodGet, "/products", http.StatusTooManyRequests, now.Add(100*time.Millisecond)); allowed {
		t.Fatal("repeated rate-limit responses inside the sampling interval must be suppressed")
	}
	if allowed, suppressed := sampler.allow(http.MethodGet, "/products", http.StatusTooManyRequests, now.Add(rateLimitLogInterval)); !allowed || suppressed != 1 {
		t.Fatalf("next sampled rate-limit response must report the suppressed count: allowed=%v suppressed=%d", allowed, suppressed)
	}
	if allowed, _ := sampler.allow(http.MethodGet, "/products", http.StatusInternalServerError, now); !allowed {
		t.Fatal("server errors must always be logged")
	}
	if allowed, suppressed := sampler.allow(http.MethodGet, "/products", http.StatusOK, now); !allowed || suppressed != 0 {
		t.Fatalf("first successful read must be logged: allowed=%v suppressed=%d", allowed, suppressed)
	}
	if allowed, _ := sampler.allow(http.MethodGet, "/products", http.StatusOK, now.Add(100*time.Millisecond)); allowed {
		t.Fatal("repeated successful reads inside the sampling interval must be suppressed")
	}
	if allowed, suppressed := sampler.allow(http.MethodGet, "/products", http.StatusOK, now.Add(readLogInterval)); !allowed || suppressed != 1 {
		t.Fatalf("next sampled read must report the suppressed count: allowed=%v suppressed=%d", allowed, suppressed)
	}
	if allowed, _ := sampler.allow(http.MethodPost, "/orders", http.StatusCreated, now); !allowed {
		t.Fatal("mutating requests must always be logged")
	}
}
