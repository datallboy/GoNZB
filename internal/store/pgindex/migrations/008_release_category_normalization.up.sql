ALTER TABLE releases
ADD COLUMN IF NOT EXISTS category_id INTEGER NOT NULL DEFAULT 8010;

CREATE INDEX IF NOT EXISTS idx_releases_category_id
ON releases(category_id);
