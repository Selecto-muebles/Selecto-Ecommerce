-- Immutable dispatch snapshots. Existing orders and shipments are untouched.
CREATE TABLE IF NOT EXISTS logistics_cutoff_batches (
    id BIGSERIAL PRIMARY KEY,
    cutoff_at TIMESTAMPTZ NOT NULL UNIQUE,
    csv_content BYTEA NOT NULL,
    csv_sha256 TEXT NOT NULL,
    order_count INTEGER NOT NULL CHECK (order_count >= 0),
    line_count INTEGER NOT NULL CHECK (line_count >= 0),
    excluded_order_count INTEGER NOT NULL DEFAULT 0 CHECK (excluded_order_count >= 0),
    legacy_review_required BOOLEAN NOT NULL DEFAULT FALSE,
    state TEXT NOT NULL DEFAULT 'prepared' CHECK (state IN ('prepared', 'handed_off')),
    prepared_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    handed_off_at TIMESTAMPTZ,
    handed_off_by TEXT,
    handoff_channel TEXT,
    handoff_recipient TEXT,
    handoff_note TEXT NOT NULL DEFAULT '',
    CONSTRAINT logistics_handoff_complete CHECK (
        (state = 'prepared' AND handed_off_at IS NULL AND handed_off_by IS NULL
            AND handoff_channel IS NULL AND handoff_recipient IS NULL)
        OR (state = 'handed_off' AND handed_off_at IS NOT NULL AND handed_off_by IS NOT NULL
            AND handoff_channel IS NOT NULL AND handoff_recipient IS NOT NULL)
    )
);

CREATE TABLE IF NOT EXISTS logistics_cutoff_batch_orders (
    batch_id BIGINT NOT NULL REFERENCES logistics_cutoff_batches(id) ON DELETE RESTRICT,
    order_id INTEGER NOT NULL UNIQUE REFERENCES orders(id) ON DELETE RESTRICT,
    PRIMARY KEY (batch_id, order_id)
);

CREATE INDEX IF NOT EXISTS idx_logistics_cutoff_batches_recent
    ON logistics_cutoff_batches(cutoff_at DESC);
