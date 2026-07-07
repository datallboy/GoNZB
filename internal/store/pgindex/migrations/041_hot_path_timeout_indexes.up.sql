CREATE INDEX IF NOT EXISTS idx_binary_identity_subject_multipart_stale
    ON public.binary_identity_current (source_posted_at DESC, binary_id)
    WHERE identity_reason = 'subject_multipart_obfuscated';

CREATE INDEX IF NOT EXISTS idx_binary_identity_subject_regroup_candidates
    ON public.binary_identity_current (source_posted_at DESC, binary_id DESC)
    INCLUDE (
        provider_id,
        newsgroup_id,
        release_family_key,
        base_stem,
        source_release_key,
        file_name,
        expected_file_count
    )
    WHERE family_kind = 'contextual_obfuscated'
      AND identity_reason = 'contextual_fallback'
      AND btrim(COALESCE(file_name, '')) <> '';

CREATE INDEX IF NOT EXISTS idx_poster_materialization_queue_ready_partition
    ON public.poster_materialization_queue (status, ready_at, source_posted_at, article_header_id)
    WHERE status IN ('pending', 'failed') AND btrim(COALESCE(poster_key, '')) <> '';
