CREATE TABLE IF NOT EXISTS federation_event_chain_issues (
  id BIGSERIAL PRIMARY KEY,
  author_node_id TEXT NOT NULL,
  event_id TEXT NOT NULL,
  issue_type TEXT NOT NULL CHECK (issue_type IN ('sequence_gap', 'fork')),
  conflicting_event_id TEXT,
  expected_sequence BIGINT,
  observed_sequence BIGINT NOT NULL,
  expected_previous_event_id TEXT,
  observed_previous_event_id TEXT,
  details TEXT NOT NULL,
  raw_event_json TEXT NOT NULL,
  detected_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  resolved_at TIMESTAMPTZ,
  UNIQUE(author_node_id, event_id, issue_type)
);

CREATE INDEX IF NOT EXISTS idx_federation_event_chain_issues_open
  ON federation_event_chain_issues(author_node_id, issue_type, detected_at DESC)
  WHERE resolved_at IS NULL;
