ALTER TABLE trust_pools
  ADD COLUMN IF NOT EXISTS visibility TEXT NOT NULL DEFAULT 'unlisted',
  ADD COLUMN IF NOT EXISTS join_mode TEXT NOT NULL DEFAULT 'approval',
  ADD COLUMN IF NOT EXISTS admission_enabled BOOLEAN NOT NULL DEFAULT TRUE;

CREATE TABLE IF NOT EXISTS federation_pool_admissions (
  proposal_event_id TEXT PRIMARY KEY REFERENCES federation_events(event_id) ON DELETE CASCADE,
  pool_id TEXT NOT NULL,
  genesis_event_id TEXT,
  candidate_node_id TEXT NOT NULL REFERENCES federation_nodes(node_id),
  candidate_url TEXT,
  relay_node_id TEXT,
  relay_url TEXT NOT NULL,
  requested_role TEXT NOT NULL DEFAULT 'member',
  requested_capabilities JSONB NOT NULL DEFAULT '[]'::jsonb,
  status TEXT NOT NULL DEFAULT 'pending',
  final_event_id TEXT REFERENCES federation_events(event_id),
  rejection_reason TEXT,
  expires_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_federation_pool_admissions_pool_status
  ON federation_pool_admissions(pool_id, status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_federation_pool_admissions_candidate
  ON federation_pool_admissions(candidate_node_id, status, created_at DESC);

CREATE TABLE IF NOT EXISTS federation_pool_approval_fragments (
  proposal_event_id TEXT NOT NULL REFERENCES federation_pool_admissions(proposal_event_id) ON DELETE CASCADE,
  admin_node_id TEXT NOT NULL REFERENCES federation_nodes(node_id),
  approved_at TIMESTAMPTZ NOT NULL,
  fragment_json JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY(proposal_event_id, admin_node_id)
);
