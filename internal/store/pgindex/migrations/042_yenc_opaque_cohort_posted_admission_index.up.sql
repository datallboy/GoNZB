CREATE INDEX IF NOT EXISTS idx_binary_observation_stats_opaque_posted_admission
    ON public.binary_observation_stats (posted_at DESC, source_posted_at, provider_id, newsgroup_id, binary_id)
    INCLUDE (total_bytes, updated_at)
    WHERE total_parts <= 1
      AND observed_parts <= 1
      AND posted_at IS NOT NULL;
