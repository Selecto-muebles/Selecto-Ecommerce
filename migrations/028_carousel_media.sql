-- Additive: originals, destinations, drafts and publication versions are retained.
CREATE TABLE IF NOT EXISTS carousel_media (
    slide_id BIGINT PRIMARY KEY REFERENCES carousel_slides(id) ON DELETE CASCADE,
    slide_version INTEGER NOT NULL CHECK (slide_version > 0),
    small_content BYTEA,
    small_mime TEXT CHECK (small_mime IN ('image/jpeg','image/png')),
    large_content BYTEA,
    large_mime TEXT CHECK (large_mime IN ('image/jpeg','image/png')),
    video_content BYTEA CHECK (video_content IS NULL OR OCTET_LENGTH(video_content) BETWEEN 1 AND 20971520)
);
