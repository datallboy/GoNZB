CREATE TABLE IF NOT EXISTS health_attestations (
  attestation_id TEXT PRIMARY KEY,
  manifest_id TEXT,
  release_id TEXT NOT NULL,
  author_node_id TEXT NOT NULL REFERENCES federation_nodes(node_id),
  pool_id TEXT,
  checked_at TIMESTAMPTZ NOT NULL,
  status TEXT NOT NULL,
  articles_total INTEGER,
  articles_available INTEGER,
  missing_articles INTEGER,
  repair_available BOOLEAN,
  repair_confidence DOUBLE PRECISION NOT NULL DEFAULT 0,
  provider_backbone_hash TEXT,
  retention_days_observed INTEGER,
  confidence DOUBLE PRECISION NOT NULL DEFAULT 0,
  availability_score DOUBLE PRECISION NOT NULL DEFAULT 0,
  method TEXT,
  body_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  source_event_id TEXT NOT NULL REFERENCES federation_events(event_id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_health_attestations_release
  ON health_attestations(release_id, checked_at DESC);

CREATE INDEX IF NOT EXISTS idx_health_attestations_manifest
  ON health_attestations(manifest_id, checked_at DESC);

CREATE INDEX IF NOT EXISTS idx_health_attestations_pool
  ON health_attestations(pool_id, release_id);

CREATE TABLE IF NOT EXISTS reputation_events (
  id BIGSERIAL PRIMARY KEY,
  node_id TEXT NOT NULL REFERENCES federation_nodes(node_id),
  pool_id TEXT,
  event_id TEXT REFERENCES federation_events(event_id),
  delta DOUBLE PRECISION NOT NULL,
  reason TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_reputation_events_node
  ON reputation_events(node_id, created_at DESC);

ALTER TABLE trust_pools
  ALTER COLUMN accepted_event_types SET DEFAULT '["ReleaseCard", "HealthAttestation"]'::jsonb;

UPDATE trust_pools
SET accepted_event_types = '["ReleaseCard", "HealthAttestation"]'::jsonb,
    updated_at = NOW()
WHERE accepted_event_types = '["ReleaseCard"]'::jsonb;
