package database

import (
	"context"
	"log/slog"

	"Selecto-Ecommerce/internal/shared/logging"

	"github.com/jackc/pgx/v5/pgxpool"
)

type DB struct {
	Pool *pgxpool.Pool
}

func NewPostgresPool(databaseURL string, logger *slog.Logger) (*DB, error) {
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		logger.Error(logging.EventDatabaseConnectionFailed, "error", err)
		return nil, err
	}

	err = pool.Ping(context.Background())
	if err != nil {
		pool.Close()
		logger.Error(logging.EventDatabaseHealthCheckFailed, "error", err)
		return nil, err
	}

	logger.Info(logging.EventDatabaseConnected)

	return &DB{
		Pool: pool,
	}, nil
}
