CREATE TABLE IF NOT EXISTS release_overrides (
  release_id TEXT PRIMARY KEY REFERENCES releases(release_id) ON DELETE CASCADE,
  display_title TEXT NOT NULL DEFAULT '',
  classification_override TEXT NOT NULL DEFAULT '',
  tmdb_id_override BIGINT NOT NULL DEFAULT 0,
  tvdb_id_override BIGINT NOT NULL DEFAULT 0,
  imdb_id_override TEXT NOT NULL DEFAULT '',
  hidden BOOLEAN NOT NULL DEFAULT FALSE,
  notes TEXT NOT NULL DEFAULT '',
  tags_json JSONB NOT NULL DEFAULT '[]'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
