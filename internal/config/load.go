package config

import (
	"os"
	"strconv"
	"strings"
	"time"

	"Selecto-Ecommerce/internal/shared/collection"
	"github.com/joho/godotenv"
)

func LoadConfig() *Config {
	_ = godotenv.Load()

	return &Config{
		Port:                      getEnv("PORT", "8080"),
		AppEnv:                    getEnv("APP_ENV", "development"),
		DatabaseURL:               getEnv("DATABASE_URL", ""),
		DatabaseSchema:            getNonEmptyEnv("DB_SCHEMA", "public"),
		PaymentsServiceURL:        strings.TrimRight(getEnv("PAYMENTS_SERVICE_URL", ""), "/"),
		PaymentsIDTokenAudience:   strings.TrimRight(strings.TrimSpace(getEnv("PAYMENTS_ID_TOKEN_AUDIENCE", "")), "/"),
		JWTSecret:                 getEnv("JWT_SECRET", ""),
		JWTTTL:                    getDurationEnv("JWT_TTL", 72*time.Hour),
		GoogleClientID:            strings.TrimSpace(getEnv("GOOGLE_CLIENT_ID", "")),
		InternalWebhookSecret:     getEnv("INTERNAL_WEBHOOK_SECRET", ""),
		CORSAllowedOrigins:        splitCSV(getEnv("CORS_ALLOWED_ORIGINS", "http://localhost:3000,http://localhost:5173")),
		RateLimitPerMinute:        getIntEnv("RATE_LIMIT_PER_MINUTE", 120),
		OrderPendingTTL:           getDurationEnv("ORDER_PENDING_TTL", 15*time.Minute),
		ReleaseWorkerInterval:     getDurationEnv("RELEASE_WORKER_INTERVAL", time.Minute),
		PaymentsRequestTimeout:    getDurationEnv("PAYMENTS_REQUEST_TIMEOUT", 27*time.Second),
		DatabaseMaxConns:          getIntEnv("DB_MAX_CONNS", 20),
		DatabaseMinConns:          getIntEnv("DB_MIN_CONNS", 2),
		DatabaseMaxConnLifetime:   getDurationEnv("DB_MAX_CONN_LIFETIME", 30*time.Minute),
		DatabaseMaxConnIdleTime:   getDurationEnv("DB_MAX_CONN_IDLE_TIME", 5*time.Minute),
		ReleaseWorkerBatchSize:    getIntEnv("RELEASE_WORKER_BATCH_SIZE", 100),
		ReleaseWorkerMaxBatches:   getIntEnv("RELEASE_WORKER_MAX_BATCHES", 10),
		StorefrontURL:             strings.TrimRight(getEnv("STOREFRONT_URL", "http://localhost:5173"), "/"),
		SMTPHost:                  strings.TrimSpace(getEnv("SMTP_HOST", "")),
		SMTPPort:                  getIntEnv("SMTP_PORT", 587),
		SMTPUsername:              strings.TrimSpace(getEnv("SMTP_USERNAME", "")),
		SMTPPassword:              getEnv("SMTP_PASSWORD", ""),
		SMTPFrom:                  strings.TrimSpace(getEnv("SMTP_FROM", "")),
		SMTPTLSMode:               strings.ToLower(strings.TrimSpace(getEnv("SMTP_TLS_MODE", "starttls"))),
		EmailWorkerInterval:       getDurationEnv("EMAIL_WORKER_INTERVAL", 10*time.Second),
		EmailWorkerBatchSize:      getIntEnv("EMAIL_WORKER_BATCH_SIZE", 20),
		EmbeddedWorkers:           getBoolEnv("RUN_EMBEDDED_WORKERS", getEnv("APP_ENV", "development") != "production"),
		JobTimeout:                getDurationEnv("JOB_TIMEOUT", 5*time.Minute),
		EmailTasksEnabled:         getBoolEnv("EMAIL_TASKS_ENABLED", false),
		EmailTasksProject:         strings.TrimSpace(getEnv("EMAIL_TASKS_PROJECT", "")),
		EmailTasksLocation:        strings.TrimSpace(getEnv("EMAIL_TASKS_LOCATION", "")),
		EmailTasksQueue:           strings.TrimSpace(getEnv("EMAIL_TASKS_QUEUE", "")),
		EmailTasksWorkerURL:       strings.TrimRight(strings.TrimSpace(getEnv("EMAIL_TASKS_WORKER_URL", "")), "/"),
		EmailTasksServiceAccount:  strings.TrimSpace(getEnv("EMAIL_TASKS_SERVICE_ACCOUNT", "")),
		EmailTasksAudience:        strings.TrimRight(strings.TrimSpace(getEnv("EMAIL_TASKS_AUDIENCE", "")), "/"),
		EmailTasksDispatchTimeout: getDurationEnv("EMAIL_TASKS_DISPATCH_TIMEOUT", 2*time.Second),
	}
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func getNonEmptyEnv(key, fallback string) string {
	value := strings.TrimSpace(getEnv(key, ""))
	if value == "" {
		return fallback
	}
	return value
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

func getBoolEnv(key string, fallback bool) bool {
	raw := strings.TrimSpace(getEnv(key, ""))
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseBool(raw)
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
