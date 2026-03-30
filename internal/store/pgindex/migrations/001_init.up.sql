-- Milestone 5 PG foundation schema
-- Source/scrape control + header ingest constraints.

CREATE TABLE IF NOT EXISTS usenet_providers (
  id BIGSERIAL PRIMARY KEY,
  provider_key TEXT NOT NULL UNIQUE,
  display_name TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS newsgroups (
  id BIGSERIAL PRIMARY KEY,
  group_name TEXT NOT NULL UNIQUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS scrape_runs (
  id BIGSERIAL PRIMARY KEY,
  provider_id BIGINT NOT NULL REFERENCES usenet_providers(id) ON DELETE RESTRICT,
  started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  finished_at TIMESTAMPTZ,
  status TEXT NOT NULL DEFAULT 'running',
  error_text TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_scrape_runs_provider_id_started_at
ON scrape_runs(provider_id, started_at DESC);

CREATE TABLE IF NOT EXISTS scrape_checkpoints (
  id BIGSERIAL PRIMARY KEY,
  provider_id BIGINT NOT NULL REFERENCES usenet_providers(id) ON DELETE CASCADE,
  newsgroup_id BIGINT NOT NULL REFERENCES newsgroups(id) ON DELETE CASCADE,
  last_article_number BIGINT NOT NULL DEFAULT 0,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (provider_id, newsgroup_id)
);

CREATE INDEX IF NOT EXISTS idx_scrape_checkpoints_provider_newsgroup
ON scrape_checkpoints(provider_id, newsgroup_id);

CREATE TABLE IF NOT EXISTS article_headers (
  id BIGSERIAL PRIMARY KEY,
  provider_id BIGINT NOT NULL REFERENCES usenet_providers(id) ON DELETE RESTRICT,
  newsgroup_id BIGINT NOT NULL REFERENCES newsgroups(id) ON DELETE RESTRICT,
  article_number BIGINT NOT NULL,
  message_id TEXT NOT NULL,
  subject TEXT NOT NULL DEFAULT '',
  poster TEXT NOT NULL DEFAULT '',
  date_utc TIMESTAMPTZ,
  bytes BIGINT NOT NULL DEFAULT 0,
  lines INTEGER NOT NULL DEFAULT 0,
  xref TEXT NOT NULL DEFAULT '',
  raw_overview_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  scraped_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (newsgroup_id, article_number),
  UNIQUE (newsgroup_id, message_id)
);

CREATE INDEX IF NOT EXISTS idx_article_headers_newsgroup_id_article_number
ON article_headers(newsgroup_id, article_number DESC);

CREATE INDEX IF NOT EXISTS idx_article_headers_newsgroup_id_date_utc
ON article_headers(newsgroup_id, date_utc DESC);
