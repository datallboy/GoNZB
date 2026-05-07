CREATE TABLE IF NOT EXISTS indexer_dashboard_stats (
  stat_key TEXT PRIMARY KEY,
  int_value BIGINT NOT NULL DEFAULT 0,
  updated_at TIMESTAMPTZ
);
