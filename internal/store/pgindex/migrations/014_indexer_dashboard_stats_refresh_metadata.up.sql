ALTER TABLE indexer_dashboard_stats
  ADD COLUMN IF NOT EXISTS refresh_attempted_at TIMESTAMPTZ;

ALTER TABLE indexer_dashboard_stats
  ADD COLUMN IF NOT EXISTS last_error TEXT NOT NULL DEFAULT '';
