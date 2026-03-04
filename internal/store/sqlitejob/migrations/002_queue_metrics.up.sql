ALTER TABLE queue_items ADD COLUMN started_at_unix INTEGER NOT NULL DEFAULT 0;
ALTER TABLE queue_items ADD COLUMN completed_at_unix INTEGER NOT NULL DEFAULT 0;
ALTER TABLE queue_items ADD COLUMN download_seconds INTEGER NOT NULL DEFAULT 0;
ALTER TABLE queue_items ADD COLUMN postprocess_seconds INTEGER NOT NULL DEFAULT 0;
ALTER TABLE queue_items ADD COLUMN avg_bps INTEGER NOT NULL DEFAULT 0;
ALTER TABLE queue_items ADD COLUMN downloaded_bytes INTEGER NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_queue_items_completed_at_unix ON queue_items(completed_at_unix);

-- Backfill existing rows with best-effort values for history views.
UPDATE queue_items
SET started_at_unix = CAST(strftime('%s', created_at) AS INTEGER)
WHERE started_at_unix = 0;

UPDATE queue_items
SET completed_at_unix = CAST(strftime('%s', updated_at) AS INTEGER)
WHERE status IN ('completed', 'failed') AND completed_at_unix = 0;

UPDATE queue_items
SET downloaded_bytes = (
  SELECT COALESCE(r.size, 0)
  FROM releases r
  WHERE r.id = queue_items.release_id
)
WHERE status = 'completed' AND downloaded_bytes = 0;
