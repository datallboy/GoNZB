CREATE TABLE IF NOT EXISTS federation_nonce_replay_cache (
  node_id TEXT NOT NULL,
  nonce TEXT NOT NULL,
  seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY(node_id, nonce)
);

CREATE INDEX IF NOT EXISTS idx_federation_nonce_expires
  ON federation_nonce_replay_cache(expires_at);

CREATE TABLE IF NOT EXISTS federation_peer_deliveries (
  peer_id BIGINT NOT NULL REFERENCES federation_peers(id) ON DELETE CASCADE,
  event_id TEXT NOT NULL REFERENCES federation_events(event_id) ON DELETE CASCADE,
  status TEXT NOT NULL DEFAULT 'pending',
  attempts INTEGER NOT NULL DEFAULT 0,
  last_attempt_at TIMESTAMPTZ,
  delivered_at TIMESTAMPTZ,
  last_error TEXT,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY(peer_id, event_id)
);

CREATE INDEX IF NOT EXISTS idx_federation_peer_deliveries_status
  ON federation_peer_deliveries(peer_id, status, updated_at);
