package email

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ErrEmailNotReady is returned by ProcessOne when the email cannot be
// claimed for processing (e.g. it was already sent or is not yet due).
var ErrEmailNotReady = errors.New("email is not ready for processing")

// ProcessOne claims and sends a single email from the outbox identified by id.
// It is designed for use with serverless task runners (e.g. GCP Cloud Tasks)
// that dispatch one task per outbox entry.
func (w *Worker) ProcessOne(ctx context.Context, id int64) error {
	email, claimed, err := w.claimOne(ctx, id)
	if err != nil {
		return err
	}
	if !claimed {
		return w.resolveUnclaimed(ctx, id)
	}

	subject, body, err := Render(email.Template, email.Payload)
	if err == nil {
		errorsByRecipient := w.mailer.SendBatch(
			ctx,
			[]string{email.Recipient},
			[]string{subject},
			[]string{body},
		)
		if len(errorsByRecipient) != 1 {
			err = errors.New("mailer returned an invalid result count")
		} else {
			err = errorsByRecipient[0]
		}
	}
	if err != nil {
		w.fail(ctx, email.ID, err)
		return err
	}

	command, err := w.db.Pool.Exec(ctx, `
		UPDATE email_outbox
		SET status='sent', sent_at=NOW(), locked_at=NULL, last_error='',
		    payload='{}'::jsonb, updated_at=NOW()
		WHERE id=$1 AND status='processing'`, email.ID)
	if err != nil {
		return fmt.Errorf("mark email sent: %w", err)
	}
	if command.RowsAffected() != 1 {
		return ErrEmailNotReady
	}
	w.logger.Info("transactional_email_sent", "email_id", email.ID, "template", email.Template)
	return nil
}

func (w *Worker) claimOne(ctx context.Context, id int64) (queuedEmail, bool, error) {
	var email queuedEmail
	err := w.db.Pool.QueryRow(ctx, `
		WITH candidate AS (
			SELECT id FROM email_outbox
			WHERE id=$1
			  AND ((status='pending' AND next_attempt_at <= NOW())
			    OR (status='processing' AND locked_at < NOW() - interval '5 minutes'))
			FOR UPDATE SKIP LOCKED
		)
		UPDATE email_outbox e
		SET status='processing', locked_at=NOW(), attempts=e.attempts+1, updated_at=NOW()
		FROM candidate
		WHERE e.id=candidate.id
		RETURNING e.id, e.recipient, e.template, e.payload`, id).Scan(
		&email.ID,
		&email.Recipient,
		&email.Template,
		&email.Payload,
	)
	if err == nil {
		return email, true, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return queuedEmail{}, false, nil
	}
	return queuedEmail{}, false, fmt.Errorf("claim email: %w", err)
}

func (w *Worker) resolveUnclaimed(ctx context.Context, id int64) error {
	var status string
	err := w.db.Pool.QueryRow(ctx, `SELECT status FROM email_outbox WHERE id=$1`, id).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read email status: %w", err)
	}
	if status == "sent" || status == "failed" {
		return nil
	}
	return ErrEmailNotReady
}