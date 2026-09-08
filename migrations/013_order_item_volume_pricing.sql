ALTER TABLE order_items
    ADD COLUMN IF NOT EXISTS original_unit_price NUMERIC(12, 2),
    ADD COLUMN IF NOT EXISTS discount_percent NUMERIC(5, 2) NOT NULL DEFAULT 0;

UPDATE order_items
SET original_unit_price = price
WHERE original_unit_price IS NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'order_items_original_unit_price_check'
    ) THEN
        ALTER TABLE order_items
            ADD CONSTRAINT order_items_original_unit_price_check
            CHECK (original_unit_price IS NULL OR original_unit_price >= price);
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'order_items_discount_percent_check'
    ) THEN
        ALTER TABLE order_items
            ADD CONSTRAINT order_items_discount_percent_check
            CHECK (discount_percent >= 0 AND discount_percent <= 100);
    END IF;
END $$;
