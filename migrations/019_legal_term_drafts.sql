CREATE TABLE IF NOT EXISTS legal_term_drafts (
    slug TEXT PRIMARY KEY,
    base_document_id BIGINT NOT NULL REFERENCES legal_documents(id),
    sections JSONB NOT NULL,
    revision BIGINT NOT NULL DEFAULT 1,
    updated_by TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
