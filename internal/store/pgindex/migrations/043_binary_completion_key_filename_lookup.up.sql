CREATE INDEX IF NOT EXISTS idx_binary_completion_keys_filename_lookup
    ON public.binary_completion_keys (
        provider_id,
        newsgroup_id,
        normalized_file_name,
        source_posted_at,
        binary_id
    )
    INCLUDE (
        is_main_payload,
        observed_parts,
        completion_ratio,
        posted_at
    );
