CREATE TABLE IF NOT EXISTS federation_activity_rollups (
  bucket_start TIMESTAMPTZ NOT NULL,
  bucket_seconds INTEGER NOT NULL,
  node_id TEXT NOT NULL DEFAULT '',
  pool_id TEXT NOT NULL DEFAULT '',
  component TEXT NOT NULL,
  job TEXT NOT NULL,
  attempts BIGINT NOT NULL DEFAULT 0,
  successes BIGINT NOT NULL DEFAULT 0,
  failures BIGINT NOT NULL DEFAULT 0,
  items_in BIGINT NOT NULL DEFAULT 0,
  items_out BIGINT NOT NULL DEFAULT 0,
  bytes_in BIGINT NOT NULL DEFAULT 0,
  bytes_out BIGINT NOT NULL DEFAULT 0,
  duration_ms BIGINT NOT NULL DEFAULT 0,
  last_error TEXT,
  last_attempt_at TIMESTAMPTZ,
  last_success_at TIMESTAMPTZ,
  last_failure_at TIMESTAMPTZ,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (bucket_start, bucket_seconds, node_id, pool_id, component)
);

CREATE INDEX IF NOT EXISTS idx_federation_activity_rollups_pool_time
  ON federation_activity_rollups(pool_id, bucket_start DESC);

CREATE INDEX IF NOT EXISTS idx_federation_activity_rollups_node_time
  ON federation_activity_rollups(node_id, bucket_start DESC);

CREATE INDEX IF NOT EXISTS idx_federation_activity_rollups_job_time
  ON federation_activity_rollups(job, bucket_start DESC);
