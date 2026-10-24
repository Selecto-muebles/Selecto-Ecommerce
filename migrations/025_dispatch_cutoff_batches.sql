-- A cutoff is a local Argentina dispatch day at 13:00. Membership is durable
-- and unique by order; downloading or opening WhatsApp never mutates it.
CREATE TABLE IF NOT EXISTS dispatch_cutoff_batches (
    id BIGSERIAL PRIMARY KEY,
    cutoff_at TIMESTAMPTZ NOT NULL UNIQUE,
    sealed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS dispatch_cutoff_batch_orders (
    batch_id BIGINT NOT NULL REFERENCES dispatch_cutoff_batches(id) ON DELETE RESTRICT,
    order_id INTEGER NOT NULL UNIQUE REFERENCES orders(id) ON DELETE RESTRICT,
    email_snapshot TEXT NOT NULL,
    shipment_status_snapshot TEXT NOT NULL,
    PRIMARY KEY (batch_id, order_id)
);

CREATE INDEX IF NOT EXISTS idx_dispatch_cutoff_batch_orders_batch
    ON dispatch_cutoff_batch_orders(batch_id);
