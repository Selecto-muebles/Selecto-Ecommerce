package database

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	migrationfiles "Selecto-Ecommerce/migrations"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const ecommerceMigrationLock int64 = 731402026

type MigrationResult struct {
	Applied int
	Skipped int
}

type migrationDefinition struct {
	Version  string
	Checksum string
	Content  string
}

func ApplyMigrations(ctx context.Context, pool *pgxpool.Pool) (MigrationResult, error) {
	migrations, err := migrationDefinitions()
	if err != nil {
		return MigrationResult{}, err
	}
	if err := ensureMigrationLedger(ctx, pool); err != nil {
		return MigrationResult{}, err
	}
	result := MigrationResult{}
	for _, migration := range migrations {
		applied, err := applyMigration(ctx, pool, migration)
		if err != nil {
			return result, err
		}
		if applied {
			result.Applied++
		} else {
			result.Skipped++
		}
	}
	return result, nil
}

func ensureMigrationLedger(ctx context.Context, pool *pgxpool.Pool) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin migration ledger transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", ecommerceMigrationLock); err != nil {
		return fmt.Errorf("lock migration ledger: %w", err)
	}
	if _, err := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		checksum TEXT NOT NULL,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`); err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration ledger: %w", err)
	}
	return nil
}

func applyMigration(ctx context.Context, pool *pgxpool.Pool, migration migrationDefinition) (bool, error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("begin migration %s: %w", migration.Version, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", ecommerceMigrationLock); err != nil {
		return false, fmt.Errorf("lock migration %s: %w", migration.Version, err)
	}
	var appliedChecksum string
	err = tx.QueryRow(ctx, "SELECT checksum FROM schema_migrations WHERE version=$1", migration.Version).Scan(&appliedChecksum)
	switch {
	case err == nil && appliedChecksum != migration.Checksum:
		return false, fmt.Errorf("migration %s checksum mismatch", migration.Version)
	case err == nil:
		if err := tx.Commit(ctx); err != nil {
			return false, fmt.Errorf("commit skipped migration %s: %w", migration.Version, err)
		}
		return false, nil
	case err != pgx.ErrNoRows:
		return false, fmt.Errorf("read migration %s: %w", migration.Version, err)
	}
	content, err := executableMigrationContent(ctx, tx, migration)
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, content); err != nil {
		return false, fmt.Errorf("apply migration %s: %w", migration.Version, err)
	}
	if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations(version, checksum) VALUES ($1, $2)", migration.Version, migration.Checksum); err != nil {
		return false, fmt.Errorf("record migration %s: %w", migration.Version, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit migration %s: %w", migration.Version, err)
	}
	return true, nil
}

func executableMigrationContent(ctx context.Context, tx pgx.Tx, migration migrationDefinition) (string, error) {
	var currentSchema string
	if err := tx.QueryRow(ctx, "SELECT current_schema()").Scan(&currentSchema); err != nil {
		return "", fmt.Errorf("resolve schema for migration %s: %w", migration.Version, err)
	}
	if currentSchema != "commerce" {
		return migration.Content, nil
	}
	switch migration.Version {
	case "010_marketing_subscriptions.sql":
		const marker = "CREATE TABLE IF NOT EXISTS commerce.marketing_subscriptions"
		start := strings.Index(migration.Content, marker)
		if start < 0 {
			return "", fmt.Errorf("migration %s is missing its table definition", migration.Version)
		}
		return migration.Content[start:], nil
	case "011_marketing_subscriptions_schema.sql":
		return `DO $$
		BEGIN
			IF to_regclass('commerce.marketing_subscriptions') IS NULL THEN
				RAISE EXCEPTION 'commerce.marketing_subscriptions does not exist';
			END IF;
		END $$;`, nil
	default:
		return migration.Content, nil
	}
}

func migrationDefinitions() ([]migrationDefinition, error) {
	entries, err := migrationfiles.Files.ReadDir(".")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}
	definitions := make([]migrationDefinition, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		content, err := migrationfiles.Files.ReadFile(entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		digest := sha256.Sum256(content)
		definitions = append(definitions, migrationDefinition{
			Version: entry.Name(), Checksum: hex.EncodeToString(digest[:]), Content: string(content),
		})
	}
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].Version < definitions[j].Version })
	if len(definitions) == 0 {
		return nil, fmt.Errorf("no embedded migrations found")
	}
	return definitions, nil
}
