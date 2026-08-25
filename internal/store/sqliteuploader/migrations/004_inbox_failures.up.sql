CREATE TABLE uploader_inbox_failures (
  path_fingerprint TEXT PRIMARY KEY,
  observed_mod_time TEXT NOT NULL,
  observed_size INTEGER NOT NULL,
  error_code TEXT NOT NULL,
  safe_message TEXT NOT NULL,
  next_attempt_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
