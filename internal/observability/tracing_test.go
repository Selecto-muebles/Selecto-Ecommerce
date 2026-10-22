package observability

import (
	"context"
	"testing"
)

func TestValidateEndpoint(t *testing.T) {
	for _, tc := range []struct {
		endpoint, environment string
		valid                 bool
	}{
		{"https://collector.example/v1/traces", "production", true},
		{"http://localhost:4318/v1/traces", "development", true},
		{"http://collector.example/v1/traces", "production", false},
		{"https://user:password@collector.example/v1/traces", "production", false},
		{"https://collector.example/v1/traces?token=secret", "production", false},
		{"not-a-url", "development", false},
	} {
		err := ValidateEndpoint(tc.endpoint, tc.environment)
		if (err == nil) != tc.valid {
			t.Errorf("ValidateEndpoint(%q, %q) = %v, valid=%v", tc.endpoint, tc.environment, err, tc.valid)
		}
	}
}

func TestTracingDisabledByDefault(t *testing.T) {
	shutdown, err := InitTracing(context.Background(), "", "production")
	if err != nil || shutdown == nil {
		t.Fatalf("disabled tracing must not affect startup: shutdown=%v err=%v", shutdown != nil, err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("disabled tracing shutdown: %v", err)
	}
}
