ALTER TABLE products
    ADD COLUMN IF NOT EXISTS description TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS category TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS product_images (
    id BIGSERIAL PRIMARY KEY,
    product_id INTEGER NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    mime_type TEXT NOT NULL CHECK (mime_type IN ('image/jpeg', 'image/png', 'image/webp')),
    alt_text TEXT NOT NULL DEFAULT '',
    sort_order INTEGER NOT NULL DEFAULT 0 CHECK (sort_order >= 0),
    content BYTEA NOT NULL,
    size_bytes INTEGER NOT NULL CHECK (size_bytes > 0 AND size_bytes <= 5242880),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS product_options (
    id BIGSERIAL PRIMARY KEY,
    product_id INTEGER NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    values JSONB NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0 CHECK (sort_order >= 0),
    CONSTRAINT product_options_name_not_blank CHECK (BTRIM(name) <> ''),
    CONSTRAINT product_options_values_array CHECK (jsonb_typeof(values) = 'array'),
    UNIQUE (product_id, name)
);

ALTER TABLE order_items
    ADD COLUMN IF NOT EXISTS selected_options JSONB NOT NULL DEFAULT '{}'::jsonb;

CREATE INDEX IF NOT EXISTS idx_product_images_product_sort
    ON product_images(product_id, sort_order, id);

CREATE INDEX IF NOT EXISTS idx_product_options_product_sort
    ON product_options(product_id, sort_order, id);

CREATE INDEX IF NOT EXISTS idx_products_category_active
    ON products(category, active, created_at DESC);
