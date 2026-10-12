ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS terms_version TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS terms_accepted_at TIMESTAMPTZ;

CREATE TABLE IF NOT EXISTS legal_documents (
    id BIGSERIAL PRIMARY KEY,
    slug TEXT NOT NULL,
    version TEXT NOT NULL,
    title TEXT NOT NULL,
    body_markdown TEXT NOT NULL,
    body_sha256 TEXT NOT NULL,
    published BOOLEAN NOT NULL DEFAULT FALSE,
    effective_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (slug, version)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_legal_documents_one_published
    ON legal_documents(slug) WHERE published;

CREATE TABLE IF NOT EXISTS legal_acceptances (
    id BIGSERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id),
    order_id INTEGER NOT NULL UNIQUE REFERENCES orders(id),
    document_id BIGINT NOT NULL REFERENCES legal_documents(id),
    accepted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    source TEXT NOT NULL DEFAULT 'checkout'
);
CREATE INDEX IF NOT EXISTS idx_legal_acceptances_user ON legal_acceptances(user_id, accepted_at DESC);

CREATE TABLE IF NOT EXISTS consumer_withdrawal_requests (
    id BIGSERIAL PRIMARY KEY,
    public_code TEXT NOT NULL UNIQUE,
    idempotency_key TEXT NOT NULL UNIQUE,
    request_sha256 TEXT NOT NULL,
    order_reference TEXT NOT NULL,
    order_id INTEGER REFERENCES orders(id),
    customer_email TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'received'
        CHECK (status IN ('received','in_review','refund_in_progress','resolved')),
    admin_note TEXT NOT NULL DEFAULT '',
    updated_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_consumer_withdrawals_queue ON consumer_withdrawal_requests(status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_consumer_withdrawals_email ON consumer_withdrawal_requests(lower(customer_email), created_at DESC);

ALTER TABLE email_outbox DROP CONSTRAINT IF EXISTS email_outbox_template_check;
ALTER TABLE email_outbox ADD CONSTRAINT email_outbox_template_check CHECK (template IN (
    'verify_email','password_reset','order_created','payment_status','shipment_status',
    'newsletter_unsubscribe','withdrawal_ack','withdrawal_update'
));
