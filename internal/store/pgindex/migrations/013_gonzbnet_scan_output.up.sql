CREATE TABLE IF NOT EXISTS gonzbnet_scan_outputs (
  scan_id TEXT PRIMARY KEY,
  body_json JSONB NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  published_event_id TEXT REFERENCES federation_events(event_id),
  published_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_gonzbnet_scan_outputs_status
  ON gonzbnet_scan_outputs(status, updated_at);

CREATE TABLE IF NOT EXISTS manifest_availability_attestations (
  attestation_id TEXT PRIMARY KEY,
  release_id TEXT NOT NULL,
  manifest_id TEXT NOT NULL,
  author_node_id TEXT NOT NULL REFERENCES federation_nodes(node_id),
  pool_id TEXT NOT NULL,
  checked_at TIMESTAMPTZ NOT NULL,
  status TEXT NOT NULL,
  confidence DOUBLE PRECISION NOT NULL DEFAULT 0,
  method TEXT,
  body_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  source_event_id TEXT NOT NULL REFERENCES federation_events(event_id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_manifest_availability_release
  ON manifest_availability_attestations(release_id, checked_at DESC);

CREATE INDEX IF NOT EXISTS idx_manifest_availability_manifest
  ON manifest_availability_attestations(manifest_id, checked_at DESC);

ALTER TABLE trust_pools
  ALTER COLUMN accepted_event_types SET DEFAULT '["ReleaseCard", "HealthAttestation", "Tombstone", "ValidatorCapacity", "ArticleAvailabilityAttestation", "ChecksumAttestation", "ManifestAvailability"]'::jsonb;

UPDATE trust_pools
SET accepted_event_types = (
    SELECT jsonb_agg(DISTINCT event_type)
    FROM jsonb_array_elements_text(
      accepted_event_types ||
      '["ManifestAvailability"]'::jsonb
    ) AS event_types(event_type)
  ),
  updated_at = NOW()
WHERE NOT accepted_event_types ? 'ManifestAvailability';
