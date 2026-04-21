CREATE TABLE IF NOT EXISTS indexer_stage_runs (
  id BIGSERIAL PRIMARY KEY,
  stage_name TEXT NOT NULL,
  trigger_kind TEXT NOT NULL DEFAULT 'scheduled',
  status TEXT NOT NULL DEFAULT 'running',
  claimed_by TEXT NOT NULL DEFAULT '',
  started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  heartbeat_at TIMESTAMPTZ,
  finished_at TIMESTAMPTZ,
  error_text TEXT NOT NULL DEFAULT '',
  metrics_json JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS idx_indexer_stage_runs_stage_started_at
ON indexer_stage_runs(stage_name, started_at DESC);

CREATE INDEX IF NOT EXISTS idx_indexer_stage_runs_stage_status
ON indexer_stage_runs(stage_name, status);

CREATE TABLE IF NOT EXISTS indexer_stage_state (
  stage_name TEXT PRIMARY KEY,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  paused BOOLEAN NOT NULL DEFAULT FALSE,
  interval_seconds INTEGER NOT NULL DEFAULT 600,
  batch_size INTEGER NOT NULL DEFAULT 0,
  concurrency INTEGER NOT NULL DEFAULT 1,
  backoff_seconds INTEGER NOT NULL DEFAULT 0,
  lease_owner TEXT NOT NULL DEFAULT '',
  lease_expires_at TIMESTAMPTZ,
  last_heartbeat_at TIMESTAMPTZ,
  last_run_id BIGINT REFERENCES indexer_stage_runs(id) ON DELETE SET NULL,
  last_success_at TIMESTAMPTZ,
  last_error TEXT NOT NULL DEFAULT '',
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_indexer_stage_state_lease_expires_at
ON indexer_stage_state(lease_expires_at);
