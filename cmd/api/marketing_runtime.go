package main

import (
	"log/slog"
	"time"

	"Selecto-Ecommerce/internal/config"
	brevoinfra "Selecto-Ecommerce/internal/infrastructure/brevo"
	"Selecto-Ecommerce/internal/infrastructure/database"
)

func newMarketingWorker(db *database.DB, cfg *config.Config, logger *slog.Logger) *brevoinfra.Worker {
	if !cfg.BrevoContactsEnabled {
		return nil
	}
	client := brevoinfra.NewClient(brevoinfra.ClientConfig{
		APIKey: cfg.BrevoAPIKey, ListID: cfg.BrevoListID,
		BaseURL: cfg.BrevoAPIBaseURL, Timeout: 15 * time.Second,
	})
	return brevoinfra.NewWorker(db, client, logger, cfg.MarketingWorkerBatchSize)
}
