CREATE TABLE IF NOT EXISTS federated_release_cards (
  release_id TEXT PRIMARY KEY,
  manifest_id TEXT,
  title TEXT NOT NULL,
  normalized_title TEXT NOT NULL,
  category_json JSONB NOT NULL DEFAULT '[]'::jsonb,
  newznab_categories JSONB NOT NULL DEFAULT '[]'::jsonb,
  size_bytes BIGINT NOT NULL DEFAULT 0,
  posted_at TIMESTAMPTZ,
  groups_json JSONB NOT NULL DEFAULT '[]'::jsonb,
  file_count INTEGER NOT NULL DEFAULT 0,
  segment_count INTEGER NOT NULL DEFAULT 0,
  poster_hash TEXT,
  subject_fingerprint TEXT NOT NULL,
  file_fingerprint TEXT NOT NULL,
  media_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  quality_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  flags_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  resolution_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  body_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  best_score DOUBLE PRECISION NOT NULL DEFAULT 0,
  availability_score DOUBLE PRECISION NOT NULL DEFAULT 0,
  manifest_confidence_score DOUBLE PRECISION NOT NULL DEFAULT 0,
  trust_score DOUBLE PRECISION NOT NULL DEFAULT 0,
  resolvable BOOLEAN NOT NULL DEFAULT FALSE,
  status TEXT NOT NULL DEFAULT 'candidate',
  source_event_id TEXT REFERENCES federation_events(event_id),
  first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_federated_release_cards_normalized_title
  ON federated_release_cards(normalized_title);

CREATE INDEX IF NOT EXISTS idx_federated_release_cards_posted
  ON federated_release_cards(posted_at DESC);

CREATE INDEX IF NOT EXISTS idx_federated_release_cards_resolvable_score
  ON federated_release_cards(resolvable, best_score DESC);

CREATE TABLE IF NOT EXISTS federated_release_sources (
  release_id TEXT NOT NULL REFERENCES federated_release_cards(release_id) ON DELETE CASCADE,
  manifest_id TEXT,
  source_node_id TEXT NOT NULL REFERENCES federation_nodes(node_id),
  source_event_id TEXT NOT NULL REFERENCES federation_events(event_id),
  pool_id TEXT NOT NULL,
  trust_score DOUBLE PRECISION NOT NULL DEFAULT 0,
  availability_score DOUBLE PRECISION NOT NULL DEFAULT 0,
  manifest_confidence_score DOUBLE PRECISION NOT NULL DEFAULT 0,
  resolvable BOOLEAN NOT NULL DEFAULT FALSE,
  first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY(release_id, source_node_id, pool_id)
);

CREATE INDEX IF NOT EXISTS idx_federated_release_sources_manifest
  ON federated_release_sources(manifest_id);

CREATE INDEX IF NOT EXISTS idx_federated_release_sources_pool
  ON federated_release_sources(pool_id, resolvable);
