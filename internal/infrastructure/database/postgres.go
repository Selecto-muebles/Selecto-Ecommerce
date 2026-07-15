package database

import (
	"context"
	"log/slog"
	"time"

	"Selecto-Ecommerce/internal/shared/logging"

	"github.com/jackc/pgx/v5/pgxpool"
)

type DB struct {
	Pool *pgxpool.Pool
}

type PoolConfig struct {
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
}

func NewPostgresPool(databaseURL string, cfg PoolConfig, logger *slog.Logger) (*DB, error) {
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		logger.Error(logging.EventDatabaseConnectionFailed, "error", err)
		return nil, err
	}
	poolConfig.MaxConns = cfg.MaxConns
	poolConfig.MinConns = cfg.MinConns
	poolConfig.MaxConnLifetime = cfg.MaxConnLifetime
	poolConfig.MaxConnIdleTime = cfg.MaxConnIdleTime

	pool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
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

	logger.Info(logging.EventDatabaseConnected, "max_connections", cfg.MaxConns, "min_connections", cfg.MinConns)

	return &DB{
		Pool: pool,
	}, nil
}
