package config

import (
	"testing"
	"time"
)

func TestLoadConfigScalabilityDefaults(t *testing.T) {
	for _, key := range []string{
		"DB_MAX_CONNS",
		"DB_MIN_CONNS",
		"DB_MAX_CONN_LIFETIME",
		"DB_MAX_CONN_IDLE_TIME",
		"RELEASE_WORKER_BATCH_SIZE",
		"RELEASE_WORKER_MAX_BATCHES",
	} {
		t.Setenv(key, "")
	}
	cfg := LoadConfig()
	if cfg.DatabaseMaxConns != 20 || cfg.DatabaseMinConns != 2 || cfg.ReleaseWorkerBatchSize != 100 || cfg.ReleaseWorkerMaxBatches != 10 {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.DatabaseMaxConnLifetime != 30*time.Minute || cfg.DatabaseMaxConnIdleTime != 5*time.Minute {
		t.Fatalf("unexpected connection lifetimes: %+v", cfg)
	}
}

func TestValidateRejectsUnsafePoolAndWorkerLimits(t *testing.T) {
	base := Config{
		DatabaseURL:             "postgres://example",
		JWTSecret:               "test-secret-with-at-least-32-characters",
		JWTTTL:                  time.Hour,
		RateLimitPerMinute:      120,
		OrderPendingTTL:         15 * time.Minute,
		ReleaseWorkerInterval:   time.Minute,
		PaymentsRequestTimeout:  27 * time.Second,
		DatabaseMaxConns:        10,
		DatabaseMinConns:        2,
		DatabaseMaxConnLifetime: 30 * time.Minute,
		DatabaseMaxConnIdleTime: 5 * time.Minute,
		ReleaseWorkerBatchSize:  100,
		ReleaseWorkerMaxBatches: 10,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("safe config validation error = %v", err)
	}
	base.DatabaseMinConns = 11
	if err := base.Validate(); err == nil {
		t.Fatal("unsafe pool validation error = nil")
	}
	base.DatabaseMinConns = 2
	base.ReleaseWorkerBatchSize = 0
	if err := base.Validate(); err == nil {
		t.Fatal("unsafe worker validation error = nil")
	}
}

func TestValidateRequiresOperationalEmailInProduction(t *testing.T) {
	cfg := Config{
		AppEnv:                  "production",
		DatabaseURL:             "postgres://example",
		PaymentsServiceURL:      "http://payments:8081",
		JWTSecret:               "test-secret-with-at-least-32-characters",
		JWTTTL:                  time.Hour,
		GoogleClientID:          "client.apps.googleusercontent.com",
		InternalWebhookSecret:   "internal-secret-with-at-least-32-chars",
		CORSAllowedOrigins:      []string{"https://tienda.example"},
		RateLimitPerMinute:      120,
		OrderPendingTTL:         15 * time.Minute,
		ReleaseWorkerInterval:   time.Minute,
		PaymentsRequestTimeout:  27 * time.Second,
		DatabaseMaxConns:        10,
		DatabaseMinConns:        2,
		DatabaseMaxConnLifetime: 30 * time.Minute,
		DatabaseMaxConnIdleTime: 5 * time.Minute,
		ReleaseWorkerBatchSize:  100,
		ReleaseWorkerMaxBatches: 10,
		StorefrontURL:           "https://tienda.example",
		SMTPHost:                "smtp.example",
		SMTPPort:                587,
		SMTPUsername:            "selecto-smtp",
		SMTPPassword:            "smtp-secret",
		SMTPFrom:                "Selecto <ventas@tienda.example>",
		SMTPTLSMode:             "starttls",
		EmailWorkerInterval:     10 * time.Second,
		EmailWorkerBatchSize:    20,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("production email config validation error = %v", err)
	}
	cfg.SMTPPassword = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("missing SMTP authentication validation error = nil")
	}
	cfg.SMTPPassword = "smtp-secret"
	cfg.SMTPFrom = "invalid"
	if err := cfg.Validate(); err == nil {
		t.Fatal("invalid SMTP_FROM validation error = nil")
	}
	cfg.SMTPFrom = "ventas@tienda.example"
	cfg.EmailWorkerBatchSize = 0
	if err := cfg.Validate(); err == nil {
		t.Fatal("disabled production email worker validation error = nil")
	}
}
