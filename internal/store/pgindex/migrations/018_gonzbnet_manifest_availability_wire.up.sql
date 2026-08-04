ALTER TABLE manifest_availability_attestations
  ADD COLUMN IF NOT EXISTS source_node_id TEXT,
  ADD COLUMN IF NOT EXISTS available BOOLEAN,
  ADD COLUMN IF NOT EXISTS fetch_policy TEXT,
  ADD COLUMN IF NOT EXISTS compressed_size_bytes BIGINT,
  ADD COLUMN IF NOT EXISTS wire_updated_at TIMESTAMPTZ;

UPDATE manifest_availability_attestations
SET source_node_id = COALESCE(source_node_id, author_node_id),
    available = COALESCE(available, status = 'available'),
    fetch_policy = COALESCE(NULLIF(fetch_policy, ''), 'trusted_peers_only'),
    compressed_size_bytes = COALESCE(compressed_size_bytes, 0),
    wire_updated_at = COALESCE(wire_updated_at, checked_at)
WHERE source_node_id IS NULL
   OR available IS NULL
   OR fetch_policy IS NULL
   OR compressed_size_bytes IS NULL
   OR wire_updated_at IS NULL;

ALTER TABLE manifest_availability_attestations
  ALTER COLUMN source_node_id SET NOT NULL,
  ALTER COLUMN available SET NOT NULL,
  ALTER COLUMN fetch_policy SET NOT NULL,
  ALTER COLUMN compressed_size_bytes SET NOT NULL,
  ALTER COLUMN wire_updated_at SET NOT NULL;
