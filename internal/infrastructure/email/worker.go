package email

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"Selecto-Ecommerce/internal/infrastructure/database"
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
		w.processAndLog(ctx)
		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.processAndLog(ctx)
			}
		}
	}()
}

func (w *Worker) processAndLog(ctx context.Context) {
	if _, err := w.ProcessBatch(ctx); err != nil {
		w.logger.Error("email_outbox_batch_failed", "error", err)
	}
}

func (w *Worker) ProcessBatch(ctx context.Context) (int, error) {
	emails, err := w.claimBatch(ctx, w.batchSize)
	if err != nil {
		return 0, err
	}
	if len(emails) == 0 {
		return 0, nil
	}

	recipients := make([]string, len(emails))
	subjects := make([]string, len(emails))
	bodies := make([]string, len(emails))
	renderErrors := make([]error, len(emails))

	for i, e := range emails {
		subject, body, err := Render(e.Template, e.Payload)
		if err != nil {
			renderErrors[i] = err
		} else {
			recipients[i] = e.Recipient
			subjects[i] = subject
			bodies[i] = body
		}
	}

	// Filter messages that rendered successfully to send to Mailer
	var sendIndices []int
	var sendRecipients []string
	var sendSubjects []string
	var sendBodies []string

	for i, err := range renderErrors {
		if err == nil {
			sendIndices = append(sendIndices, i)
			sendRecipients = append(sendRecipients, recipients[i])
			sendSubjects = append(sendSubjects, subjects[i])
			sendBodies = append(sendBodies, bodies[i])
		}
	}

	var sendErrors []error
	if len(sendRecipients) > 0 {
		sendErrors = w.mailer.SendBatch(ctx, sendRecipients, sendSubjects, sendBodies)
	}

	var firstError error
	processed := 0

	for i, e := range emails {
		var emailErr error
		if renderErrors[i] != nil {
			emailErr = renderErrors[i]
		} else {
			// Find the corresponding error in sendErrors
			for idx, origIdx := range sendIndices {
				if origIdx == i {
					emailErr = sendErrors[idx]
					break
				}
			}
		}

		if emailErr != nil {
			w.fail(ctx, e.ID, emailErr)
			if firstError == nil {
				firstError = emailErr
			}
			continue
		}

		if _, err := w.db.Pool.Exec(ctx, `UPDATE email_outbox SET status='sent', sent_at=NOW(), locked_at=NULL, last_error='', payload='{}'::jsonb, updated_at=NOW() WHERE id=$1`, e.ID); err != nil {
			if firstError == nil {
				firstError = err
			}
			continue
		}
		processed++
		w.logger.Info("transactional_email_sent", "email_id", e.ID, "template", e.Template)
	}

	return processed, firstError
}

func (w *Worker) claimBatch(ctx context.Context, limit int) ([]queuedEmail, error) {
	rows, err := w.db.Pool.Query(ctx, `
		WITH candidate AS (
			SELECT id FROM email_outbox
			WHERE (status='pending' AND next_attempt_at <= NOW())
			   OR (status='processing' AND locked_at < NOW() - interval '5 minutes')
			ORDER BY id
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE email_outbox e
		SET status='processing', locked_at=NOW(), attempts=e.attempts+1, updated_at=NOW()
		FROM candidate
		WHERE e.id=candidate.id
		RETURNING e.id, e.recipient, e.template, e.payload`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var emails []queuedEmail
	for rows.Next() {
		var e queuedEmail
		if err := rows.Scan(&e.ID, &e.Recipient, &e.Template, &e.Payload); err != nil {
			return nil, err
		}
		emails = append(emails, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return emails, nil
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
