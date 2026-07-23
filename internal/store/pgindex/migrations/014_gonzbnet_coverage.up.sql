CREATE TABLE IF NOT EXISTS scanner_capacities (
  node_id TEXT PRIMARY KEY REFERENCES federation_nodes(node_id) ON DELETE CASCADE,
  published_at TIMESTAMPTZ NOT NULL,
  groups_json JSONB NOT NULL DEFAULT '[]'::jsonb,
  max_ranges_per_hour INTEGER NOT NULL DEFAULT 0,
  max_bytes_per_hour BIGINT NOT NULL DEFAULT 0,
  body_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  source_event_id TEXT NOT NULL REFERENCES federation_events(event_id),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS coverage_group_observations (
  observation_id TEXT PRIMARY KEY,
  pool_id TEXT NOT NULL,
  group_name TEXT NOT NULL,
  observed_at TIMESTAMPTZ NOT NULL,
  low_watermark BIGINT NOT NULL DEFAULT 0,
  high_watermark BIGINT NOT NULL DEFAULT 0,
  retention_days INTEGER NOT NULL DEFAULT 0,
  confidence DOUBLE PRECISION NOT NULL DEFAULT 0,
  author_node_id TEXT NOT NULL REFERENCES federation_nodes(node_id),
  body_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  source_event_id TEXT NOT NULL REFERENCES federation_events(event_id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_coverage_group_observations_pool_group
  ON coverage_group_observations(pool_id, group_name, observed_at DESC);

CREATE TABLE IF NOT EXISTS coverage_plans (
  plan_id TEXT PRIMARY KEY,
  pool_id TEXT NOT NULL,
  group_name TEXT NOT NULL,
  range_start BIGINT,
  range_end BIGINT,
  window_start TIMESTAMPTZ,
  window_end TIMESTAMPTZ,
  priority INTEGER NOT NULL DEFAULT 0,
  author_node_id TEXT NOT NULL REFERENCES federation_nodes(node_id),
  body_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  source_event_id TEXT NOT NULL REFERENCES federation_events(event_id),
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS coverage_assignments (
  assignment_id TEXT PRIMARY KEY,
  plan_id TEXT,
  pool_id TEXT NOT NULL,
  group_name TEXT NOT NULL,
  assigned_node_id TEXT NOT NULL,
  range_start BIGINT,
  range_end BIGINT,
  window_start TIMESTAMPTZ,
  window_end TIMESTAMPTZ,
  priority INTEGER NOT NULL DEFAULT 0,
  due_at TIMESTAMPTZ,
  status TEXT NOT NULL DEFAULT 'assigned',
  author_node_id TEXT NOT NULL REFERENCES federation_nodes(node_id),
  body_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  source_event_id TEXT NOT NULL REFERENCES federation_events(event_id),
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_coverage_assignments_node_status
  ON coverage_assignments(assigned_node_id, status, priority DESC);

CREATE TABLE IF NOT EXISTS coverage_claims (
  claim_id TEXT PRIMARY KEY,
  claim_type TEXT NOT NULL,
  assignment_id TEXT,
  pool_id TEXT NOT NULL,
  group_name TEXT NOT NULL,
  node_id TEXT NOT NULL,
  range_start BIGINT,
  range_end BIGINT,
  window_start TIMESTAMPTZ,
  window_end TIMESTAMPTZ,
  claimed_at TIMESTAMPTZ NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  status TEXT NOT NULL DEFAULT 'active',
  author_node_id TEXT NOT NULL REFERENCES federation_nodes(node_id),
  body_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  source_event_id TEXT NOT NULL REFERENCES federation_events(event_id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_coverage_claims_active
  ON coverage_claims(pool_id, group_name, expires_at)
  WHERE status = 'active';

CREATE TABLE IF NOT EXISTS coverage_checkpoints (
  checkpoint_id TEXT PRIMARY KEY,
  pool_id TEXT NOT NULL,
  group_name TEXT NOT NULL,
  low_watermark BIGINT NOT NULL DEFAULT 0,
  high_watermark BIGINT NOT NULL DEFAULT 0,
  author_node_id TEXT NOT NULL REFERENCES federation_nodes(node_id),
  body_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  source_event_id TEXT NOT NULL REFERENCES federation_events(event_id),
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS coverage_range_outcomes (
  outcome_id TEXT PRIMARY KEY,
  outcome_type TEXT NOT NULL,
  claim_id TEXT,
  assignment_id TEXT,
  pool_id TEXT NOT NULL,
  group_name TEXT NOT NULL,
  node_id TEXT NOT NULL,
  range_start BIGINT NOT NULL,
  range_end BIGINT NOT NULL,
  release_count INTEGER NOT NULL DEFAULT 0,
  reason TEXT,
  occurred_at TIMESTAMPTZ NOT NULL,
  author_node_id TEXT NOT NULL REFERENCES federation_nodes(node_id),
  body_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  source_event_id TEXT NOT NULL REFERENCES federation_events(event_id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_coverage_range_outcomes_pool_group
  ON coverage_range_outcomes(pool_id, group_name, occurred_at DESC);

ALTER TABLE trust_pools
  ALTER COLUMN accepted_event_types SET DEFAULT '["ReleaseCard", "HealthAttestation", "Tombstone", "ValidatorCapacity", "ArticleAvailabilityAttestation", "ChecksumAttestation", "ManifestAvailability", "ScannerCapacity", "GroupObservation", "CoveragePlan", "CoverageAssignment", "RangeClaim", "TimeWindowClaim", "CoverageCheckpoint", "RangeComplete", "RangeFailed"]'::jsonb;

UPDATE trust_pools
SET accepted_event_types = (
    SELECT jsonb_agg(DISTINCT event_type)
    FROM jsonb_array_elements_text(
      accepted_event_types ||
      '["ScannerCapacity", "GroupObservation", "CoveragePlan", "CoverageAssignment", "RangeClaim", "TimeWindowClaim", "CoverageCheckpoint", "RangeComplete", "RangeFailed"]'::jsonb
    ) AS event_types(event_type)
  ),
  updated_at = NOW()
WHERE NOT accepted_event_types ? 'CoverageAssignment';
