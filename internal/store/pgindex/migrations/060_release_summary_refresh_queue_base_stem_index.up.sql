CREATE INDEX IF NOT EXISTS idx_release_family_summary_refresh_queue_base_stem
    ON release_family_summary_refresh_queue (queued_at, provider_id, newsgroup_id, family_key)
    WHERE key_kind = 'base_stem';
