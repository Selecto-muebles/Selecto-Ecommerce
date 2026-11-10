-- New storage only: existing photos and their constraints remain unchanged.
CREATE TABLE IF NOT EXISTS product_videos (
    id BIGSERIAL PRIMARY KEY,
    product_id INTEGER NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    mime_type TEXT NOT NULL DEFAULT 'video/mp4' CHECK (mime_type='video/mp4'),
    alt_text TEXT NOT NULL DEFAULT '',
    sort_order INTEGER NOT NULL DEFAULT 0 CHECK (sort_order >= 0),
    content BYTEA NOT NULL,
    size_bytes INTEGER NOT NULL CHECK (size_bytes > 0 AND size_bytes <= 20971520),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_product_videos_product_sort ON product_videos(product_id,sort_order,id);
