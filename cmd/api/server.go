package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/shared/logging"
)

func serveHTTP(ctx context.Context, cfg *config.Config, logger *slog.Logger, handler http.Handler) {
	logger.Info(logging.EventServerStarting, "port", cfg.Port, "environment", cfg.AppEnv, "version", version, "commit", commit)
	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- server.ListenAndServe() }()
	select {
	case <-ctx.Done():
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