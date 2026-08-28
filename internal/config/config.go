package config

import (
	"errors"
	"net/mail"
	"strings"
	"time"
)

type Config struct {
	Port                      string
	AppEnv                    string
	DatabaseURL               string
	DatabaseSchema            string
	PaymentsServiceURL        string
	PaymentsIDTokenAudience   string
	JWTSecret                 string
	JWTTTL                    time.Duration
	GoogleClientID            string
	InternalWebhookSecret     string
	CORSAllowedOrigins        []string
	RateLimitPerMinute        int
	OrderPendingTTL           time.Duration
	ReleaseWorkerInterval     time.Duration
	PaymentsRequestTimeout    time.Duration
	DatabaseMaxConns          int
	DatabaseMinConns          int
	DatabaseMaxConnLifetime   time.Duration
	DatabaseMaxConnIdleTime   time.Duration
	ReleaseWorkerBatchSize    int
	ReleaseWorkerMaxBatches   int
	StorefrontURL             string
	SMTPHost                  string
	SMTPPort                  int
	SMTPUsername              string
	SMTPPassword              string
	SMTPFrom                  string
	SMTPTLSMode               string
	EmailWorkerInterval       time.Duration
	EmailWorkerBatchSize      int
	EmbeddedWorkers           bool
	JobTimeout                time.Duration
	EmailTasksEnabled         bool
	EmailTasksProject         string
	EmailTasksLocation        string
	EmailTasksQueue           string
	EmailTasksWorkerURL       string
	EmailTasksServiceAccount  string
	EmailTasksAudience        string
	EmailTasksDispatchTimeout time.Duration
}

func (c *Config) Validate() error {
	if err := c.validateDatabase(); err != nil {
		return err
	}
	if c.JWTSecret == "" {
		return errors.New("JWT_SECRET is required")
	}
	if len(c.JWTSecret) < 32 {
		return errors.New("JWT_SECRET must be at least 32 characters")
	}
	if c.JWTTTL <= 0 {
		return errors.New("JWT_TTL must be positive")
	}
	if c.OrderPendingTTL <= 0 {
		return errors.New("ORDER_PENDING_TTL must be positive")
	}
	if c.EmbeddedWorkers && c.ReleaseWorkerInterval <= 0 {
		return errors.New("RELEASE_WORKER_INTERVAL must be positive")
	}
	if c.PaymentsRequestTimeout <= 0 || c.PaymentsRequestTimeout >= 30*time.Second {
		return errors.New("PAYMENTS_REQUEST_TIMEOUT must be positive and lower than 30s")
	}
	if c.RateLimitPerMinute <= 0 {
		return errors.New("RATE_LIMIT_PER_MINUTE must be positive")
	}
	if c.ReleaseWorkerBatchSize <= 0 || c.ReleaseWorkerBatchSize > 1000 {
		return errors.New("RELEASE_WORKER_BATCH_SIZE must be between 1 and 1000")
	}
	if c.ReleaseWorkerMaxBatches <= 0 || c.ReleaseWorkerMaxBatches > 100 {
		return errors.New("RELEASE_WORKER_MAX_BATCHES must be between 1 and 100")
	}
	if c.SMTPPort != 0 && (c.SMTPPort < 0 || c.SMTPPort > 65535) {
		return errors.New("SMTP_PORT must be between 1 and 65535")
	}
	if c.SMTPTLSMode != "" && c.SMTPTLSMode != "starttls" && c.SMTPTLSMode != "tls" && c.SMTPTLSMode != "none" {
		return errors.New("SMTP_TLS_MODE must be starttls, tls or none")
	}
	if c.EmailWorkerInterval < 0 || c.EmailWorkerBatchSize < 0 || c.EmailWorkerBatchSize > 100 {
		return errors.New("email worker settings are invalid")
	}
	if err := validateIDTokenAudience(c.PaymentsIDTokenAudience); err != nil {
		return err
	}
	if err := validateAudienceTarget(c.PaymentsIDTokenAudience, c.PaymentsServiceURL); err != nil {
		return err
	}
	if err := c.validateEmailTasks(); err != nil {
		return err
	}
	if isCloudRunURL(c.PaymentsServiceURL) && c.PaymentsIDTokenAudience == "" {
		return errors.New("PAYMENTS_ID_TOKEN_AUDIENCE is required for a private Cloud Run payments service")
	}
	if c.AppEnv == "production" {
		if c.EmbeddedWorkers {
			return errors.New("RUN_EMBEDDED_WORKERS must be false in production")
		}
		if c.StorefrontURL == "" {
			return errors.New("STOREFRONT_URL is required in production")
		}
		if c.PaymentsServiceURL == "" {
			return errors.New("PAYMENTS_SERVICE_URL is required in production")
		}
		if len(c.InternalWebhookSecret) < 32 {
			return errors.New("INTERNAL_WEBHOOK_SECRET must be at least 32 characters in production")
		}
		if c.JWTTTL > 24*time.Hour {
			return errors.New("JWT_TTL cannot exceed 24 hours in production")
		}
		if c.GoogleClientID == "" {
			return errors.New("GOOGLE_CLIENT_ID is required in production")
		}
		if !strings.HasPrefix(c.StorefrontURL, "https://") {
			return errors.New("STOREFRONT_URL must use HTTPS in production")
		}
		if allowsAllOrigins(c.CORSAllowedOrigins) {
			return errors.New("CORS_ALLOWED_ORIGINS cannot allow all origins in production")
		}
	}
	return nil
}

