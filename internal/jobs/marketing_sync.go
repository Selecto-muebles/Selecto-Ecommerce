package jobs

import (
	"context"
	"log/slog"
	"time"

	"Selecto-Ecommerce/internal/config"
	brevoinfra "Selecto-Ecommerce/internal/infrastructure/brevo"
	"Selecto-Ecommerce/internal/infrastructure/database"
)

func runMarketingSync(ctx context.Context, db *database.DB, cfg *config.Config, logger *slog.Logger) (int, error) {
	if !cfg.BrevoContactsEnabled {
		return 0, nil
	}
	client := brevoinfra.NewClient(brevoinfra.ClientConfig{
		APIKey: cfg.BrevoAPIKey, ListID: cfg.BrevoListID,
		BaseURL: cfg.BrevoAPIBaseURL, Timeout: 15 * time.Second,
	})
	worker := brevoinfra.NewWorker(db, client, logger, cfg.MarketingWorkerBatchSize)
	processed, err := worker.ProcessBatch(ctx)
	logger.Info("marketing_sync_job_completed", "contacts_processed", processed, "failed", err != nil)
	return processed, err
}
