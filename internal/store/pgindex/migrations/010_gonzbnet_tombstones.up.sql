CREATE TABLE IF NOT EXISTS tombstone_votes (
  id BIGSERIAL PRIMARY KEY,
  target_type TEXT NOT NULL,
  target_id TEXT NOT NULL,
  pool_id TEXT,
  reason TEXT NOT NULL,
  severity TEXT NOT NULL,
  author_node_id TEXT NOT NULL REFERENCES federation_nodes(node_id),
  source_event_id TEXT NOT NULL REFERENCES federation_events(event_id),
  evidence_event_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
  effective_at TIMESTAMPTZ NOT NULL,
  expires_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(source_event_id)
);

CREATE INDEX IF NOT EXISTS idx_tombstone_votes_target
  ON tombstone_votes(target_type, target_id, pool_id);

CREATE TABLE IF NOT EXISTS tombstones (
  id BIGSERIAL PRIMARY KEY,
  target_type TEXT NOT NULL,
  target_id TEXT NOT NULL,
  pool_id TEXT,
  reason TEXT NOT NULL,
  severity TEXT NOT NULL,
  source_event_id TEXT NOT NULL REFERENCES federation_events(event_id),
  active BOOLEAN NOT NULL DEFAULT FALSE,
  approval_count INTEGER NOT NULL DEFAULT 0,
  approvals_required INTEGER NOT NULL DEFAULT 1,
  effective_at TIMESTAMPTZ NOT NULL,
  expires_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_tombstones_unique_target
  ON tombstones(target_type, target_id, COALESCE(pool_id, '__local__'));

CREATE INDEX IF NOT EXISTS idx_tombstones_active_target
  ON tombstones(active, target_type, target_id);

ALTER TABLE trust_pools
  ALTER COLUMN accepted_event_types SET DEFAULT '["ReleaseCard", "HealthAttestation", "Tombstone"]'::jsonb;

UPDATE trust_pools
SET accepted_event_types = accepted_event_types || '["Tombstone"]'::jsonb,
    updated_at = NOW()
WHERE NOT (accepted_event_types ? 'Tombstone');
