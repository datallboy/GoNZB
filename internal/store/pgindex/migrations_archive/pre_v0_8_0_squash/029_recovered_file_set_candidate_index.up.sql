CREATE INDEX IF NOT EXISTS idx_binaries_recovered_file_set_candidates
    ON public.binaries (provider_id, file_set_key, newsgroup_id)
    INCLUDE (
        posted_at,
        updated_at,
        is_main_payload,
        is_auxiliary,
        expected_file_count,
        expected_archive_file_count,
        total_parts,
        observed_parts,
        total_bytes,
        source_release_key,
        release_name,
        release_family_key,
        base_stem
    )
    WHERE COALESCE(recovered_source, '') = 'yenc_header'
      AND btrim(file_set_key) <> ''
      AND posted_at IS NOT NULL;
