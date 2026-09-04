CREATE SCHEMA IF NOT EXISTS commerce;

DO $$
BEGIN
    IF to_regclass('commerce.marketing_subscriptions') IS NULL
        AND to_regclass('public.marketing_subscriptions') IS NOT NULL THEN
        ALTER TABLE public.marketing_subscriptions SET SCHEMA commerce;
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS commerce.marketing_subscriptions (
    id BIGSERIAL PRIMARY KEY,
    email TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'subscribed' CHECK (status IN ('subscribed', 'unsubscribed')),
    source TEXT NOT NULL DEFAULT 'storefront',
    consent_at TIMESTAMPTZ,
    unsubscribed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT marketing_subscriptions_email_not_blank CHECK (length(trim(email)) > 3)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_marketing_subscriptions_email
    ON commerce.marketing_subscriptions (lower(email));
CREATE INDEX IF NOT EXISTS idx_marketing_subscriptions_status_created
    ON commerce.marketing_subscriptions (status, created_at DESC, id DESC);
