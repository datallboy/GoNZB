CREATE INDEX IF NOT EXISTS idx_binary_observation_incomplete_rank
    ON public.binary_observation_stats (
        observed_parts DESC,
        binary_id DESC
    )
    INCLUDE (provider_id, newsgroup_id, total_parts)
    WHERE total_parts > 0
      AND observed_parts < total_parts;
