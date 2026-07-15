package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"Selecto-Ecommerce/internal/config"
	httpDelivery "Selecto-Ecommerce/internal/delivery/http"
	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/logging"
)

var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		runHealthcheck()
		return
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg := config.LoadConfig()
	if err := cfg.Validate(); err != nil {
		logger.Error(logging.EventApplicationConfigurationInvalid, "error", err)
		os.Exit(1)
	}

	db, err := database.NewPostgresPool(cfg.DatabaseURL, database.PoolConfig{
		MaxConns:        int32(cfg.DatabaseMaxConns),
		MinConns:        int32(cfg.DatabaseMinConns),
		MaxConnLifetime: cfg.DatabaseMaxConnLifetime,
		MaxConnIdleTime: cfg.DatabaseMaxConnIdleTime,
	}, logger)
	if err != nil {
		os.Exit(1)
	}
	defer db.Pool.Close()

	appCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	httpDelivery.StartExpiredOrderWorker(appCtx, db, cfg, logger)
	router := httpDelivery.SetupRouter(db, cfg, logger)

	logger.Info(logging.EventServerStarting, "port", cfg.Port, "environment", cfg.AppEnv, "version", version, "commit", commit)
	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- server.ListenAndServe() }()
	select {
	case <-appCtx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error(logging.EventServerFailed, "error", err)
		}
	case err := <-serverErrors:
		if err != nil && err != http.ErrServerClosed {
			logger.Error(logging.EventServerFailed, "error", err)
			os.Exit(1)
		}
	}
}

func runHealthcheck() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get(fmt.Sprintf("http://127.0.0.1:%s/ready", port))
	if err != nil || response.StatusCode != http.StatusOK {
		os.Exit(1)
	}
	_ = response.Body.Close()
}
