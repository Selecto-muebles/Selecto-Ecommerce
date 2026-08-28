ALTER TABLE products
    ADD COLUMN IF NOT EXISTS active BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN IF NOT EXISTS sku TEXT,
    ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS paid_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS cancelled_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS payment_status TEXT,
    ADD COLUMN IF NOT EXISTS payment_id BIGINT,
    ADD COLUMN IF NOT EXISTS active_payment_preference_id TEXT,
    ADD COLUMN IF NOT EXISTS active_checkout_url TEXT,
    ADD COLUMN IF NOT EXISTS active_payment_environment TEXT;

UPDATE orders
SET expires_at = created_at + interval '15 minutes'
WHERE expires_at IS NULL AND status = 'pending';

UPDATE orders
SET payment_status = status
WHERE payment_status IS NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'orders_payment_status_check'
    ) THEN
        ALTER TABLE orders
            ADD CONSTRAINT orders_payment_status_check
            CHECK (payment_status IS NULL OR payment_status IN ('pending', 'paid', 'failed', 'cancelled'));
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS payment_webhook_events (
    id BIGSERIAL PRIMARY KEY,
    event_key TEXT NOT NULL UNIQUE,
    payment_id BIGINT NOT NULL,
    order_id INTEGER NOT NULL REFERENCES orders(id),
    status TEXT NOT NULL CHECK (status IN ('paid', 'failed', 'cancelled')),
    amount_cents BIGINT,
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at TIMESTAMPTZ,
    result TEXT
);

CREATE TABLE IF NOT EXISTS audit_logs (
    id BIGSERIAL PRIMARY KEY,
    actor_email TEXT NOT NULL,
    action TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    entity_id BIGINT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_orders_active_payment_preference
    ON orders(active_payment_preference_id)
    WHERE active_payment_preference_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_orders_status_expires_at
    ON orders(status, expires_at);

CREATE INDEX IF NOT EXISTS idx_orders_user_created_at
    ON orders(user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_orders_payment_id
    ON orders(payment_id)
    WHERE payment_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_products_active_created_at
    ON products(active, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_payment_webhook_events_payment_id
    ON payment_webhook_events(payment_id);

CREATE INDEX IF NOT EXISTS idx_payment_webhook_events_order_id
    ON payment_webhook_events(order_id);

CREATE INDEX IF NOT EXISTS idx_audit_logs_entity_created_at
    ON audit_logs(entity_type, entity_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_audit_logs_actor_created_at
    ON audit_logs(actor_email, created_at DESC);
