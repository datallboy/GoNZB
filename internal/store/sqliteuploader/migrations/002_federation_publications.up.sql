CREATE TABLE uploader_federation_publications (
  submission_id TEXT NOT NULL,
  pool_id TEXT NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('requested', 'published', 'withdrawal_requested', 'withdrawn', 'failed')),
  release_id TEXT NOT NULL DEFAULT '',
  manifest_id TEXT NOT NULL DEFAULT '',
  card_event_id TEXT NOT NULL DEFAULT '',
  manifest_event_id TEXT NOT NULL DEFAULT '',
  publication_state_event_id TEXT NOT NULL DEFAULT '',
  attempt_count INTEGER NOT NULL DEFAULT 0,
  last_error TEXT NOT NULL DEFAULT '',
  next_attempt_at TEXT,
  requested_by TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (submission_id, pool_id),
  FOREIGN KEY (submission_id) REFERENCES uploader_submissions(id) ON DELETE CASCADE
);

CREATE INDEX uploader_federation_publications_due_idx
ON uploader_federation_publications(state, next_attempt_at);
