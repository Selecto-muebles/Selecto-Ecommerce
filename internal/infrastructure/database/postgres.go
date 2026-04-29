package database

import (
	"context"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
)

type DB struct {
	Pool *pgxpool.Pool
}

func NewPostgresPool(databaseURL string) *DB {
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		log.Fatalf("Unable to connect to database: %v", err)
	}

	err = pool.Ping(context.Background())
	if err != nil {
		log.Fatalf("Database not responding: %v", err)
	}

	log.Println("✅ Connected to PostgreSQL")

	return &DB{
		Pool: pool,
	}
}