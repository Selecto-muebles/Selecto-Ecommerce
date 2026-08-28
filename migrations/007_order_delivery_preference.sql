ALTER TABLE order_shipping_addresses
    ADD COLUMN IF NOT EXISTS requested_delivery_date DATE;
