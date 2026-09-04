ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS payment_provider TEXT,
    ADD COLUMN IF NOT EXISTS provider_payment_id TEXT;

ALTER TABLE payment_webhook_events
    ADD COLUMN IF NOT EXISTS payment_provider TEXT,
    ADD COLUMN IF NOT EXISTS provider_payment_id TEXT;

ALTER TABLE payment_webhook_events ALTER COLUMN payment_id DROP NOT NULL;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'orders_payment_provider_check') THEN
        ALTER TABLE orders ADD CONSTRAINT orders_payment_provider_check
            CHECK (payment_provider IS NULL OR BTRIM(payment_provider) <> '') NOT VALID;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'payment_webhook_events_provider_check') THEN
        ALTER TABLE payment_webhook_events ADD CONSTRAINT payment_webhook_events_provider_check
            CHECK (payment_provider IS NULL OR BTRIM(payment_provider) <> '') NOT VALID;
    END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS idx_orders_provider_payment_id
    ON orders (payment_provider, provider_payment_id)
    WHERE payment_provider IS NOT NULL AND provider_payment_id IS NOT NULL;

DROP INDEX IF EXISTS idx_payment_webhook_events_provider_payment_id;

CREATE INDEX IF NOT EXISTS idx_payment_webhook_events_provider_payment_id
    ON payment_webhook_events (payment_provider, provider_payment_id)
    WHERE payment_provider IS NOT NULL AND provider_payment_id IS NOT NULL;
