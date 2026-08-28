package main

import (
	"context"
	"fmt"
	"log/slog"

	"Selecto-Ecommerce/internal/config"
	"Selecto-Ecommerce/internal/infrastructure/database"
	mailinfra "Selecto-Ecommerce/internal/infrastructure/email"
)

func newEmailWorker(db *database.DB, cfg *config.Config, logger *slog.Logger) *mailinfra.Worker {
	mailer := mailinfra.NewSMTPMailer(mailinfra.SMTPConfig{
		Host: cfg.SMTPHost, Port: cfg.SMTPPort, Username: cfg.SMTPUsername,
		Password: cfg.SMTPPassword, From: cfg.SMTPFrom, TLSMode: cfg.SMTPTLSMode,
	})
	return mailinfra.NewWorker(db, mailer, logger, cfg.EmailWorkerInterval, cfg.EmailWorkerBatchSize)
}

func newEmailNotifiers(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
) ([]mailinfra.DispatchNotifier, func(), error) {
	if !cfg.EmailTasksEnabled {
		return nil, func() {}, nil
	}
	dispatcher, err := mailinfra.NewTaskDispatcher(ctx, mailinfra.TaskDispatcherConfig{
		Project: cfg.EmailTasksProject, Location: cfg.EmailTasksLocation,
		Queue: cfg.EmailTasksQueue, WorkerURL: cfg.EmailTasksWorkerURL,
		ServiceAccount: cfg.EmailTasksServiceAccount, Audience: cfg.EmailTasksAudience,
		Timeout: cfg.EmailTasksDispatchTimeout,
	}, logger)
	if err != nil {
		return nil, func() {}, fmt.Errorf("initialize email task dispatcher: %w", err)
	}
	return []mailinfra.DispatchNotifier{dispatcher}, func() {
		if err := dispatcher.Close(); err != nil {
			logger.Warn("email_task_dispatcher_close_failed", "error", err)
		}
	}, nil
}
