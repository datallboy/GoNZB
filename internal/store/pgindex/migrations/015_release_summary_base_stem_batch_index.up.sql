CREATE INDEX IF NOT EXISTS idx_binary_identity_base_stem_summary
    ON public.binary_identity_current (
        provider_id,
        newsgroup_id,
        lower(btrim(base_stem))
    )
    INCLUDE (
        binary_id,
        source_release_key,
        release_key,
        release_name,
        family_kind,
        file_name,
        binary_name,
        match_confidence,
        expected_file_count,
        expected_archive_file_count,
        is_auxiliary,
        is_main_payload
    )
    WHERE btrim(base_stem) <> ''
      AND GREATEST(expected_file_count, expected_archive_file_count) > 1;
