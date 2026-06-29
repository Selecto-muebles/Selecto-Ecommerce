package config

import (
	"errors"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Port                  string
	AppEnv                string
	DatabaseURL           string
	PaymentsServiceURL    string
	JWTSecret             string
	JWTTTL                time.Duration
	InternalWebhookSecret string
	CORSAllowedOrigins    []string
	RateLimitPerMinute    int
	OrderPendingTTL       time.Duration
	ReleaseWorkerInterval time.Duration
}

func LoadConfig() *Config {
	err := godotenv.Load()
	if err != nil {
		log.Println("No .env file found")
	}

	return &Config{
		Port:                  getEnv("PORT", "8080"),
		AppEnv:                getEnv("APP_ENV", "development"),
		DatabaseURL:           getEnv("DATABASE_URL", ""),
		PaymentsServiceURL:    strings.TrimRight(getEnv("PAYMENTS_SERVICE_URL", ""), "/"),
		JWTSecret:             getEnv("JWT_SECRET", ""),
		JWTTTL:                getDurationEnv("JWT_TTL", 72*time.Hour),
		InternalWebhookSecret: getEnv("INTERNAL_WEBHOOK_SECRET", ""),
		CORSAllowedOrigins:    splitCSV(getEnv("CORS_ALLOWED_ORIGINS", "http://localhost:3000,http://localhost:5173")),
		RateLimitPerMinute:    getIntEnv("RATE_LIMIT_PER_MINUTE", 120),
		OrderPendingTTL:       getDurationEnv("ORDER_PENDING_TTL", 15*time.Minute),
		ReleaseWorkerInterval: getDurationEnv("RELEASE_WORKER_INTERVAL", time.Minute),
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
	if c.RateLimitPerMinute <= 0 {
		return errors.New("RATE_LIMIT_PER_MINUTE must be positive")
	}
	if c.AppEnv == "production" {
		if c.PaymentsServiceURL == "" {
			return errors.New("PAYMENTS_SERVICE_URL is required in production")
		}
		if c.InternalWebhookSecret == "" {
			return errors.New("INTERNAL_WEBHOOK_SECRET is required in production")
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
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func allowsAllOrigins(origins []string) bool {
	for _, origin := range origins {
		if origin == "*" {
			return true
		}
	}
	return false
}
