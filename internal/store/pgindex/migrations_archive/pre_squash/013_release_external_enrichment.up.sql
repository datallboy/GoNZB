ALTER TABLE releases
ADD COLUMN IF NOT EXISTS tmdb_id BIGINT NOT NULL DEFAULT 0;

ALTER TABLE releases
ADD COLUMN IF NOT EXISTS tvdb_id BIGINT NOT NULL DEFAULT 0;

ALTER TABLE releases
ADD COLUMN IF NOT EXISTS external_media_type TEXT NOT NULL DEFAULT '';

ALTER TABLE releases
ADD COLUMN IF NOT EXISTS original_media_title TEXT NOT NULL DEFAULT '';

ALTER TABLE releases
ADD COLUMN IF NOT EXISTS external_year INTEGER NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS release_tmdb_matches (
  release_id TEXT NOT NULL REFERENCES releases(release_id) ON DELETE CASCADE,
  tmdb_id BIGINT NOT NULL,
  media_type TEXT NOT NULL DEFAULT '',
  title TEXT NOT NULL DEFAULT '',
  original_title TEXT NOT NULL DEFAULT '',
  year INTEGER NOT NULL DEFAULT 0,
  confidence DOUBLE PRECISION NOT NULL DEFAULT 0,
  chosen BOOLEAN NOT NULL DEFAULT FALSE,
  payload_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (release_id, tmdb_id, media_type)
);

CREATE TABLE IF NOT EXISTS release_tvdb_matches (
  release_id TEXT NOT NULL REFERENCES releases(release_id) ON DELETE CASCADE,
  tvdb_id BIGINT NOT NULL,
  media_type TEXT NOT NULL DEFAULT 'tv',
  title TEXT NOT NULL DEFAULT '',
  original_title TEXT NOT NULL DEFAULT '',
  year INTEGER NOT NULL DEFAULT 0,
  confidence DOUBLE PRECISION NOT NULL DEFAULT 0,
  chosen BOOLEAN NOT NULL DEFAULT FALSE,
  payload_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (release_id, tvdb_id)
);

CREATE INDEX IF NOT EXISTS idx_release_tmdb_matches_release_id
ON release_tmdb_matches(release_id);

CREATE INDEX IF NOT EXISTS idx_release_tvdb_matches_release_id
ON release_tvdb_matches(release_id);
