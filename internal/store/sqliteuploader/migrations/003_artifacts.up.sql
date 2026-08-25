CREATE TABLE uploader_artifacts (
  id TEXT PRIMARY KEY,
  submission_id TEXT NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('nfo', 'screenshot', 'sample', 'subtitle', 'metadata', 'other')),
  original_filename TEXT NOT NULL,
  label TEXT NOT NULL DEFAULT '',
  declared_media_type TEXT NOT NULL DEFAULT '',
  detected_media_type TEXT NOT NULL,
  size_bytes INTEGER NOT NULL,
  sha256 TEXT NOT NULL,
  display_order INTEGER NOT NULL,
  blob_key TEXT NOT NULL,
  created_at TEXT NOT NULL,
  FOREIGN KEY (submission_id) REFERENCES uploader_submissions(id) ON DELETE CASCADE,
  UNIQUE (submission_id, original_filename),
  UNIQUE (submission_id, sha256)
);

CREATE INDEX uploader_artifacts_submission_order_idx
ON uploader_artifacts(submission_id, display_order, id);
