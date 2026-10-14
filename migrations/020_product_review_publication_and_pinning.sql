-- Publicar reseñas verificadas de inmediato y permitir destacarlas desde el backoffice.
-- El valor predeterminado sigue pendiente: solo la API que valida la compra publica.
ALTER TABLE product_reviews
    ADD COLUMN IF NOT EXISTS pinned BOOLEAN NOT NULL DEFAULT FALSE;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname='product_reviews_pinned_published'
          AND conrelid='product_reviews'::regclass
    ) THEN
        ALTER TABLE product_reviews
            ADD CONSTRAINT product_reviews_pinned_published
            CHECK (NOT pinned OR status='published');
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_product_reviews_public_pinned
    ON product_reviews(product_id, pinned DESC, created_at DESC, id DESC)
    WHERE status='published';
