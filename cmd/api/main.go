package main

import (
	"log/slog"
	"os"

	"Selecto-Ecommerce/internal/config"
	httpDelivery "Selecto-Ecommerce/internal/delivery/http"
	"Selecto-Ecommerce/internal/infrastructure/database"
	"Selecto-Ecommerce/internal/shared/logging"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg := config.LoadConfig()
	if err := cfg.Validate(); err != nil {
		logger.Error(logging.EventApplicationConfigurationInvalid, "error", err)
		os.Exit(1)
	}

	db, err := database.NewPostgresPool(cfg.DatabaseURL, logger)
	if err != nil {
		os.Exit(1)
	}
	defer db.Pool.Close()

	httpDelivery.StartExpiredOrderWorker(db, cfg, logger)
	router := httpDelivery.SetupRouter(db, cfg, logger)

	logger.Info(logging.EventServerStarting, "port", cfg.Port, "environment", cfg.AppEnv)
	if err := router.Run(":" + cfg.Port); err != nil {
		logger.Error(logging.EventServerFailed, "error", err)
		os.Exit(1)
	}
}
