-- Milestone 6 release formation, enrichment scaffolding, and NZB cache metadata.


CREATE TABLE IF NOT EXISTS releases (
  release_id TEXT PRIMARY KEY,
  guid TEXT NOT NULL UNIQUE,
  provider_id BIGINT NOT NULL REFERENCES usenet_providers(id) ON DELETE RESTRICT,
  release_key TEXT NOT NULL,
  title TEXT NOT NULL DEFAULT '',
  search_title TEXT NOT NULL DEFAULT '',
  category TEXT NOT NULL DEFAULT 'usenet',
  poster TEXT NOT NULL DEFAULT '',
  size_bytes BIGINT NOT NULL DEFAULT 0,
  posted_at TIMESTAMPTZ,
  file_count INTEGER NOT NULL DEFAULT 0,
  par_file_count INTEGER NOT NULL DEFAULT 0,
  completion_pct DOUBLE PRECISION NOT NULL DEFAULT 0,
  source_kind TEXT NOT NULL DEFAULT 'usenet_index',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (provider_id, release_key)
);

CREATE INDEX IF NOT EXISTS idx_releases_search_title
ON releases(search_title);

CREATE INDEX IF NOT EXISTS idx_releases_posted_at
ON releases(posted_at DESC);

CREATE INDEX IF NOT EXISTS idx_releases_provider_release_key
ON releases(provider_id, release_key);

CREATE TABLE IF NOT EXISTS release_files (
  id BIGSERIAL PRIMARY KEY,
  release_id TEXT NOT NULL REFERENCES releases(release_id) ON DELETE CASCADE,
  binary_id BIGINT REFERENCES binaries(id) ON DELETE SET NULL,
  file_name TEXT NOT NULL,
  size_bytes BIGINT NOT NULL DEFAULT 0,
  file_index INTEGER NOT NULL DEFAULT 0,
  is_pars BOOLEAN NOT NULL DEFAULT FALSE,
  subject TEXT NOT NULL DEFAULT '',
  poster TEXT NOT NULL DEFAULT '',
  posted_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (release_id, file_name)
);

CREATE INDEX IF NOT EXISTS idx_release_files_release_id
ON release_files(release_id);

CREATE TABLE IF NOT EXISTS release_file_articles (
  id BIGSERIAL PRIMARY KEY,
  release_file_id BIGINT NOT NULL REFERENCES release_files(id) ON DELETE CASCADE,
  article_header_id BIGINT NOT NULL REFERENCES article_headers(id) ON DELETE CASCADE,
  part_number INTEGER NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (release_file_id, article_header_id),
  UNIQUE (release_file_id, part_number)
);

CREATE INDEX IF NOT EXISTS idx_release_file_articles_release_file_id
ON release_file_articles(release_file_id);

CREATE TABLE IF NOT EXISTS release_newsgroups (
  release_id TEXT NOT NULL REFERENCES releases(release_id) ON DELETE CASCADE,
  newsgroup_id BIGINT NOT NULL REFERENCES newsgroups(id) ON DELETE RESTRICT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (release_id, newsgroup_id)
);

CREATE TABLE IF NOT EXISTS regex_rules (
  id BIGSERIAL PRIMARY KEY,
  rule_name TEXT NOT NULL UNIQUE,
  pattern TEXT NOT NULL,
  replacement TEXT NOT NULL DEFAULT '',
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS regex_hits (
  id BIGSERIAL PRIMARY KEY,
  release_id TEXT NOT NULL REFERENCES releases(release_id) ON DELETE CASCADE,
  rule_id BIGINT NOT NULL REFERENCES regex_rules(id) ON DELETE RESTRICT,
  field_name TEXT NOT NULL DEFAULT '',
  matched_text TEXT NOT NULL DEFAULT '',
  captured_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (release_id, rule_id, field_name, matched_text)
);

CREATE INDEX IF NOT EXISTS idx_regex_hits_release_id
ON regex_hits(release_id);

CREATE TABLE IF NOT EXISTS predb_entries (
  id BIGSERIAL PRIMARY KEY,
  normalized_title TEXT NOT NULL UNIQUE,
  title TEXT NOT NULL,
  category TEXT NOT NULL DEFAULT '',
  source TEXT NOT NULL DEFAULT '',
  posted_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS release_predb_matches (
  release_id TEXT NOT NULL REFERENCES releases(release_id) ON DELETE CASCADE,
  predb_entry_id BIGINT NOT NULL REFERENCES predb_entries(id) ON DELETE RESTRICT,
  confidence DOUBLE PRECISION NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (release_id, predb_entry_id)
);

CREATE TABLE IF NOT EXISTS nzb_cache (
  release_id TEXT PRIMARY KEY REFERENCES releases(release_id) ON DELETE CASCADE,
  generation_status TEXT NOT NULL DEFAULT 'pending',
  nzb_hash_sha256 TEXT NOT NULL DEFAULT '',
  generated_at TIMESTAMPTZ,
  last_error TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_nzb_cache_generation_status
ON nzb_cache(generation_status);