ALTER TABLE users
    ALTER COLUMN password DROP NOT NULL;

CREATE TABLE IF NOT EXISTS user_identities (
    id BIGSERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider TEXT NOT NULL CHECK (provider IN ('google')),
    provider_subject TEXT NOT NULL,
    provider_email TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (provider, provider_subject),
    UNIQUE (user_id, provider)
);

CREATE INDEX IF NOT EXISTS idx_user_identities_user_id
    ON user_identities(user_id);

CREATE TABLE IF NOT EXISTS order_shipping_addresses (
    order_id INTEGER PRIMARY KEY REFERENCES orders(id) ON DELETE CASCADE,
    recipient_first_name TEXT NOT NULL,
    recipient_last_name TEXT NOT NULL,
    dni TEXT NOT NULL,
    street_address TEXT NOT NULL,
    street_number TEXT NOT NULL,
    postal_code TEXT NOT NULL,
    province TEXT NOT NULL,
    locality TEXT NOT NULL,
    phone_number TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO order_shipping_addresses (
    order_id,
    recipient_first_name,
    recipient_last_name,
    dni,
    street_address,
    street_number,
    postal_code,
    province,
    locality,
    phone_number
)
SELECT
    o.id,
    COALESCE(u.first_name, ''),
    COALESCE(u.last_name, ''),
    COALESCE(u.dni, ''),
    COALESCE(u.street_address, ''),
    COALESCE(u.street_number, ''),
    COALESCE(u.postal_code, ''),
    COALESCE(u.province, ''),
    COALESCE(u.locality, ''),
    COALESCE(u.phone_number, '')
FROM orders o
JOIN users u ON u.id = o.user_id
ON CONFLICT (order_id) DO NOTHING;

CREATE TABLE IF NOT EXISTS shipments (
    id BIGSERIAL PRIMARY KEY,
    order_id INTEGER NOT NULL UNIQUE REFERENCES orders(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'preparing'
        CHECK (status IN ('preparing', 'ready_for_dispatch', 'shipped', 'delivered', 'delivery_failed', 'cancelled')),
    carrier TEXT NOT NULL DEFAULT '',
    tracking_number TEXT NOT NULL DEFAULT '',
    tracking_url TEXT NOT NULL DEFAULT '',
    estimated_delivery_at TIMESTAMPTZ,
    shipped_at TIMESTAMPTZ,
    delivered_at TIMESTAMPTZ,
    customer_note TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO shipments (order_id, status)
SELECT id, 'preparing'
FROM orders
WHERE status = 'paid'
ON CONFLICT (order_id) DO NOTHING;

CREATE INDEX IF NOT EXISTS idx_shipments_status_updated_at
    ON shipments(status, updated_at DESC);
