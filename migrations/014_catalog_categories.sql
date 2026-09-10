-- Additive migration. The runner wraps this file and its ledger entry in one transaction.
CREATE TABLE IF NOT EXISTS categories (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL CHECK (BTRIM(name) <> ''),
    slug TEXT NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
    active BOOLEAN NOT NULL DEFAULT TRUE,
    sort_order INTEGER NOT NULL DEFAULT 0 CHECK (sort_order >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS categories_normalized_name ON categories (LOWER(BTRIM(name)));
ALTER TABLE products ADD COLUMN IF NOT EXISTS category_id BIGINT REFERENCES categories(id) ON DELETE RESTRICT;
CREATE INDEX IF NOT EXISTS products_category_id ON products(category_id);

-- Keep legacy writers compatible while old and new revisions coexist.
-- The hash suffix makes backfilled slugs deterministic even for non-ASCII names.
CREATE OR REPLACE FUNCTION sync_product_category() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    normalized TEXT;
    category_name TEXT;
BEGIN
    IF (TG_OP = 'INSERT' AND NEW.category_id IS NOT NULL)
       OR (TG_OP = 'UPDATE' AND NEW.category_id IS DISTINCT FROM OLD.category_id AND NEW.category_id IS NOT NULL) THEN
        SELECT name INTO STRICT category_name FROM categories WHERE id = NEW.category_id;
        NEW.category := category_name;
    ELSE
        normalized := LOWER(BTRIM(NEW.category));
        IF normalized = '' THEN
            NEW.category_id := NULL;
            NEW.category := '';
        ELSE
            INSERT INTO categories(name, slug)
            VALUES (BTRIM(NEW.category), 'categoria-' || MD5(normalized))
            ON CONFLICT (LOWER(BTRIM(name))) DO UPDATE SET name = categories.name
            RETURNING id, name INTO NEW.category_id, NEW.category;
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
CREATE OR REPLACE TRIGGER products_category_sync BEFORE INSERT OR UPDATE OF category, category_id
ON products FOR EACH ROW EXECUTE FUNCTION sync_product_category();
UPDATE products SET category = category WHERE BTRIM(category) <> '' AND category_id IS NULL;
