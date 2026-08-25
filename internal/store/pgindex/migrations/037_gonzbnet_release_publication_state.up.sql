CREATE TABLE IF NOT EXISTS federated_release_publication_states (
  release_id TEXT NOT NULL,
  source_node_id TEXT NOT NULL REFERENCES federation_nodes(node_id),
  pool_id TEXT NOT NULL,
  manifest_id TEXT,
  state TEXT NOT NULL CHECK (state IN ('active', 'withdrawn')),
  reason TEXT NOT NULL DEFAULT '',
  effective_at TIMESTAMPTZ NOT NULL,
  source_event_id TEXT NOT NULL REFERENCES federation_events(event_id),
  source_sequence BIGINT NOT NULL,
  supersedes_event_id TEXT,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (release_id, source_node_id, pool_id)
);

CREATE INDEX IF NOT EXISTS idx_federated_release_publication_states_active
  ON federated_release_publication_states(pool_id, release_id, state);
