-- Product trust, richer commercial information and operational archival.
-- The migration is additive and is executed transactionally by the migration runner.
ALTER TABLE products
    ADD COLUMN IF NOT EXISTS specifications JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS archived_at TIMESTAMPTZ;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='products_specifications_object') THEN
        ALTER TABLE products ADD CONSTRAINT products_specifications_object
            CHECK (jsonb_typeof(specifications)='object');
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS product_reviews (
    id BIGSERIAL PRIMARY KEY,
    product_id INTEGER NOT NULL REFERENCES products(id) ON DELETE RESTRICT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    order_id INTEGER NOT NULL REFERENCES orders(id) ON DELETE RESTRICT,
    rating SMALLINT NOT NULL CHECK (rating BETWEEN 1 AND 5),
    title TEXT NOT NULL DEFAULT '' CHECK (char_length(title) <= 120),
    comment TEXT NOT NULL CHECK (char_length(BTRIM(comment)) BETWEEN 10 AND 2000),
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','published','rejected')),
    moderated_by TEXT,
    moderated_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, product_id)
);

CREATE INDEX IF NOT EXISTS idx_product_reviews_public
    ON product_reviews(product_id, created_at DESC) WHERE status='published';
CREATE INDEX IF NOT EXISTS idx_product_reviews_moderation
    ON product_reviews(status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_products_archived_at
    ON products(archived_at) WHERE archived_at IS NOT NULL;

-- This staging artefact was explicitly requested to disappear from operations.
-- Preserve referential history instead of deleting order evidence.
UPDATE products
SET active=FALSE, archived_at=COALESCE(archived_at, NOW()), updated_at=NOW()
WHERE sku='RBM-STG-001' AND name='Ray-Ban Meta - Producto de prueba';
