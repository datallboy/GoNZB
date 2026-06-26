CREATE INDEX IF NOT EXISTS idx_binary_observation_stats_posted_cohort
    ON public.binary_observation_stats (provider_id, newsgroup_id, posted_at DESC, binary_id)
    WHERE total_parts <= 1 AND observed_parts <= 1;

CREATE INDEX IF NOT EXISTS idx_binary_observation_stats_incomplete_updated
    ON public.binary_observation_stats (updated_at DESC, binary_id)
    WHERE total_parts > 0 AND observed_parts < total_parts;

CREATE INDEX IF NOT EXISTS idx_binary_identity_opaque_subject_cohort
    ON public.binary_identity_current (provider_id, newsgroup_id, identity_reason, family_kind, identity_strength, binary_id)
    WHERE family_kind = 'opaque_set'
      AND identity_reason = 'opaque_subject_set'
      AND is_main_payload = TRUE;
