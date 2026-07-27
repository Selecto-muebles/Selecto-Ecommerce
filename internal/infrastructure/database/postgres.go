package database

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
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
	ExpectedSchema  string
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

	expectedSchema := strings.TrimSpace(cfg.ExpectedSchema)
	if expectedSchema == "" {
		expectedSchema = "public"
	}
	var currentSchema string
	if err := pool.QueryRow(context.Background(), "SELECT current_schema()").Scan(&currentSchema); err != nil {
		pool.Close()
		logger.Error(logging.EventDatabaseHealthCheckFailed, "error", err)
		return nil, fmt.Errorf("resolve current database schema: %w", err)
	}
	if currentSchema != expectedSchema {
		pool.Close()
		err := fmt.Errorf("unexpected database schema %q, expected %q", currentSchema, expectedSchema)
		logger.Error(logging.EventDatabaseConnectionFailed, "error", err)
		return nil, err
	}

	logger.Info(
		logging.EventDatabaseConnected,
		"schema", currentSchema,
		"max_connections", cfg.MaxConns,
		"min_connections", cfg.MinConns,
	)

	return &DB{
		Pool: pool,
	}, nil
}
