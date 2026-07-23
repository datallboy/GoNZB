ALTER TABLE scanner_capacities
  ADD COLUMN IF NOT EXISTS pool_id TEXT,
  ADD COLUMN IF NOT EXISTS max_groups INTEGER,
  ADD COLUMN IF NOT EXISTS max_articles_per_hour BIGINT,
  ADD COLUMN IF NOT EXISTS max_header_bytes_per_hour BIGINT,
  ADD COLUMN IF NOT EXISTS preferred_group_patterns JSONB,
  ADD COLUMN IF NOT EXISTS excluded_group_patterns JSONB,
  ADD COLUMN IF NOT EXISTS supports_article_range_scan BOOLEAN,
  ADD COLUMN IF NOT EXISTS supports_time_window_scan BOOLEAN,
  ADD COLUMN IF NOT EXISTS retention_days_observed INTEGER,
  ADD COLUMN IF NOT EXISTS provider_scope_hash TEXT;

ALTER TABLE scanner_heartbeats
  ADD COLUMN IF NOT EXISTS queue_depth INTEGER,
  ADD COLUMN IF NOT EXISTS current_articles_per_minute BIGINT;

ALTER TABLE coverage_group_observations
  ADD COLUMN IF NOT EXISTS node_id TEXT,
  ADD COLUMN IF NOT EXISTS provider_scope_hash TEXT,
  ADD COLUMN IF NOT EXISTS estimated_count BIGINT,
  ADD COLUMN IF NOT EXISTS posts_per_hour_estimate DOUBLE PRECISION,
  ADD COLUMN IF NOT EXISTS scan_supported BOOLEAN;

ALTER TABLE coverage_checkpoints
  ADD COLUMN IF NOT EXISTS node_id TEXT,
  ADD COLUMN IF NOT EXISTS provider_scope_hash TEXT,
  ADD COLUMN IF NOT EXISTS claim_id TEXT,
  ADD COLUMN IF NOT EXISTS range_start BIGINT,
  ADD COLUMN IF NOT EXISTS range_current BIGINT,
  ADD COLUMN IF NOT EXISTS range_end BIGINT,
  ADD COLUMN IF NOT EXISTS release_cards_emitted INTEGER,
  ADD COLUMN IF NOT EXISTS manifests_emitted INTEGER,
  ADD COLUMN IF NOT EXISTS errors INTEGER,
  ADD COLUMN IF NOT EXISTS checked_at TIMESTAMPTZ;
