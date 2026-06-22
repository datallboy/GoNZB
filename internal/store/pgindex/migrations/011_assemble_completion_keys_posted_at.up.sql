ALTER TABLE public.binary_completion_keys
    ADD COLUMN IF NOT EXISTS posted_at timestamp with time zone;

DROP INDEX IF EXISTS public.idx_binary_completion_keys_match_rank;

CREATE INDEX IF NOT EXISTS idx_binary_completion_keys_match_rank
    ON public.binary_completion_keys (
        provider_id,
        newsgroup_id,
        normalized_file_name,
        is_main_payload DESC,
        completion_ratio DESC,
        observed_parts DESC,
        binary_id DESC
    )
    INCLUDE (posted_at);
