CREATE TABLE IF NOT EXISTS resolution_manifests (
  manifest_id TEXT PRIMARY KEY,
  release_id TEXT NOT NULL,
  source_node_id TEXT REFERENCES federation_nodes(node_id),
  source_event_id TEXT REFERENCES federation_events(event_id),
  encoding TEXT NOT NULL DEFAULT 'jcs-json',
  compression TEXT,
  encrypted BOOLEAN NOT NULL DEFAULT FALSE,
  canonical_manifest_json TEXT,
  body_json JSONB,
  body_blob BYTEA,
  nzb_sha256 TEXT,
  generated_nzb BYTEA,
  fetched_at TIMESTAMPTZ,
  verified_at TIMESTAMPTZ,
  validation_status TEXT NOT NULL DEFAULT 'unknown',
  rejection_reason TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_resolution_manifests_release
  ON resolution_manifests(release_id);

CREATE TABLE IF NOT EXISTS federated_manifest_sources (
  manifest_id TEXT NOT NULL,
  release_id TEXT,
  source_node_id TEXT NOT NULL REFERENCES federation_nodes(node_id),
  pool_id TEXT NOT NULL,
  advertised BOOLEAN NOT NULL DEFAULT TRUE,
  last_success_at TIMESTAMPTZ,
  last_failure_at TIMESTAMPTZ,
  failure_count INTEGER NOT NULL DEFAULT 0,
  avg_latency_ms INTEGER,
  trust_score DOUBLE PRECISION NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY(manifest_id, source_node_id, pool_id)
);

CREATE INDEX IF NOT EXISTS idx_federated_manifest_sources_release
  ON federated_manifest_sources(release_id);
