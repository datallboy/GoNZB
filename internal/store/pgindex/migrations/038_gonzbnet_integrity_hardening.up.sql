ALTER TABLE resolution_manifests
  ADD COLUMN IF NOT EXISTS fetched_from_node_id TEXT REFERENCES federation_nodes(node_id),
  ADD COLUMN IF NOT EXISTS cache_integrity_status TEXT NOT NULL DEFAULT 'unknown',
  ADD COLUMN IF NOT EXISTS cache_integrity_failed_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS cache_integrity_error TEXT;

ALTER TABLE resolution_manifests
  DROP CONSTRAINT IF EXISTS resolution_manifests_cache_integrity_status_check;

ALTER TABLE resolution_manifests
  ADD CONSTRAINT resolution_manifests_cache_integrity_status_check
  CHECK (cache_integrity_status IN ('unknown', 'verified', 'failed'));

CREATE INDEX IF NOT EXISTS idx_federation_nodes_status_node
  ON federation_nodes(status, node_id);

CREATE INDEX IF NOT EXISTS idx_tombstones_active_target_pool
  ON tombstones(target_type, target_id, pool_id)
  WHERE active = TRUE;

CREATE INDEX IF NOT EXISTS idx_release_publication_state_effective
  ON federated_release_publication_states(pool_id, source_node_id, release_id, state);
