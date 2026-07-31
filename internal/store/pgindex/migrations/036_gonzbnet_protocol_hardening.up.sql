-- The generic body_json GIN index is expensive for manifest-heavy event
-- ingestion and no maintained query uses it. Keep the pool_ids GIN index and
-- add narrow indexes for the JSON field and ordered scans that are used.
DROP INDEX IF EXISTS idx_federation_events_body_gin;

CREATE INDEX IF NOT EXISTS idx_federation_events_outbox
  ON federation_events (created_at ASC, event_id ASC)
  WHERE validation_status = 'accepted'
    AND visibility <> 'local';

CREATE INDEX IF NOT EXISTS idx_federation_events_accepted_received
  ON federation_events (received_at DESC, author_node_id)
  WHERE validation_status = 'accepted';

CREATE INDEX IF NOT EXISTS idx_federation_events_revoked_subject
  ON federation_events ((body_json->>'subject_node_id'), created_at ASC, event_id ASC)
  WHERE validation_status = 'accepted'
    AND event_type = 'PoolMemberRevoked';

CREATE INDEX IF NOT EXISTS idx_health_attestations_pool_checked
  ON health_attestations (pool_id, checked_at DESC)
  INCLUDE (status, author_node_id);

CREATE INDEX IF NOT EXISTS idx_article_availability_pool_checked
  ON article_availability_attestations (pool_id, checked_at DESC)
  INCLUDE (status, author_node_id);

CREATE INDEX IF NOT EXISTS idx_federation_nodes_handshake_expiry
  ON federation_nodes (last_seen_at)
  WHERE status = 'handshaken';
