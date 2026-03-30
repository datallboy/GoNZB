-- Milestone 6 assembly layer foundation.

CREATE TABLE IF NOT EXISTS posters (
  id BIGSERIAL PRIMARY KEY,
  poster_name TEXT NOT NULL UNIQUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS article_poster_map (
  article_header_id BIGINT PRIMARY KEY REFERENCES article_headers(id) ON DELETE CASCADE,
  poster_id BIGINT NOT NULL REFERENCES posters(id) ON DELETE RESTRICT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_article_poster_map_poster_id
ON article_poster_map(poster_id);

CREATE TABLE IF NOT EXISTS binaries (
  id BIGSERIAL PRIMARY KEY,
  provider_id BIGINT NOT NULL REFERENCES usenet_providers(id) ON DELETE RESTRICT,
  newsgroup_id BIGINT NOT NULL REFERENCES newsgroups(id) ON DELETE RESTRICT,
  poster_id BIGINT REFERENCES posters(id) ON DELETE SET NULL,
  release_key TEXT NOT NULL,
  release_name TEXT NOT NULL DEFAULT '',
  binary_key TEXT NOT NULL,
  binary_name TEXT NOT NULL DEFAULT '',
  file_name TEXT NOT NULL DEFAULT '',
  total_parts INTEGER NOT NULL DEFAULT 0,
  observed_parts INTEGER NOT NULL DEFAULT 0,
  total_bytes BIGINT NOT NULL DEFAULT 0,
  first_article_number BIGINT NOT NULL DEFAULT 0,
  last_article_number BIGINT NOT NULL DEFAULT 0,
  posted_at TIMESTAMPTZ,
  status TEXT NOT NULL DEFAULT 'assembled',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (provider_id, newsgroup_id, binary_key)
);

CREATE INDEX IF NOT EXISTS idx_binaries_release_key
ON binaries(provider_id, newsgroup_id, release_key);

CREATE INDEX IF NOT EXISTS idx_binaries_updated_at
ON binaries(updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_binaries_poster_id
ON binaries(poster_id);

CREATE TABLE IF NOT EXISTS binary_parts (
  id BIGSERIAL PRIMARY KEY,
  binary_id BIGINT NOT NULL REFERENCES binaries(id) ON DELETE CASCADE,
  article_header_id BIGINT NOT NULL UNIQUE REFERENCES article_headers(id) ON DELETE CASCADE,
  message_id TEXT NOT NULL DEFAULT '',
  part_number INTEGER NOT NULL,
  total_parts INTEGER NOT NULL DEFAULT 0,
  segment_bytes BIGINT NOT NULL DEFAULT 0,
  file_name TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (binary_id, part_number)
);

CREATE INDEX IF NOT EXISTS idx_binary_parts_binary_id
ON binary_parts(binary_id);

CREATE INDEX IF NOT EXISTS idx_binary_parts_message_id
ON binary_parts(message_id);

CREATE TABLE IF NOT EXISTS part_repair_queue (
  id BIGSERIAL PRIMARY KEY,
  binary_id BIGINT NOT NULL REFERENCES binaries(id) ON DELETE CASCADE,
  reason TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  last_error TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (binary_id, reason)
);

CREATE INDEX IF NOT EXISTS idx_part_repair_queue_status
ON part_repair_queue(status);