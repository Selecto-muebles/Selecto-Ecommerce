-- Operational catalogue controls, verified newsletter opt-out and Brevo delivery tracking.
-- Additive and safe to execute more than once.

ALTER TABLE shipments ADD COLUMN IF NOT EXISTS status_changed_at TIMESTAMPTZ;
UPDATE shipments SET status_changed_at=updated_at WHERE status_changed_at IS NULL;
ALTER TABLE shipments ALTER COLUMN status_changed_at SET DEFAULT NOW();
ALTER TABLE shipments ALTER COLUMN status_changed_at SET NOT NULL;

ALTER TABLE categories
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

ALTER TABLE product_reviews
    ADD COLUMN IF NOT EXISTS moderation_note TEXT NOT NULL DEFAULT '';

ALTER TABLE commerce.marketing_subscriptions
    ADD COLUMN IF NOT EXISTS sync_status TEXT NOT NULL DEFAULT 'pending',
    ADD COLUMN IF NOT EXISTS sync_version BIGINT NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS sync_attempts INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS sync_error TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS synced_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS last_provider_event_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS suppression_reason TEXT NOT NULL DEFAULT '';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname='marketing_subscriptions_sync_status_check'
    ) THEN
        ALTER TABLE commerce.marketing_subscriptions
            ADD CONSTRAINT marketing_subscriptions_sync_status_check
            CHECK (sync_status IN ('pending','syncing','synced','failed','suppressed'));
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS commerce.marketing_sync_outbox (
    id BIGSERIAL PRIMARY KEY,
    event_key TEXT NOT NULL UNIQUE,
    subscription_id BIGINT NOT NULL REFERENCES commerce.marketing_subscriptions(id) ON DELETE CASCADE,
    email TEXT NOT NULL,
    operation TEXT NOT NULL CHECK (operation IN ('subscribe','unsubscribe')),
    subscription_version BIGINT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending','processing','succeeded','failed','obsolete')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    locked_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_marketing_sync_outbox_due
    ON commerce.marketing_sync_outbox(next_attempt_at, id)
    WHERE status IN ('pending','processing');

INSERT INTO commerce.marketing_sync_outbox(event_key,subscription_id,email,operation,subscription_version)
SELECT 'marketing:'||id||':'||sync_version||':'||CASE WHEN status='subscribed' THEN 'subscribe' ELSE 'unsubscribe' END,
 id,email,CASE WHEN status='subscribed' THEN 'subscribe' ELSE 'unsubscribe' END,sync_version
FROM commerce.marketing_subscriptions WHERE sync_status='pending'
ON CONFLICT(event_key) DO NOTHING;

CREATE TABLE IF NOT EXISTS commerce.marketing_unsubscribe_tokens (
    id BIGSERIAL PRIMARY KEY,
    email TEXT NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_marketing_unsubscribe_tokens_active
    ON commerce.marketing_unsubscribe_tokens(lower(email), expires_at DESC)
    WHERE consumed_at IS NULL;

CREATE TABLE IF NOT EXISTS brevo_webhook_events (
    event_key TEXT PRIMARY KEY,
    event_type TEXT NOT NULL,
    event_scope TEXT NOT NULL CHECK (event_scope IN ('transactional','marketing')),
    email TEXT NOT NULL DEFAULT '',
    provider_message_id TEXT NOT NULL DEFAULT '',
    outbox_event_key TEXT NOT NULL DEFAULT '',
    occurred_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_brevo_webhook_events_created
    ON brevo_webhook_events(created_at DESC);

ALTER TABLE email_outbox
    ADD COLUMN IF NOT EXISTS delivery_status TEXT NOT NULL DEFAULT 'pending',
    ADD COLUMN IF NOT EXISTS provider_message_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS delivered_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS last_provider_event_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS bounce_reason TEXT NOT NULL DEFAULT '';

UPDATE email_outbox SET delivery_status='sent' WHERE status='sent' AND delivery_status='pending';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname='email_outbox_delivery_status_check'
    ) THEN
        ALTER TABLE email_outbox
            ADD CONSTRAINT email_outbox_delivery_status_check
            CHECK (delivery_status IN (
                'pending','sent','delivered','deferred','bounced','blocked','spam','invalid','error'
            ));
    END IF;
END $$;

ALTER TABLE email_outbox DROP CONSTRAINT IF EXISTS email_outbox_template_check;
ALTER TABLE email_outbox ADD CONSTRAINT email_outbox_template_check CHECK (template IN (
    'verify_email',
    'password_reset',
    'order_created',
    'payment_status',
    'shipment_status',
    'newsletter_unsubscribe'
));
