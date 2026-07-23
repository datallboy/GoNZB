CREATE TABLE IF NOT EXISTS federation_peers (
  id BIGSERIAL PRIMARY KEY,
  node_id TEXT REFERENCES federation_nodes(node_id),
  peer_url TEXT NOT NULL UNIQUE,
  source TEXT NOT NULL DEFAULT 'manual',
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  pinned_public_key BYTEA,
  last_connected_at TIMESTAMPTZ,
  last_sync_at TIMESTAMPTZ,
  failure_count INTEGER NOT NULL DEFAULT 0,
  last_error TEXT,
  status TEXT NOT NULL DEFAULT 'pending',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_federation_peers_enabled_status
  ON federation_peers(enabled, status);

CREATE TABLE IF NOT EXISTS federation_peer_cursors (
  peer_id BIGINT NOT NULL REFERENCES federation_peers(id) ON DELETE CASCADE,
  pool_id TEXT NOT NULL DEFAULT '',
  event_type TEXT NOT NULL DEFAULT '',
  cursor TEXT,
  last_event_id TEXT,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY(peer_id, pool_id, event_type)
);
