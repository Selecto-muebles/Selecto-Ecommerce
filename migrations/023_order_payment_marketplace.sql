-- Preserve the payment route when an existing preference is recovered after a retry.
ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS active_payment_marketplace BOOLEAN NOT NULL DEFAULT FALSE;
