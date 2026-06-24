CREATE INDEX IF NOT EXISTS idx_binary_completion_keys_match_rank
    ON public.binary_completion_keys (
        provider_id,
        newsgroup_id,
        normalized_file_name,
        is_main_payload DESC,
        completion_ratio DESC,
        observed_parts DESC,
        binary_id DESC
    );

CREATE INDEX IF NOT EXISTS idx_article_assembly_queue_structured_latest
    ON public.article_header_assembly_queue (
        provider_id,
        newsgroup_id,
        normalized_file_name,
        article_header_id DESC
    )
    INCLUDE (claim_until)
    WHERE normalized_file_name <> ''::text;
