CREATE TABLE IF NOT EXISTS scanner_heartbeats (
  node_id TEXT NOT NULL REFERENCES federation_nodes(node_id) ON DELETE CASCADE,
  pool_id TEXT NOT NULL,
  published_at TIMESTAMPTZ NOT NULL,
  groups_json JSONB NOT NULL DEFAULT '[]'::jsonb,
  active_claims_json JSONB NOT NULL DEFAULT '[]'::jsonb,
  status TEXT NOT NULL,
  body_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  source_event_id TEXT NOT NULL REFERENCES federation_events(event_id),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY(node_id, pool_id)
);

CREATE INDEX IF NOT EXISTS idx_scanner_heartbeats_pool_status
  ON scanner_heartbeats(pool_id, status, published_at DESC);

CREATE TABLE IF NOT EXISTS coverage_stale_claim_penalties (
  claim_id TEXT PRIMARY KEY,
  node_id TEXT NOT NULL REFERENCES federation_nodes(node_id),
  pool_id TEXT NOT NULL,
  group_name TEXT NOT NULL,
  expired_at TIMESTAMPTZ NOT NULL,
  penalty_score DOUBLE PRECISION NOT NULL DEFAULT 0,
  reason TEXT NOT NULL DEFAULT 'stale_claim',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE trust_pools
  ALTER COLUMN accepted_event_types SET DEFAULT '["ReleaseCard", "HealthAttestation", "Tombstone", "ValidatorCapacity", "ArticleAvailabilityAttestation", "ChecksumAttestation", "ManifestAvailability", "ScannerCapacity", "ScannerHeartbeat", "GroupObservation", "CoveragePlan", "CoverageAssignment", "RangeClaim", "TimeWindowClaim", "CoverageCheckpoint", "RangeComplete", "RangeFailed"]'::jsonb;

UPDATE trust_pools
SET accepted_event_types = (
    SELECT jsonb_agg(DISTINCT event_type)
    FROM jsonb_array_elements_text(
      accepted_event_types ||
      '["ScannerHeartbeat"]'::jsonb
    ) AS event_types(event_type)
  ),
  updated_at = NOW()
WHERE NOT accepted_event_types ? 'ScannerHeartbeat';
