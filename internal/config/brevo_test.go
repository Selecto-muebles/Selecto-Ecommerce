package config

import (
	"strings"
	"testing"
)

func TestBrevoValidation(t *testing.T) {
	valid := Config{BrevoContactsEnabled: true, BrevoAPIKey: "isolated-test-key", BrevoListID: 12,
		BrevoAPIBaseURL: "https://api.brevo.com/v3", BrevoWebhookToken: strings.Repeat("x", 32), MarketingWorkerBatchSize: 20}
	for _, tc := range []struct {
		name   string
		change func(*Config)
	}{
		{"missing API key", func(c *Config) { c.BrevoAPIKey = "" }},
		{"missing list", func(c *Config) { c.BrevoListID = 0 }},
		{"insecure endpoint", func(c *Config) { c.BrevoAPIBaseURL = "http://api.brevo.com/v3" }},
		{"short webhook token", func(c *Config) { c.BrevoWebhookToken = "short" }},
		{"disabled batch", func(c *Config) { c.MarketingWorkerBatchSize = 0 }},
		{"unbounded batch", func(c *Config) { c.MarketingWorkerBatchSize = 101 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := valid
			tc.change(&cfg)
			if err := cfg.validateBrevo(); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
	if err := valid.validateBrevo(); err != nil {
		t.Fatal(err)
	}
	if err := (&Config{}).validateBrevo(); err != nil {
		t.Fatal("disabled sync broke legacy deployment", err)
	}
}

func TestLoadBrevoDefaultsDisabled(t *testing.T) {
	for _, key := range []string{"BREVO_CONTACTS_ENABLED", "BREVO_API_KEY", "BREVO_LIST_ID", "BREVO_API_BASE_URL", "BREVO_WEBHOOK_TOKEN", "MARKETING_WORKER_BATCH_SIZE"} {
		t.Setenv(key, "")
	}
	cfg := LoadConfig()
	if cfg.BrevoContactsEnabled || cfg.BrevoListID != 0 || cfg.BrevoAPIKey != "" || cfg.MarketingWorkerBatchSize != 20 || cfg.BrevoAPIBaseURL != "https://api.brevo.com/v3" {
		t.Fatal("unexpected Brevo configuration defaults")
	}
}
