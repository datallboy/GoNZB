CREATE TABLE IF NOT EXISTS resolution_manifest_events (
  manifest_id TEXT NOT NULL REFERENCES resolution_manifests(manifest_id) ON DELETE CASCADE,
  pool_id TEXT NOT NULL REFERENCES trust_pools(pool_id),
  author_node_id TEXT NOT NULL REFERENCES federation_nodes(node_id),
  source_event_id TEXT NOT NULL UNIQUE REFERENCES federation_events(event_id),
  fetched_from_node_id TEXT REFERENCES federation_nodes(node_id),
  verified_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (manifest_id, pool_id, author_node_id)
);

INSERT INTO resolution_manifest_events (
  manifest_id,
  pool_id,
  author_node_id,
  source_event_id,
  fetched_from_node_id,
  verified_at,
  updated_at
)
SELECT DISTINCT ON (manifest.manifest_id, pool_id.value, event.author_node_id)
       manifest.manifest_id,
       pool_id.value,
       event.author_node_id,
       event.event_id,
       manifest.fetched_from_node_id,
       COALESCE(manifest.verified_at, event.received_at),
       NOW()
FROM resolution_manifests manifest
JOIN federation_events event
  ON event.event_type = 'ResolutionManifest'
 AND event.validation_status = 'accepted'
 AND event.body_json->>'manifest_id' = manifest.manifest_id
 AND event.body_json->>'release_id' = manifest.release_id
CROSS JOIN LATERAL jsonb_array_elements_text(event.pool_ids) AS pool_id(value)
JOIN trust_pools pool ON pool.pool_id = pool_id.value
WHERE jsonb_array_length(event.pool_ids) = 1
ORDER BY manifest.manifest_id, pool_id.value, event.author_node_id, event.created_at DESC, event.event_id DESC
ON CONFLICT (manifest_id, pool_id, author_node_id) DO UPDATE SET
  source_event_id = EXCLUDED.source_event_id,
  fetched_from_node_id = EXCLUDED.fetched_from_node_id,
  verified_at = EXCLUDED.verified_at,
  updated_at = NOW();

CREATE INDEX IF NOT EXISTS idx_resolution_manifest_events_pool_manifest
  ON resolution_manifest_events(pool_id, manifest_id, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_federation_events_resolution_manifest_id
  ON federation_events ((body_json->>'manifest_id'), created_at DESC)
  WHERE event_type = 'ResolutionManifest' AND validation_status = 'accepted';
