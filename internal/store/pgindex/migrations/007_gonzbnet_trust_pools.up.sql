CREATE TABLE IF NOT EXISTS trust_pools (
  pool_id TEXT PRIMARY KEY,
  display_name TEXT NOT NULL,
  description TEXT,
  genesis_event_id TEXT REFERENCES federation_events(event_id),
  policy_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  membership_threshold INTEGER NOT NULL DEFAULT 1,
  moderation_threshold INTEGER NOT NULL DEFAULT 1,
  checkpoint_witness_threshold INTEGER NOT NULL DEFAULT 1,
  accept_mode TEXT NOT NULL DEFAULT 'pool_member',
  min_node_trust_score DOUBLE PRECISION NOT NULL DEFAULT 0,
  accepted_event_types JSONB NOT NULL DEFAULT '["ReleaseCard"]'::jsonb,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  latest_checkpoint_event_id TEXT,
  latest_merkle_root TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_trust_pools_enabled
  ON trust_pools(enabled);

CREATE TABLE IF NOT EXISTS pool_members (
  pool_id TEXT NOT NULL REFERENCES trust_pools(pool_id) ON DELETE CASCADE,
  node_id TEXT NOT NULL REFERENCES federation_nodes(node_id),
  role TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'active',
  approved_event_id TEXT REFERENCES federation_events(event_id),
  revoked_event_id TEXT REFERENCES federation_events(event_id),
  joined_at TIMESTAMPTZ,
  revoked_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY(pool_id, node_id, role)
);

CREATE INDEX IF NOT EXISTS idx_pool_members_node
  ON pool_members(node_id, status);

CREATE INDEX IF NOT EXISTS idx_pool_members_pool_status
  ON pool_members(pool_id, status, role);
