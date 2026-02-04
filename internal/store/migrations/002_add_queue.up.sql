CREATE TABLE IF NOT EXISTS queue_items (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    password TEXT,
    status TEXT NOT NULL,
    total_bytes INTEGER DEFAULT 0,
    bytes_written INTEGER DEFAULT 0,
    tasks TEXT NOT NULL, -- Stores JSON blob of []*nzb.DownloadFile
    error TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Trigger to automatically update the updated_at timestamp
CREATE TRIGGER IF NOT EXISTS update_queue_item_timestamp 
AFTER UPDATE ON queue_items
BEGIN
    UPDATE queue_items SET updated_at = CURRENT_TIMESTAMP WHERE id = OLD.id;
END;