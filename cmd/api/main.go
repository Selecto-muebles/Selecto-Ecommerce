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
	mailinfra "Selecto-Ecommerce/internal/infrastructure/email"
	"Selecto-Ecommerce/internal/jobs"
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
	emailTaskWorker := isEmailTaskWorkerCommand(os.Args[1:])
	jobName, jobMode, err := parseCommand(os.Args[1:])
	if err != nil {
		logger.Error(logging.EventApplicationConfigurationInvalid, "error", err)
		os.Exit(2)
	}
	if emailTaskWorker {
		err = cfg.ValidateEmailTaskWorker()
	} else if jobMode {
		err = cfg.ValidateJob(jobName)
	} else {
		err = cfg.Validate()
	}
	if err != nil {
		logger.Error(logging.EventApplicationConfigurationInvalid, "error", err)
		os.Exit(1)
	}

	db, err := database.NewPostgresPool(cfg.DatabaseURL, database.PoolConfig{
		MaxConns:        int32(cfg.DatabaseMaxConns),
		MinConns:        int32(cfg.DatabaseMinConns),
		MaxConnLifetime: cfg.DatabaseMaxConnLifetime,
		MaxConnIdleTime: cfg.DatabaseMaxConnIdleTime,
		ExpectedSchema:  cfg.DatabaseSchema,
	}, logger)
	if err != nil {
		os.Exit(1)
	}
	defer db.Pool.Close()

	appCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if jobMode {
		jobCtx, cancel := context.WithTimeout(appCtx, cfg.JobTimeout)
		defer cancel()
		if err := jobs.Run(jobCtx, jobName, db, cfg, logger); err != nil {
			logger.Error("serverless_job_failed", "job", jobName, "error", err)
			db.Pool.Close()
			os.Exit(1)
		}
		logger.Info("serverless_job_completed", "job", jobName)
		return
	}
	if emailTaskWorker {
		worker := newEmailWorker(db, cfg, logger)
		serveHTTP(appCtx, cfg, logger, httpDelivery.SetupEmailTaskRouter(db, cfg, logger, worker))
		return
	}
	if cfg.EmbeddedWorkers {
		httpDelivery.StartExpiredOrderWorker(appCtx, db, cfg, logger)
	}
	if cfg.EmbeddedWorkers && cfg.SMTPHost != "" && cfg.SMTPFrom != "" {
		mailinfra.NewWorker(db, mailinfra.NewSMTPMailer(mailinfra.SMTPConfig{
			Host: cfg.SMTPHost, Port: cfg.SMTPPort, Username: cfg.SMTPUsername,
			Password: cfg.SMTPPassword, From: cfg.SMTPFrom, TLSMode: cfg.SMTPTLSMode,
		}), logger, cfg.EmailWorkerInterval, cfg.EmailWorkerBatchSize).Start(appCtx)
	} else if cfg.EmbeddedWorkers {
		logger.Warn("transactional_email_disabled", "reason", "SMTP is not configured")
	}
	notifiers, closeNotifiers, err := newEmailNotifiers(appCtx, cfg, logger)
	if err != nil {
		logger.Error(logging.EventApplicationConfigurationInvalid, "error", err)
		os.Exit(1)
	}
	defer closeNotifiers()
	serveHTTP(appCtx, cfg, logger, httpDelivery.SetupRouter(db, cfg, logger, notifiers...))
}

func parseCommand(args []string) (string, bool, error) {
	if isEmailTaskWorkerCommand(args) {
		return "", false, nil
	}
	if len(args) == 0 {
		return "", false, nil
	}
	if len(args) == 2 && args[0] == "job" && (args[1] == jobs.ExpireOrders || args[1] == jobs.EmailOutbox) {
		return args[1], true, nil
	}
	return "", false, fmt.Errorf("usage: selecto-ecommerce [job expire-orders|job email-outbox|serve email-outbox]")
}

func isEmailTaskWorkerCommand(args []string) bool {
	return len(args) == 2 && args[0] == "serve" && args[1] == "email-outbox"
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
