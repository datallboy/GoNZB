CREATE INDEX IF NOT EXISTS idx_binary_identity_release_family_lookup
    ON public.binary_identity_current (provider_id, newsgroup_id, release_family_key, source_posted_at, binary_id)
    WHERE BTRIM(release_family_key) <> '';

CREATE INDEX IF NOT EXISTS idx_binary_identity_base_stem_lookup
    ON public.binary_identity_current (provider_id, newsgroup_id, LOWER(BTRIM(base_stem)), source_posted_at, binary_id)
    WHERE BTRIM(base_stem) <> ''
      AND GREATEST(expected_file_count, expected_archive_file_count) > 1;

CREATE INDEX IF NOT EXISTS idx_binary_identity_file_set_lookup
    ON public.binary_identity_current (provider_id, file_set_key, source_posted_at, binary_id)
    WHERE BTRIM(file_set_key) <> '';

CREATE INDEX IF NOT EXISTS idx_binary_lifecycle_status_lookup
    ON public.binary_lifecycle (source_posted_at, binary_id, lifecycle_status);
