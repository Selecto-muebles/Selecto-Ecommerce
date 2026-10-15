-- Freeze dispatch identifiers for new orders. Historical rows can only be
-- backfilled from the catalog as it exists when this migration runs.
ALTER TABLE order_items
    ADD COLUMN IF NOT EXISTS product_name_snapshot TEXT,
    ADD COLUMN IF NOT EXISTS sku_snapshot TEXT;

UPDATE order_items AS oi
SET product_name_snapshot = COALESCE(oi.product_name_snapshot, p.name),
    sku_snapshot = COALESCE(oi.sku_snapshot, p.sku, '')
FROM products AS p
WHERE oi.product_id = p.id
  AND (oi.product_name_snapshot IS NULL OR oi.sku_snapshot IS NULL);

UPDATE order_items
SET product_name_snapshot = COALESCE(product_name_snapshot, ''),
    sku_snapshot = COALESCE(sku_snapshot, '')
WHERE product_name_snapshot IS NULL OR sku_snapshot IS NULL;

ALTER TABLE order_items
    ALTER COLUMN product_name_snapshot SET DEFAULT '',
    ALTER COLUMN product_name_snapshot SET NOT NULL,
    ALTER COLUMN sku_snapshot SET DEFAULT '',
    ALTER COLUMN sku_snapshot SET NOT NULL;
