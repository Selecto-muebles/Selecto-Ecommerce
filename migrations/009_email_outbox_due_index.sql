DROP INDEX IF EXISTS idx_email_outbox_due;

CREATE INDEX idx_email_outbox_due
    ON email_outbox(next_attempt_at, id)
    WHERE status IN ('pending', 'processing');
