CREATE INDEX IF NOT EXISTS idx_release_family_readiness_key_bucket_lookup
    ON public.release_family_readiness_summaries (
        provider_id,
        newsgroup_id,
        key_kind,
        family_key,
        readiness_bucket,
        source_posted_at
    );
