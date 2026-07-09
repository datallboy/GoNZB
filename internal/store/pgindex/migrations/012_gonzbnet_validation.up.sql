CREATE TABLE IF NOT EXISTS federation_validation_tasks (
  task_id BIGSERIAL PRIMARY KEY,
  manifest_id TEXT NOT NULL REFERENCES resolution_manifests(manifest_id) ON DELETE CASCADE,
  release_id TEXT NOT NULL,
  source_node_id TEXT REFERENCES federation_nodes(node_id),
  source_event_id TEXT REFERENCES federation_events(event_id),
  pool_id TEXT NOT NULL DEFAULT 'pool.local',
  status TEXT NOT NULL DEFAULT 'pending',
  priority INTEGER NOT NULL DEFAULT 0,
  attempts INTEGER NOT NULL DEFAULT 0,
  last_error TEXT,
  claimed_by_node_id TEXT,
  claimed_at TIMESTAMPTZ,
  due_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  completed_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(manifest_id, pool_id)
);

CREATE INDEX IF NOT EXISTS idx_federation_validation_tasks_ready
  ON federation_validation_tasks(status, due_at, priority DESC);

CREATE TABLE IF NOT EXISTS article_availability_attestations (
  attestation_id TEXT PRIMARY KEY,
  release_id TEXT NOT NULL,
  manifest_id TEXT NOT NULL,
  author_node_id TEXT NOT NULL REFERENCES federation_nodes(node_id),
  pool_id TEXT NOT NULL,
  checked_at TIMESTAMPTZ NOT NULL,
  status TEXT NOT NULL,
  articles_total INTEGER NOT NULL DEFAULT 0,
  articles_available INTEGER NOT NULL DEFAULT 0,
  missing_articles INTEGER NOT NULL DEFAULT 0,
  provider_backbone_hash TEXT,
  retention_days_observed INTEGER NOT NULL DEFAULT 0,
  confidence DOUBLE PRECISION NOT NULL DEFAULT 0,
  validation_score DOUBLE PRECISION NOT NULL DEFAULT 0,
  method TEXT,
  body_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  source_event_id TEXT NOT NULL REFERENCES federation_events(event_id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_article_availability_release
  ON article_availability_attestations(release_id, checked_at DESC);

CREATE INDEX IF NOT EXISTS idx_article_availability_manifest
  ON article_availability_attestations(manifest_id, checked_at DESC);

CREATE TABLE IF NOT EXISTS checksum_attestations (
  attestation_id TEXT PRIMARY KEY,
  release_id TEXT NOT NULL,
  manifest_id TEXT NOT NULL,
  author_node_id TEXT NOT NULL REFERENCES federation_nodes(node_id),
  pool_id TEXT NOT NULL,
  checked_at TIMESTAMPTZ NOT NULL,
  status TEXT NOT NULL,
  checksums_total INTEGER NOT NULL DEFAULT 0,
  checksums_verified INTEGER NOT NULL DEFAULT 0,
  checksums_failed INTEGER NOT NULL DEFAULT 0,
  confidence DOUBLE PRECISION NOT NULL DEFAULT 0,
  checksum_score DOUBLE PRECISION NOT NULL DEFAULT 0,
  method TEXT,
  body_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  source_event_id TEXT NOT NULL REFERENCES federation_events(event_id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_checksum_attestations_release
  ON checksum_attestations(release_id, checked_at DESC);

CREATE INDEX IF NOT EXISTS idx_checksum_attestations_manifest
  ON checksum_attestations(manifest_id, checked_at DESC);

ALTER TABLE federated_release_sources
  ADD COLUMN IF NOT EXISTS validation_score DOUBLE PRECISION NOT NULL DEFAULT 0;

ALTER TABLE federated_release_sources
  ADD COLUMN IF NOT EXISTS validation_attestation_count INTEGER NOT NULL DEFAULT 0;

ALTER TABLE federated_release_sources
  ADD COLUMN IF NOT EXISTS checksum_score DOUBLE PRECISION NOT NULL DEFAULT 0;

ALTER TABLE trust_pools
  ALTER COLUMN accepted_event_types SET DEFAULT '["ReleaseCard", "HealthAttestation", "Tombstone", "ValidatorCapacity", "ArticleAvailabilityAttestation", "ChecksumAttestation"]'::jsonb;

UPDATE trust_pools
SET accepted_event_types = (
    SELECT jsonb_agg(DISTINCT event_type)
    FROM jsonb_array_elements_text(
      accepted_event_types ||
      '["ValidatorCapacity", "ArticleAvailabilityAttestation", "ChecksumAttestation"]'::jsonb
    ) AS event_types(event_type)
  ),
  updated_at = NOW()
WHERE NOT accepted_event_types ? 'ArticleAvailabilityAttestation';