func (c *Config) ValidateJob(name string) error {
	if err := c.validateDatabase(); err != nil {
		return err
	}
	if c.JobTimeout <= 0 || c.JobTimeout > time.Hour {
		return errors.New("JOB_TIMEOUT must be positive and at most 1h")
	}
	switch name {
	case "expire-orders":
		if c.OrderPendingTTL <= 0 || c.ReleaseWorkerBatchSize <= 0 || c.ReleaseWorkerBatchSize > 1000 {
			return errors.New("expired order job settings are invalid")
		}
		if c.ReleaseWorkerMaxBatches <= 0 || c.ReleaseWorkerMaxBatches > 100 {
			return errors.New("RELEASE_WORKER_MAX_BATCHES must be between 1 and 100")
		}
	case "email-outbox":
		if c.EmailWorkerBatchSize <= 0 || c.EmailWorkerBatchSize > 100 {
			return errors.New("EMAIL_WORKER_BATCH_SIZE must be between 1 and 100")
		}
		if err := c.validateSMTP(); err != nil {
			return err
		}
	default:
		return errors.New("unknown job")
	}
	return nil
}

func (c *Config) validateDatabase() error {
	if c.DatabaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}
	if c.DatabaseSchema != "public" && c.DatabaseSchema != "commerce" {
		return errors.New("DB_SCHEMA must be public or commerce")
	}
	if c.DatabaseMaxConns <= 0 || c.DatabaseMaxConns > 200 {
		return errors.New("DB_MAX_CONNS must be between 1 and 200")
	}
	if c.DatabaseMinConns < 0 || c.DatabaseMinConns > c.DatabaseMaxConns {
		return errors.New("DB_MIN_CONNS must be between 0 and DB_MAX_CONNS")
	}
	if c.DatabaseMaxConnLifetime <= 0 || c.DatabaseMaxConnIdleTime <= 0 {
		return errors.New("database connection lifetimes must be positive")
	}
	return nil
}

func (c *Config) validateSMTP() error {
	if c.SMTPHost == "" || c.SMTPUsername == "" || c.SMTPPassword == "" || c.SMTPFrom == "" {
		return errors.New("SMTP_HOST, SMTP_USERNAME, SMTP_PASSWORD and SMTP_FROM are required")
	}
	if c.SMTPPort <= 0 {
		return errors.New("SMTP_PORT must be configured")
	}
	if _, err := mail.ParseAddress(c.SMTPFrom); err != nil {
		return errors.New("SMTP_FROM must be a valid email address")
	}
	if c.SMTPTLSMode == "none" {
		return errors.New("SMTP_TLS_MODE cannot be none")
	}
	return nil
}
