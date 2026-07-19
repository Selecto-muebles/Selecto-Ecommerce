package email

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

type Execer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func Enqueue(ctx context.Context, db Execer, eventKey, recipient, template string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal email payload: %w", err)
	}
	_, err = db.Exec(ctx, `
		INSERT INTO email_outbox (event_key, recipient, template, payload)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (event_key) DO NOTHING`, eventKey, recipient, template, raw)
	if err != nil {
		return fmt.Errorf("enqueue email: %w", err)
	}
	return nil
}
