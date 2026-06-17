CREATE INDEX IF NOT EXISTS idx_binary_identity_base_stem_file_set_refresh
    ON public.binary_identity_current (
        provider_id,
        newsgroup_id,
        lower(btrim(base_stem))
    )
    INCLUDE (
        binary_id,
        file_set_key,
        expected_file_count,
        expected_archive_file_count
    )
    WHERE btrim(base_stem) <> ''
      AND btrim(file_set_key) <> ''
      AND GREATEST(expected_file_count, expected_archive_file_count) > 1;
