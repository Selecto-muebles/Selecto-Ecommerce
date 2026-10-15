-- This ledger is intentionally inert until Gestionar confirms the normal-sale
-- contract. An uncertain request must never be retried automatically.
CREATE TABLE IF NOT EXISTS gestionar_dispatches (
    order_id INTEGER PRIMARY KEY REFERENCES orders(id) ON DELETE CASCADE,
    submission_key TEXT NOT NULL UNIQUE,
    package_state TEXT NOT NULL DEFAULT 'reserved'
        CHECK (package_state IN ('reserved', 'in_flight', 'uncertain', 'confirmed', 'rejected')),
    external_package_id TEXT UNIQUE,
    attempted_at TIMESTAMPTZ,
    confirmed_at TIMESTAMPTZ,
    last_error_code TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT gestionar_confirmed_package_id CHECK (
        (package_state = 'confirmed' AND NULLIF(BTRIM(external_package_id), '') IS NOT NULL)
        OR (package_state <> 'confirmed' AND external_package_id IS NULL)
    )
);

CREATE INDEX IF NOT EXISTS idx_gestionar_dispatches_state
    ON gestionar_dispatches(package_state, updated_at);

CREATE TABLE IF NOT EXISTS gestionar_pickup_batches (
    id BIGSERIAL PRIMARY KEY,
    request_key TEXT NOT NULL UNIQUE,
    pickup_time_id INTEGER NOT NULL CHECK (pickup_time_id > 0),
    state TEXT NOT NULL DEFAULT 'reserved'
        CHECK (state IN ('reserved', 'in_flight', 'uncertain', 'confirmed', 'rejected')),
    external_pickup_id TEXT UNIQUE,
    attempted_at TIMESTAMPTZ,
    confirmed_at TIMESTAMPTZ,
    last_error_code TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS gestionar_pickup_orders (
    batch_id BIGINT NOT NULL REFERENCES gestionar_pickup_batches(id) ON DELETE CASCADE,
    order_id INTEGER NOT NULL UNIQUE REFERENCES gestionar_dispatches(order_id) ON DELETE CASCADE,
    PRIMARY KEY (batch_id, order_id)
);

CREATE TABLE IF NOT EXISTS gestionar_status_events (
    id BIGSERIAL PRIMARY KEY,
    order_id INTEGER NOT NULL REFERENCES gestionar_dispatches(order_id) ON DELETE CASCADE,
    event_key TEXT NOT NULL UNIQUE,
    external_status TEXT NOT NULL,
    occurred_at TIMESTAMPTZ,
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT gestionar_status_nonempty CHECK (BTRIM(external_status) <> '')
);

CREATE INDEX IF NOT EXISTS idx_gestionar_status_events_order
    ON gestionar_status_events(order_id, received_at DESC);
