package config

import (
	"errors"
	"net/mail"
	"os"
	"strconv"
	"strings"
	"time"

	"Selecto-Ecommerce/internal/shared/collection"

	"github.com/joho/godotenv"
)

type Config struct {
	Port                    string
	AppEnv                  string
	DatabaseURL             string
	PaymentsServiceURL      string
	JWTSecret               string
	JWTTTL                  time.Duration
	GoogleClientID          string
	InternalWebhookSecret   string
	CORSAllowedOrigins      []string
	RateLimitPerMinute      int
	OrderPendingTTL         time.Duration
	ReleaseWorkerInterval   time.Duration
	PaymentsRequestTimeout  time.Duration
	DatabaseMaxConns        int
	DatabaseMinConns        int
	DatabaseMaxConnLifetime time.Duration
	DatabaseMaxConnIdleTime time.Duration
	ReleaseWorkerBatchSize  int
	ReleaseWorkerMaxBatches int
	StorefrontURL           string
	SMTPHost                string
	SMTPPort                int
	SMTPUsername            string
	SMTPPassword            string
	SMTPFrom                string
	SMTPTLSMode             string
	EmailWorkerInterval     time.Duration
	EmailWorkerBatchSize    int
}

func LoadConfig() *Config {
	_ = godotenv.Load()

	return &Config{
		Port:                    getEnv("PORT", "8080"),
		AppEnv:                  getEnv("APP_ENV", "development"),
		DatabaseURL:             getEnv("DATABASE_URL", ""),
		PaymentsServiceURL:      strings.TrimRight(getEnv("PAYMENTS_SERVICE_URL", ""), "/"),
		JWTSecret:               getEnv("JWT_SECRET", ""),
		JWTTTL:                  getDurationEnv("JWT_TTL", 72*time.Hour),
		GoogleClientID:          strings.TrimSpace(getEnv("GOOGLE_CLIENT_ID", "")),
		InternalWebhookSecret:   getEnv("INTERNAL_WEBHOOK_SECRET", ""),
		CORSAllowedOrigins:      splitCSV(getEnv("CORS_ALLOWED_ORIGINS", "http://localhost:3000,http://localhost:5173")),
		RateLimitPerMinute:      getIntEnv("RATE_LIMIT_PER_MINUTE", 120),
		OrderPendingTTL:         getDurationEnv("ORDER_PENDING_TTL", 15*time.Minute),
		ReleaseWorkerInterval:   getDurationEnv("RELEASE_WORKER_INTERVAL", time.Minute),
		PaymentsRequestTimeout:  getDurationEnv("PAYMENTS_REQUEST_TIMEOUT", 27*time.Second),
		DatabaseMaxConns:        getIntEnv("DB_MAX_CONNS", 20),
		DatabaseMinConns:        getIntEnv("DB_MIN_CONNS", 2),
		DatabaseMaxConnLifetime: getDurationEnv("DB_MAX_CONN_LIFETIME", 30*time.Minute),
		DatabaseMaxConnIdleTime: getDurationEnv("DB_MAX_CONN_IDLE_TIME", 5*time.Minute),
		ReleaseWorkerBatchSize:  getIntEnv("RELEASE_WORKER_BATCH_SIZE", 100),
		ReleaseWorkerMaxBatches: getIntEnv("RELEASE_WORKER_MAX_BATCHES", 10),
		StorefrontURL:           strings.TrimRight(getEnv("STOREFRONT_URL", "http://localhost:5173"), "/"),
		SMTPHost:                strings.TrimSpace(getEnv("SMTP_HOST", "")),
		SMTPPort:                getIntEnv("SMTP_PORT", 587),
		SMTPUsername:            strings.TrimSpace(getEnv("SMTP_USERNAME", "")),
		SMTPPassword:            getEnv("SMTP_PASSWORD", ""),
		SMTPFrom:                strings.TrimSpace(getEnv("SMTP_FROM", "")),
		SMTPTLSMode:             strings.ToLower(strings.TrimSpace(getEnv("SMTP_TLS_MODE", "starttls"))),
		EmailWorkerInterval:     getDurationEnv("EMAIL_WORKER_INTERVAL", 10*time.Second),
		EmailWorkerBatchSize:    getIntEnv("EMAIL_WORKER_BATCH_SIZE", 20),
	}
}

func (c *Config) Validate() error {
	if c.DatabaseURL == "" {
		return errors.New("DATABASE_URL is required")
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
	if c.ReleaseWorkerInterval <= 0 {
		return errors.New("RELEASE_WORKER_INTERVAL must be positive")
	}
	if c.PaymentsRequestTimeout <= 0 || c.PaymentsRequestTimeout >= 30*time.Second {
		return errors.New("PAYMENTS_REQUEST_TIMEOUT must be positive and lower than 30s")
	}
	if c.RateLimitPerMinute <= 0 {
		return errors.New("RATE_LIMIT_PER_MINUTE must be positive")
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
	if c.AppEnv == "production" {
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
		if c.SMTPHost == "" || c.SMTPUsername == "" || c.SMTPPassword == "" || c.SMTPFrom == "" {
			return errors.New("SMTP_HOST, SMTP_USERNAME, SMTP_PASSWORD and SMTP_FROM are required in production")
		}
		if c.SMTPPort <= 0 {
			return errors.New("SMTP_PORT must be configured in production")
		}
		if _, err := mail.ParseAddress(c.SMTPFrom); err != nil {
			return errors.New("SMTP_FROM must be a valid email address")
		}
		if c.EmailWorkerInterval <= 0 || c.EmailWorkerBatchSize <= 0 {
			return errors.New("email worker must be enabled in production")
		}
		if c.SMTPTLSMode == "none" {
			return errors.New("SMTP_TLS_MODE cannot be none in production")
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

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func getDurationEnv(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(getEnv(key, ""))
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return fallback
	}
	return value
}

func getIntEnv(key string, fallback int) int {
	raw := strings.TrimSpace(getEnv(key, ""))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func splitCSV(value string) []string {
	return collection.Filter(
		collection.Map(strings.Split(value, ","), strings.TrimSpace),
		func(part string) bool { return part != "" },
	)
}

func allowsAllOrigins(origins []string) bool {
	return collection.Contains(origins, func(origin string) bool { return origin == "*" })
}
