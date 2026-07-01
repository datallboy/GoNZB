CREATE INDEX IF NOT EXISTS idx_release_recovered_file_set_candidates_lookup
    ON public.release_recovered_file_set_candidates (
        provider_id,
        file_set_key,
        source_posted_at
    );

CREATE INDEX IF NOT EXISTS idx_release_ready_candidates_lookup
    ON public.release_ready_candidates (
        provider_id,
        key_kind,
        family_key,
        source_posted_at,
        newsgroup_id
    );
