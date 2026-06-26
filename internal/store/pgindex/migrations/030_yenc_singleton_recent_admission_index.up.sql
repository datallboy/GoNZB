CREATE INDEX IF NOT EXISTS idx_binary_observation_stats_singleton_updated
    ON public.binary_observation_stats (updated_at DESC, binary_id DESC)
    INCLUDE (source_posted_at, provider_id, newsgroup_id, posted_at, total_bytes)
    WHERE total_parts <= 1
      AND observed_parts <= 1
      AND posted_at IS NOT NULL;
