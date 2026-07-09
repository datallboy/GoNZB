CREATE TABLE IF NOT EXISTS federation_nodes (
  node_id TEXT PRIMARY KEY,
  public_key BYTEA NOT NULL,
  alias TEXT,
  software TEXT,
  software_version TEXT,
  actor_url TEXT,
  base_url TEXT,
  inbox_url TEXT,
  outbox_url TEXT,
  ws_url TEXT,
  capabilities JSONB NOT NULL DEFAULT '{}'::jsonb,
  profile_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_seen_at TIMESTAMPTZ,
  last_verified_at TIMESTAMPTZ,
  local_trust_score DOUBLE PRECISION NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'unknown',
  blocked_reason TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_federation_nodes_status
  ON federation_nodes(status);

CREATE TABLE IF NOT EXISTS federation_events (
  event_id TEXT PRIMARY KEY,
  spec_version TEXT NOT NULL,
  event_type TEXT NOT NULL,
  author_node_id TEXT NOT NULL,
  author_public_key BYTEA NOT NULL,
  sequence BIGINT NOT NULL,
  previous_event_id TEXT,
  body_schema TEXT NOT NULL,
  body_hash TEXT NOT NULL,
  signature_alg TEXT NOT NULL,
  signature BYTEA NOT NULL,
  canonical_event_json TEXT NOT NULL,
  body_json JSONB NOT NULL,
  pool_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
  visibility TEXT NOT NULL DEFAULT 'pool',
  created_at TIMESTAMPTZ NOT NULL,
  not_before TIMESTAMPTZ,
  expires_at TIMESTAMPTZ,
  received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  validation_status TEXT NOT NULL DEFAULT 'pending',
  rejection_reason TEXT,
  projected BOOLEAN NOT NULL DEFAULT FALSE,
  projected_at TIMESTAMPTZ,
  UNIQUE(author_node_id, sequence)
);

CREATE INDEX IF NOT EXISTS idx_federation_events_type_created
  ON federation_events(event_type, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_federation_events_author_sequence
  ON federation_events(author_node_id, sequence);

CREATE INDEX IF NOT EXISTS idx_federation_events_pool_ids_gin
  ON federation_events USING GIN(pool_ids);

CREATE INDEX IF NOT EXISTS idx_federation_events_body_gin
  ON federation_events USING GIN(body_json jsonb_path_ops);

CREATE TABLE IF NOT EXISTS federation_rejected_events (
  id BIGSERIAL PRIMARY KEY,
  event_id TEXT,
  author_node_id TEXT,
  event_type TEXT,
  raw_event_json TEXT NOT NULL,
  rejection_reason TEXT NOT NULL,
  received_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_federation_rejected_events_received
  ON federation_rejected_events(received_at DESC);
