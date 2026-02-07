-- photos: store PhotosGetInfo metadata
CREATE TABLE IF NOT EXISTS photos (
    photo_id   VARCHAR(20) PRIMARY KEY,
    info_json  JSONB NOT NULL,
    width      BIGINT DEFAULT 0,
    height     BIGINT DEFAULT 0,
    fetched_at TIMESTAMPTZ DEFAULT NOW()
);

-- photo_tags: for tag-based queries (index, related)
CREATE TABLE IF NOT EXISTS photo_tags (
    photo_id VARCHAR(20) NOT NULL REFERENCES photos(photo_id) ON DELETE CASCADE,
    tag      VARCHAR(100) NOT NULL,
    PRIMARY KEY (photo_id, tag)
);

CREATE INDEX IF NOT EXISTS idx_photo_tags_tag_photo ON photo_tags(tag, photo_id);
CREATE INDEX IF NOT EXISTS idx_photo_tags_photo ON photo_tags(photo_id);
