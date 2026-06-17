CREATE INDEX IF NOT EXISTS idx_binary_identity_inspect_discovery_backlog
    ON public.binary_identity_current (updated_at DESC, binary_id DESC)
    INCLUDE (
        release_family_key,
        base_stem,
        release_name,
        binary_name,
        file_name,
        file_index,
        expected_file_count,
        expected_archive_file_count,
        is_auxiliary,
        is_main_payload,
        match_confidence,
        match_status
    )
    WHERE (is_main_payload = TRUE OR is_auxiliary = FALSE)
      AND (
          LOWER(COALESCE(NULLIF(file_name, ''), NULLIF(binary_name, ''), '')) LIKE '%.bin'
          OR COALESCE(NULLIF(file_name, ''), NULLIF(binary_name, ''), '') !~ '\.[A-Za-z0-9]{1,8}$'
      );
