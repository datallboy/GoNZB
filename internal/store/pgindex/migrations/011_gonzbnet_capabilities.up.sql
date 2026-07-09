CREATE TABLE IF NOT EXISTS federation_node_capabilities (
  node_id TEXT PRIMARY KEY REFERENCES federation_nodes(node_id) ON DELETE CASCADE,
  capabilities JSONB NOT NULL DEFAULT '{}'::jsonb,
  module_status JSONB NOT NULL DEFAULT '{}'::jsonb,
  scanner_capacity JSONB,
  validator_capacity JSONB,
  provider_scope JSONB,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE pool_members
  ADD COLUMN IF NOT EXISTS allowed_capabilities JSONB NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE pool_members
  ADD COLUMN IF NOT EXISTS limits_json JSONB NOT NULL DEFAULT '{}'::jsonb;

CREATE INDEX IF NOT EXISTS idx_pool_members_capabilities
  ON pool_members USING GIN(allowed_capabilities);
