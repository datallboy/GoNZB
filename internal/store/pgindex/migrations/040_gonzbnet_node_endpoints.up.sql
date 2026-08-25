CREATE TABLE IF NOT EXISTS federation_node_endpoints (
  id BIGSERIAL PRIMARY KEY,
  node_id TEXT NOT NULL REFERENCES federation_nodes(node_id) ON DELETE CASCADE,
  transport_type TEXT NOT NULL,
  locator TEXT NOT NULL,
  priority INTEGER NOT NULL DEFAULT 100,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  path_type TEXT NOT NULL DEFAULT '',
  ice_state TEXT NOT NULL DEFAULT '',
  rtt_ms BIGINT NOT NULL DEFAULT 0,
  reconnect_count BIGINT NOT NULL DEFAULT 0,
  bytes_sent BIGINT NOT NULL DEFAULT 0,
  bytes_received BIGINT NOT NULL DEFAULT 0,
  failure_count INTEGER NOT NULL DEFAULT 0,
  last_success_at TIMESTAMPTZ,
  last_failure_at TIMESTAMPTZ,
  last_error TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT federation_node_endpoints_transport_check
    CHECK (transport_type IN ('https', 'ice')),
  CONSTRAINT federation_node_endpoints_priority_check CHECK (priority >= 0),
  CONSTRAINT federation_node_endpoints_locator_unique UNIQUE (node_id, locator)
);

INSERT INTO federation_node_endpoints (node_id, transport_type, locator, priority, enabled)
SELECT node_id, 'https', base_url, 100, TRUE
FROM federation_nodes
WHERE COALESCE(base_url, '') <> ''
ON CONFLICT (node_id, locator) DO NOTHING;

CREATE INDEX IF NOT EXISTS idx_federation_node_endpoints_selection
  ON federation_node_endpoints (node_id, enabled, priority, transport_type);

CREATE INDEX IF NOT EXISTS idx_federation_node_endpoints_locator
  ON federation_node_endpoints (locator);

CREATE INDEX IF NOT EXISTS idx_federation_node_endpoints_coordinator
  ON federation_node_endpoints ((lower(split_part(split_part(locator, '@', 2), '/', 1))))
  WHERE transport_type = 'ice' AND enabled = TRUE;
