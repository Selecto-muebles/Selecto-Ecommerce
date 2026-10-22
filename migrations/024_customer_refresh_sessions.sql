CREATE TABLE IF NOT EXISTS customer_refresh_sessions (
    id BIGSERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    session_version BIGINT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    rotated_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_customer_refresh_sessions_user
    ON customer_refresh_sessions(user_id);

CREATE INDEX IF NOT EXISTS idx_customer_refresh_sessions_expires
    ON customer_refresh_sessions(expires_at);
