CREATE INDEX IF NOT EXISTS idx_products_created_at
    ON products(created_at DESC);

CREATE INDEX IF NOT EXISTS idx_products_sku
    ON products(sku)
    WHERE sku IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_orders_created_at
    ON orders(created_at DESC);

CREATE INDEX IF NOT EXISTS idx_orders_paid_at
    ON orders(paid_at DESC)
    WHERE paid_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at
    ON audit_logs(created_at DESC);
