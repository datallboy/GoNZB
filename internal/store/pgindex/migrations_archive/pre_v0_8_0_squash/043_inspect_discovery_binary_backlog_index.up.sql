CREATE INDEX IF NOT EXISTS idx_binaries_inspect_discovery_backlog
    ON public.binaries (updated_at DESC, id DESC)
    INCLUDE (
        provider_id,
        newsgroup_id,
        release_family_key,
        base_stem,
        file_name,
        binary_name,
        release_name,
        poster_id,
        posted_at,
        total_bytes,
        total_parts,
        match_confidence
    )
    WHERE COALESCE(recovered_extension, '') = ''
      AND (is_main_payload = TRUE OR is_auxiliary = FALSE)
      AND (
          LOWER(COALESCE(NULLIF(file_name, ''), NULLIF(binary_name, ''), '')) LIKE '%.bin'
          OR COALESCE(NULLIF(file_name, ''), NULLIF(binary_name, ''), '') !~ '\.[A-Za-z0-9]{1,8}$'
      );
