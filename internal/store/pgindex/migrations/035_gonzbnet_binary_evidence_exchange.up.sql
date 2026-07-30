CREATE TABLE IF NOT EXISTS yenc_header_evidence (
  evidence_id BIGSERIAL PRIMARY KEY,
  source_posted_at TIMESTAMPTZ NOT NULL,
  message_id TEXT NOT NULL,
  decoded_file_name TEXT NOT NULL,
  part_number INTEGER NOT NULL DEFAULT 0,
  total_parts INTEGER NOT NULL DEFAULT 0,
  file_size BIGINT NOT NULL DEFAULT 0,
  part_begin BIGINT NOT NULL DEFAULT 0,
  part_end BIGINT NOT NULL DEFAULT 0,
  provenance TEXT NOT NULL CHECK (provenance IN ('local_body', 'peer')),
  source_pool_id TEXT NOT NULL DEFAULT '',
  source_node_id TEXT NOT NULL DEFAULT '',
  source_bundle_id TEXT NOT NULL DEFAULT '',
  acceptance_state TEXT NOT NULL DEFAULT 'accepted'
    CHECK (acceptance_state IN ('accepted', 'quarantined', 'rejected')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (message_id, provenance, source_node_id)
);

CREATE INDEX IF NOT EXISTS idx_yenc_header_evidence_message
  ON yenc_header_evidence (message_id, acceptance_state);
CREATE INDEX IF NOT EXISTS idx_yenc_header_evidence_source_date
  ON yenc_header_evidence (source_posted_at);
CREATE INDEX IF NOT EXISTS idx_yenc_header_evidence_source_message
  ON yenc_header_evidence (source_posted_at, message_id);

CREATE TABLE IF NOT EXISTS binary_exchange_identities (
  binary_id BIGINT NOT NULL REFERENCES binary_core(binary_id) ON DELETE CASCADE,
  scheme TEXT NOT NULL CHECK (scheme IN ('subject_multipart_v1', 'yenc_v1', 'content_v1')),
  match_id TEXT NOT NULL,
  identity_json JSONB NOT NULL,
  confidence DOUBLE PRECISION NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (binary_id, scheme)
);

CREATE INDEX IF NOT EXISTS idx_binary_exchange_identities_match
  ON binary_exchange_identities (scheme, match_id);

CREATE TABLE IF NOT EXISTS binary_peer_segments (
  peer_segment_id BIGSERIAL PRIMARY KEY,
  binary_id BIGINT NOT NULL REFERENCES binary_core(binary_id) ON DELETE CASCADE,
  source_posted_at TIMESTAMPTZ NOT NULL,
  part_number INTEGER NOT NULL,
  total_parts INTEGER NOT NULL,
  message_id TEXT NOT NULL,
  segment_bytes BIGINT NOT NULL DEFAULT 0,
  posted_at TIMESTAMPTZ,
  observed_groups JSONB NOT NULL DEFAULT '[]'::jsonb,
  file_name TEXT NOT NULL DEFAULT '',
  file_size BIGINT NOT NULL DEFAULT 0,
  source_pool_id TEXT NOT NULL,
  source_node_id TEXT NOT NULL,
  source_bundle_id TEXT NOT NULL,
  acceptance_state TEXT NOT NULL DEFAULT 'accepted'
    CHECK (acceptance_state IN ('accepted', 'quarantined', 'rejected')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (binary_id, part_number, message_id)
);

CREATE INDEX IF NOT EXISTS idx_binary_peer_segments_effective
  ON binary_peer_segments (binary_id, part_number, acceptance_state);
CREATE INDEX IF NOT EXISTS idx_binary_peer_segments_source_date
  ON binary_peer_segments (source_posted_at);

CREATE TABLE IF NOT EXISTS binary_evidence_repair_work_items (
  binary_id BIGINT PRIMARY KEY REFERENCES binary_core(binary_id) ON DELETE CASCADE,
  scheme TEXT NOT NULL,
  match_id TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'ready'
    CHECK (status IN ('ready', 'running', 'done', 'stale')),
  attempts INTEGER NOT NULL DEFAULT 0,
  ready_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  lease_owner TEXT,
  lease_expires_at TIMESTAMPTZ,
  last_error TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_binary_evidence_repair_ready
  ON binary_evidence_repair_work_items (status, ready_at, binary_id);

CREATE TABLE IF NOT EXISTS binary_evidence_exchange_diagnostics (
  diagnostic_id BIGSERIAL PRIMARY KEY,
  pool_id TEXT NOT NULL,
  peer_node_id TEXT NOT NULL,
  direction TEXT NOT NULL CHECK (direction IN ('consume', 'serve')),
  evidence_kind TEXT NOT NULL CHECK (evidence_kind IN ('yenc', 'segments')),
  request_count INTEGER NOT NULL DEFAULT 0,
  hit_count INTEGER NOT NULL DEFAULT 0,
  item_count INTEGER NOT NULL DEFAULT 0,
  body_requests_avoided INTEGER NOT NULL DEFAULT 0,
  response_bytes BIGINT NOT NULL DEFAULT 0,
  conflicts INTEGER NOT NULL DEFAULT 0,
  quarantines INTEGER NOT NULL DEFAULT 0,
  latency_ms BIGINT NOT NULL DEFAULT 0,
  error_text TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_binary_evidence_exchange_diagnostics_recent
  ON binary_evidence_exchange_diagnostics (created_at DESC, pool_id, evidence_kind);

CREATE OR REPLACE VIEW binary_effective_parts AS
SELECT
  bp.binary_id,
  bp.source_posted_at,
  bp.part_number,
  bp.total_parts,
  bp.message_id,
  bp.segment_bytes,
  bp.file_name,
  ah.date_utc AS posted_at,
  COALESCE(ah.article_number, 0) AS article_number,
  COALESCE(
    (SELECT jsonb_agg(DISTINCT cg.observed_group_name ORDER BY cg.observed_group_name)
       FROM article_header_crosspost_groups cg
      WHERE cg.source_posted_at = bp.source_posted_at
        AND cg.article_header_id = bp.article_header_id),
    '[]'::jsonb
  ) AS observed_groups,
  'local'::text AS part_source
FROM binary_parts bp
LEFT JOIN article_headers ah
  ON ah.source_posted_at = bp.source_posted_at
 AND ah.id = bp.article_header_id
UNION ALL
SELECT
  ps.binary_id,
  ps.source_posted_at,
  ps.part_number,
  ps.total_parts,
  ps.message_id,
  ps.segment_bytes,
  ps.file_name,
  ps.posted_at,
  0::bigint AS article_number,
  ps.observed_groups,
  'peer'::text AS part_source
FROM binary_peer_segments ps
WHERE ps.acceptance_state = 'accepted'
  AND NOT EXISTS (
    SELECT 1
    FROM binary_parts local
    WHERE local.binary_id = ps.binary_id
      AND local.part_number = ps.part_number
  );
