DELETE FROM public.release_family_summary_refresh_queue
WHERE key_kind = 'base_stem'
  AND family_key ~* '^[a-z0-9]{12,}$';

DELETE FROM public.release_ready_candidate_acks
WHERE key_kind = 'base_stem'
  AND family_key ~* '^[a-z0-9]{12,}$';

DELETE FROM public.release_ready_candidates
WHERE key_kind = 'base_stem'
  AND family_key ~* '^[a-z0-9]{12,}$';

DELETE FROM public.release_family_readiness_summaries
WHERE key_kind = 'base_stem'
  AND family_key ~* '^[a-z0-9]{12,}$';
