CREATE INDEX IF NOT EXISTS idx_release_family_yenc_recovery_candidates
    ON public.release_family_readiness_summaries (provider_id, newsgroup_id, family_key)
    WHERE key_kind = 'release_family'
      AND readiness_bucket IN ('overgrouped_contextual', 'weak_single_binary', 'weak_obfuscated_set');
