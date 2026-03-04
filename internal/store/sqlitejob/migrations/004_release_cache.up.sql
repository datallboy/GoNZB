CREATE TABLE IF NOT EXISTS release_cache (
    release_id TEXT PRIMARY KEY,
    present BOOLEAN NOT NULL DEFAULT 0,
    blob_size INTEGER NOT NULL DEFAULT 0,
    blob_mtime_unix INTEGER NOT NULL DEFAULT 0,
    cached_at_unix INTEGER NOT NULL DEFAULT 0,
    verified_at_unix INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    FOREIGN KEY(release_id) REFERENCES releases(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_release_cache_present ON release_cache(present);
