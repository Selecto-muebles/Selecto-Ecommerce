package email

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"Selecto-Ecommerce/internal/infrastructure/database"

	"github.com/jackc/pgx/v5"
)

type Worker struct {
	db        *database.DB
	mailer    Mailer
	logger    *slog.Logger
	interval  time.Duration
	batchSize int
}

type queuedEmail struct {
	ID        int64
	Recipient string
	Template  string
	Payload   json.RawMessage
}

func NewWorker(db *database.DB, mailer Mailer, logger *slog.Logger, interval time.Duration, batchSize int) *Worker {
	return &Worker{db: db, mailer: mailer, logger: logger, interval: interval, batchSize: batchSize}
}

func (w *Worker) Start(ctx context.Context) {
	go func() {
		w.process(ctx)
		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.process(ctx)
			}
		}
	}()
}

func (w *Worker) process(ctx context.Context) {
	for i := 0; i < w.batchSize; i++ {
		current, ok, err := w.claim(ctx)
		if err != nil {
			w.logger.Error("email_outbox_claim_failed", "error", err)
			return
		}
		if !ok {
			return
		}
		subject, body, err := Render(current.Template, current.Payload)
		if err == nil {
			err = w.mailer.Send(ctx, current.Recipient, subject, body)
		}
		if err != nil {
			w.fail(ctx, current.ID, err)
			continue
		}
		if _, err := w.db.Pool.Exec(ctx, `UPDATE email_outbox SET status='sent', sent_at=NOW(), locked_at=NULL, last_error='', payload='{}'::jsonb, updated_at=NOW() WHERE id=$1`, current.ID); err != nil {
			w.logger.Error("email_outbox_complete_failed", "email_id", current.ID, "error", err)
			continue
		}
		w.logger.Info("transactional_email_sent", "email_id", current.ID, "template", current.Template)
	}
}

func (w *Worker) claim(ctx context.Context) (queuedEmail, bool, error) {
	var current queuedEmail
	err := w.db.Pool.QueryRow(ctx, `
		WITH candidate AS (
			SELECT id FROM email_outbox
			WHERE (status='pending' AND next_attempt_at <= NOW())
			   OR (status='processing' AND locked_at < NOW() - interval '5 minutes')
			ORDER BY id
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE email_outbox e
		SET status='processing', locked_at=NOW(), attempts=e.attempts+1, updated_at=NOW()
		FROM candidate
		WHERE e.id=candidate.id
		RETURNING e.id, e.recipient, e.template, e.payload`,
	).Scan(&current.ID, &current.Recipient, &current.Template, &current.Payload)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return current, false, nil
		}
		return current, false, err
	}
	return current, true, nil
}

func (w *Worker) fail(ctx context.Context, id int64, sendErr error) {
	if _, err := w.db.Pool.Exec(ctx, `
		UPDATE email_outbox
		SET status=CASE WHEN attempts >= 5 THEN 'failed' ELSE 'pending' END,
		    next_attempt_at=NOW() + make_interval(secs => LEAST(300, (POWER(2, attempts)::INTEGER * 5))),
		    locked_at=NULL,
		    last_error=LEFT($2, 1000),
		    updated_at=NOW()
		WHERE id=$1`, id, sendErr.Error()); err != nil {
		w.logger.Error("email_outbox_retry_failed", "email_id", id, "error", err)
		return
	}
	w.logger.Warn("transactional_email_failed", "email_id", id, "error", sendErr)
}
