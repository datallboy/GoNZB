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
