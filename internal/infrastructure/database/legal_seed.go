package database

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	migrationfiles "Selecto-Ecommerce/migrations"

	"github.com/jackc/pgx/v5/pgxpool"
)

func seedSelectoTerms(ctx context.Context, pool *pgxpool.Pool) error {
	body, err := migrationfiles.Files.ReadFile("selecto-terms-v1.md")
	if err != nil {
		return fmt.Errorf("read embedded Selecto terms: %w", err)
	}
	const version = "1.0"
	digest := sha256.Sum256(body)
	_, err = pool.Exec(ctx, `
		INSERT INTO legal_documents(slug,version,title,body_markdown,body_sha256,published,effective_at)
		VALUES('terms', $1, 'Términos y Condiciones Generales y Políticas Comerciales', $2, $3, TRUE, '2026-10-12T00:00:00Z')
		ON CONFLICT(slug,version) DO NOTHING`, version, string(body), hex.EncodeToString(digest[:]))
	if err != nil {
		return fmt.Errorf("seed Selecto terms version %s: %w", version, err)
	}
	return nil
}
