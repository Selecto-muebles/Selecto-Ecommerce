package jobs

import (
	"context"
	"log/slog"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/infrastructure/database"
	mailinfra "Selecto-Ecommerce/internal/infrastructure/email"
	postgresrepo "Selecto-Ecommerce/internal/repository/postgres"
	orderservice "Selecto-Ecommerce/internal/service/orders"
	"Selecto-Ecommerce/internal/shared/logging"
)

const (
	ExpireOrders = "expire-orders"
	EmailOutbox  = "email-outbox"
)

func Run(ctx context.Context, name string, db *database.DB, cfg *config.Config, logger *slog.Logger) error {
	switch name {
	case ExpireOrders:
		_, err := runExpiredOrders(ctx, db, cfg, logger)
		return err
	case EmailOutbox:
		_, err := runEmailOutbox(ctx, db, cfg, logger)
		return err
	default:
		return configError(name)
	}
}

func runExpiredOrders(ctx context.Context, db *database.DB, cfg *config.Config, logger *slog.Logger) (int64, error) {
	expirer := orderservice.NewExpirer(postgresrepo.NewOrderRepository(db, nil))
	var total int64
	for batch := 0; batch < cfg.ReleaseWorkerMaxBatches; batch++ {
		released, err := expirer.Release(ctx, cfg.OrderPendingTTL, cfg.ReleaseWorkerBatchSize, true)
		if err != nil {
			return total, err
		}
		total += released
		if released < int64(cfg.ReleaseWorkerBatchSize) {
			break
		}
	}
	logger.Info(logging.EventExpiredOrderReleaseCompleted, "orders_released", total)
	return total, nil
}

func runEmailOutbox(ctx context.Context, db *database.DB, cfg *config.Config, logger *slog.Logger) (int, error) {
	worker := mailinfra.NewWorker(db, mailinfra.NewSMTPMailer(mailinfra.SMTPConfig{
		Host: cfg.SMTPHost, Port: cfg.SMTPPort, Username: cfg.SMTPUsername,
		Password: cfg.SMTPPassword, From: cfg.SMTPFrom, TLSMode: cfg.SMTPTLSMode,
	}), logger, 0, cfg.EmailWorkerBatchSize)
	processed, err := worker.ProcessBatch(ctx)
	logger.Info("email_outbox_job_completed", "emails_processed", processed, "failed", err != nil)
	return processed, err
}

type unknownJobError string

func (err unknownJobError) Error() string { return "unknown job: " + string(err) }

func configError(name string) error { return unknownJobError(name) }
