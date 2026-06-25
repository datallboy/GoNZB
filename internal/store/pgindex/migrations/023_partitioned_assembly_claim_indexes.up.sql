CREATE INDEX IF NOT EXISTS idx_article_assembly_queue_recent_claimable
    ON public.article_header_assembly_queue (article_header_id DESC, source_posted_at)
    WHERE normalized_file_name <> '';

CREATE INDEX IF NOT EXISTS idx_article_assembly_queue_structured_lookup
    ON public.article_header_assembly_queue (
        provider_id,
        newsgroup_id,
        normalized_file_name,
        article_header_id DESC,
        source_posted_at
    )
    WHERE normalized_file_name <> '';

CREATE INDEX IF NOT EXISTS idx_article_assembly_queue_general_claimable
    ON public.article_header_assembly_queue (article_header_id DESC, source_posted_at, claim_until);
