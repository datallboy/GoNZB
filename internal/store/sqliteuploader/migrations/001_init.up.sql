CREATE TABLE uploader_submissions (
  id TEXT PRIMARY KEY,
  state TEXT NOT NULL CHECK (state IN ('pending_review', 'approved', 'rejected')),
  release_id TEXT NOT NULL UNIQUE,
  title TEXT NOT NULL,
  normalized_title TEXT NOT NULL,
  category_id INTEGER NOT NULL,
  category TEXT NOT NULL,
  size_bytes INTEGER NOT NULL,
  posted_at TEXT NOT NULL,
  poster TEXT NOT NULL DEFAULT '',
  groups_json TEXT NOT NULL DEFAULT '[]',
  file_count INTEGER NOT NULL,
  segment_count INTEGER NOT NULL,
  password TEXT NOT NULL DEFAULT '',
  has_password BOOLEAN NOT NULL DEFAULT 0,
  has_par2 BOOLEAN NOT NULL DEFAULT 0,
  has_nfo BOOLEAN NOT NULL DEFAULT 0,
  obfuscated_subjects BOOLEAN NOT NULL DEFAULT 0,
  encrypted_names BOOLEAN NOT NULL DEFAULT 0,
  imdb_id TEXT NOT NULL DEFAULT '',
  tmdb_id INTEGER NOT NULL DEFAULT 0,
  tvdb_id INTEGER NOT NULL DEFAULT 0,
  year INTEGER NOT NULL DEFAULT 0,
  resolution TEXT NOT NULL DEFAULT '',
  media_source TEXT NOT NULL DEFAULT '',
  video_codec TEXT NOT NULL DEFAULT '',
  audio_codec TEXT NOT NULL DEFAULT '',
  nzb_sha256 TEXT NOT NULL UNIQUE,
  nzb_blob_key TEXT NOT NULL,
  idempotency_key TEXT NOT NULL DEFAULT '',
  intake_kind TEXT NOT NULL,
  provenance_tool TEXT NOT NULL DEFAULT '',
  provenance_version TEXT NOT NULL DEFAULT '',
  provenance_external_id TEXT NOT NULL DEFAULT '',
  original_filename TEXT NOT NULL DEFAULT '',
  submitted_by TEXT NOT NULL DEFAULT '',
  reviewer TEXT NOT NULL DEFAULT '',
  review_note TEXT NOT NULL DEFAULT '',
  files_json TEXT NOT NULL DEFAULT '[]',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  reviewed_at TEXT,
  approved_at TEXT,
  rejected_at TEXT
);

CREATE UNIQUE INDEX uploader_submissions_idempotency_key_unique
ON uploader_submissions(idempotency_key)
WHERE idempotency_key <> '';

CREATE UNIQUE INDEX uploader_submissions_provenance_external_unique
ON uploader_submissions(provenance_tool, provenance_external_id)
WHERE provenance_external_id <> '';

CREATE INDEX uploader_submissions_state_posted_idx
ON uploader_submissions(state, posted_at DESC, id);

CREATE INDEX uploader_submissions_normalized_title_idx
ON uploader_submissions(normalized_title);

CREATE TABLE uploader_submission_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  submission_id TEXT NOT NULL,
  event_type TEXT NOT NULL,
  actor TEXT NOT NULL DEFAULT '',
  prior_state TEXT NOT NULL DEFAULT '',
  next_state TEXT NOT NULL DEFAULT '',
  note TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  FOREIGN KEY (submission_id) REFERENCES uploader_submissions(id) ON DELETE CASCADE
);

CREATE INDEX uploader_submission_events_submission_idx
ON uploader_submission_events(submission_id, id);
