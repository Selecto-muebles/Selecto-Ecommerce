package config

import "testing"

func TestRateLimitEdgeSecretIsOptionalAndMustBeStrong(t *testing.T) {
	cfg := productionAPIConfig()
	for _, secret := range []string{"", "isolated-edge-secret-at-least-32-characters"} {
		cfg.RateLimitEdgeSecret = secret
		if err := cfg.Validate(); err != nil {
			t.Fatalf("valid edge configuration rejected: %v", err)
		}
	}
	cfg.RateLimitEdgeSecret = "weak"
	if err := cfg.Validate(); err == nil {
		t.Fatal("short edge secret accepted")
	}
	t.Setenv("RATE_LIMIT_EDGE_SECRET", " isolated-edge-secret-at-least-32-characters ")
	if got := LoadConfig().RateLimitEdgeSecret; got != "isolated-edge-secret-at-least-32-characters" {
		t.Fatal("edge configuration not loaded")
	}
}
