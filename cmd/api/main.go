package main

import (
	"log"
	"log/slog"
	"os"

	"Selecto-Ecommerce/internal/config"
	httpDelivery "Selecto-Ecommerce/internal/delivery/http"
	"Selecto-Ecommerce/internal/infrastructure/database"
)

func main() {
	cfg := config.LoadConfig()
	if err := cfg.Validate(); err != nil {
		log.Fatalf("invalid configuration: %v", err)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	db := database.NewPostgresPool(cfg.DatabaseURL)
	defer db.Pool.Close()

	httpDelivery.StartExpiredOrderWorker(db, cfg, logger)
	router := httpDelivery.SetupRouter(db, cfg, logger)

	logger.Info("server_starting", "port", cfg.Port, "env", cfg.AppEnv)
	if err := router.Run(":" + cfg.Port); err != nil {
		logger.Error("server_failed", "error", err)
		os.Exit(1)
	}
}
