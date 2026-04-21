CREATE TABLE IF NOT EXISTS predb_backfill_checkpoints (
    provider TEXT PRIMARY KEY,
    offset_hint INTEGER NOT NULL DEFAULT 0,
    oldest_posted_at TIMESTAMPTZ NULL,
    oldest_normalized_title TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_predb_backfill_checkpoints_updated_at
    ON predb_backfill_checkpoints (updated_at DESC);
