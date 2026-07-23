CREATE TABLE IF NOT EXISTS gonzbnet_scan_output_publications (
  scan_id TEXT NOT NULL REFERENCES gonzbnet_scan_outputs(scan_id) ON DELETE CASCADE,
  pool_id TEXT NOT NULL,
  event_id TEXT NOT NULL REFERENCES federation_events(event_id),
  published_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (scan_id, pool_id)
);

CREATE INDEX IF NOT EXISTS idx_gonzbnet_scan_output_publications_pool
  ON gonzbnet_scan_output_publications(pool_id, published_at DESC);
