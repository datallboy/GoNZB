-- Milestone 3 baseline: downloader runtime + optional cache tables only.
-- No SQLite release catalog ownership.

PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS queue_items (
  id TEXT PRIMARY KEY,                        -- KSUID
  status TEXT NOT NULL,
  out_dir TEXT NOT NULL,
  error TEXT,
  source_kind TEXT NOT NULL CHECK (source_kind IN ('manual','aggregator','usenet_index')),
  source_release_id TEXT,                     -- nullable for manual direct upload
  release_title TEXT NOT NULL DEFAULT '',
  release_size INTEGER NOT NULL DEFAULT 0,
  release_snapshot_json TEXT NOT NULL DEFAULT '{}',
  payload_mode TEXT NOT NULL DEFAULT 'cached' CHECK (payload_mode IN ('cached','ephemeral')),
  resumable BOOLEAN NOT NULL DEFAULT 1,

  -- CHANGED: queue item points to deduped file set
  file_set_id TEXT,

  started_at_unix INTEGER NOT NULL DEFAULT 0,
  completed_at_unix INTEGER NOT NULL DEFAULT 0,
  download_seconds INTEGER NOT NULL DEFAULT 0,
  postprocess_seconds INTEGER NOT NULL DEFAULT 0,
  avg_bps INTEGER NOT NULL DEFAULT 0,
  downloaded_bytes INTEGER NOT NULL DEFAULT 0,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_queue_items_status ON queue_items(status);
CREATE INDEX IF NOT EXISTS idx_queue_items_completed_at_unix ON queue_items(completed_at_unix);
CREATE INDEX IF NOT EXISTS idx_queue_items_source_kind ON queue_items(source_kind);
CREATE INDEX IF NOT EXISTS idx_queue_items_source_release_id ON queue_items(source_release_id);
CREATE INDEX IF NOT EXISTS idx_queue_items_payload_mode ON queue_items(payload_mode);
CREATE INDEX IF NOT EXISTS idx_queue_items_resumable ON queue_items(resumable);
CREATE INDEX IF NOT EXISTS idx_queue_items_file_set_id ON queue_items(file_set_id);

CREATE TRIGGER IF NOT EXISTS trg_queue_items_updated_at
AFTER UPDATE ON queue_items
BEGIN
  UPDATE queue_items SET updated_at = CURRENT_TIMESTAMP WHERE id = OLD.id;
END;

CREATE TABLE IF NOT EXISTS queue_item_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  queue_item_id TEXT NOT NULL,
  stage TEXT NOT NULL,
  status TEXT NOT NULL,
  message TEXT NOT NULL DEFAULT '',
  meta_json TEXT NOT NULL DEFAULT '',
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY(queue_item_id) REFERENCES queue_items(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_queue_item_events_queue_item_id_created_at
ON queue_item_events(queue_item_id, created_at);

-- CHANGED: deduplicated queue item files
CREATE TABLE IF NOT EXISTS queue_file_sets (
  id TEXT PRIMARY KEY,                        -- KSUID
  content_hash TEXT NOT NULL UNIQUE,          -- sha256 of normalized file list payload
  total_files INTEGER NOT NULL DEFAULT 0,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_queue_file_sets_content_hash ON queue_file_sets(content_hash);

CREATE TABLE IF NOT EXISTS queue_file_set_items (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  file_set_id TEXT NOT NULL,
  file_name TEXT NOT NULL,
  size INTEGER NOT NULL DEFAULT 0,
  file_index INTEGER NOT NULL DEFAULT 0,
  is_pars BOOLEAN NOT NULL DEFAULT 0,
  subject TEXT NOT NULL DEFAULT '',
  date_unix INTEGER NOT NULL DEFAULT 0,
  poster TEXT NOT NULL DEFAULT '',
  groups_json TEXT NOT NULL DEFAULT '[]',
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY(file_set_id) REFERENCES queue_file_sets(id) ON DELETE CASCADE,
  UNIQUE(file_set_id, file_index)
);

CREATE INDEX IF NOT EXISTS idx_queue_file_set_items_file_set_id
ON queue_file_set_items(file_set_id);

-- Optional payload cache metadata table (module can be disabled at runtime).
CREATE TABLE IF NOT EXISTS blob_cache_index (
  key TEXT PRIMARY KEY,
  size_bytes INTEGER NOT NULL DEFAULT 0,
  mtime_unix INTEGER NOT NULL DEFAULT 0,
  last_verified_unix INTEGER NOT NULL DEFAULT 0,
  last_error TEXT NOT NULL DEFAULT ''
);

-- Optional lightweight aggregator cache table (no PG dependency).
CREATE TABLE IF NOT EXISTS aggregator_release_cache (
  release_id TEXT PRIMARY KEY,
  title TEXT NOT NULL DEFAULT '',
  size_bytes INTEGER NOT NULL DEFAULT 0,
  source TEXT NOT NULL DEFAULT '',
  category TEXT NOT NULL DEFAULT '',
  guid TEXT NOT NULL DEFAULT '',
  publish_date_unix INTEGER NOT NULL DEFAULT 0,
  nzb_cached BOOLEAN NOT NULL DEFAULT 0,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_aggregator_release_cache_title
ON aggregator_release_cache(title);
CREATE INDEX IF NOT EXISTS idx_aggregator_release_cache_source
ON aggregator_release_cache(source);

CREATE TABLE IF NOT EXISTS module_schema_version (
  module_name TEXT PRIMARY KEY,
  version INTEGER NOT NULL,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO module_schema_version (module_name, version)
VALUES ('sqlitejob', 1)
ON CONFLICT(module_name) DO UPDATE SET
  version = excluded.version,
  updated_at = CURRENT_TIMESTAMP;
