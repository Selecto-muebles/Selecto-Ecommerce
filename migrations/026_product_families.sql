CREATE TABLE IF NOT EXISTS product_families (
    id SERIAL PRIMARY KEY,
    name TEXT NOT NULL CHECK (LENGTH(BTRIM(name)) BETWEEN 2 AND 120),
    axes JSONB NOT NULL CHECK (jsonb_typeof(axes) = 'array'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_product_families_name ON product_families (LOWER(name));

ALTER TABLE products ADD COLUMN IF NOT EXISTS family_id INTEGER REFERENCES product_families(id) ON DELETE RESTRICT;
ALTER TABLE products ADD COLUMN IF NOT EXISTS variant_values JSONB NOT NULL DEFAULT '{}'::jsonb
    CHECK (jsonb_typeof(variant_values) = 'object');

CREATE INDEX IF NOT EXISTS idx_products_family_id ON products(family_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_products_family_variant_active
    ON products (family_id, (variant_values::text))
    WHERE family_id IS NOT NULL AND archived_at IS NULL;

-- Link the four existing listings only when every expected product is present exactly once.
-- Their identifiers, prices, images, descriptions, stock and order history remain unchanged.
DO $$
DECLARE
    family_key INTEGER;
BEGIN
    IF (SELECT COUNT(*) = 4 AND COUNT(DISTINCT name) = 4 FROM products
        WHERE archived_at IS NULL AND category = 'Calistenia' AND name IN
          ('Paralelas De Selecto 30 Cm', 'Paralelas Negras 30 Cm', 'Paralelas Naturales 40 Cm', 'Paralelas Negras 40 Cm'))
    THEN
        INSERT INTO product_families(name, axes)
        VALUES ('Paralelas Selecto', '["Largo", "Color"]'::jsonb)
        ON CONFLICT (LOWER(name)) DO UPDATE SET name = EXCLUDED.name
        RETURNING id INTO family_key;

        UPDATE products SET family_id = family_key,
            variant_values = jsonb_build_object(
                'Largo', CASE WHEN name LIKE '%40 Cm' THEN '40 cm' ELSE '30 cm' END,
                'Color', CASE WHEN name ILIKE '%Negras%' THEN 'Negro' ELSE 'Natural' END
            )
        WHERE archived_at IS NULL AND category = 'Calistenia' AND name IN
          ('Paralelas De Selecto 30 Cm', 'Paralelas Negras 30 Cm', 'Paralelas Naturales 40 Cm', 'Paralelas Negras 40 Cm');
    END IF;
END $$;
