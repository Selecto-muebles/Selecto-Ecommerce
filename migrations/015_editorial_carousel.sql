-- Additive. Existing products/categories and application revisions remain compatible.
CREATE TABLE IF NOT EXISTS carousel_slides (
    id BIGSERIAL PRIMARY KEY,
    title TEXT NOT NULL CHECK (CHAR_LENGTH(BTRIM(title)) BETWEEN 1 AND 100),
    subtitle TEXT NOT NULL DEFAULT '' CHECK (CHAR_LENGTH(subtitle) <= 240),
    alt_text TEXT NOT NULL CHECK (CHAR_LENGTH(BTRIM(alt_text)) BETWEEN 1 AND 180),
    cta_label TEXT NOT NULL CHECK (CHAR_LENGTH(BTRIM(cta_label)) BETWEEN 1 AND 50),
    target_kind TEXT NOT NULL CHECK (target_kind IN ('product','category')),
    product_id INTEGER REFERENCES products(id) ON DELETE SET NULL,
    category_id BIGINT REFERENCES categories(id) ON DELETE SET NULL,
    CONSTRAINT carousel_target_kind CHECK (
      (target_kind='product' AND category_id IS NULL) OR
      (target_kind='category' AND product_id IS NULL)),
    sort_order INTEGER NOT NULL DEFAULT 0 CHECK (sort_order BETWEEN 0 AND 999),
    active BOOLEAN NOT NULL DEFAULT FALSE,
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    mime_type TEXT NOT NULL CHECK (mime_type IN ('image/jpeg','image/png')),
    content BYTEA NOT NULL CHECK (OCTET_LENGTH(content) BETWEEN 1 AND 2097152),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_carousel_order ON carousel_slides(active,sort_order,id);
