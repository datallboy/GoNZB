CREATE TABLE IF NOT EXISTS federation_pending_projections (
  event_id TEXT PRIMARY KEY REFERENCES federation_events(event_id) ON DELETE CASCADE,
  event_type TEXT NOT NULL,
  projection_kind TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  attempts INTEGER NOT NULL DEFAULT 0,
  last_error TEXT,
  first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  resolved_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_federation_pending_projections_status
  ON federation_pending_projections(status, last_attempt_at);
