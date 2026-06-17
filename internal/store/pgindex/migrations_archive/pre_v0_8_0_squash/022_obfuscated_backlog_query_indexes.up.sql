CREATE INDEX IF NOT EXISTS idx_binaries_yenc_recovery_backlog
    ON public.binaries (provider_id, newsgroup_id, release_family_key, updated_at DESC, id)
    WHERE is_main_payload = true
      AND COALESCE(recovered_source, '') <> 'yenc_header'
      AND family_kind IN ('contextual_obfuscated', 'numeric_obfuscated_set', 'opaque_set');

CREATE INDEX IF NOT EXISTS idx_article_header_ingest_payloads_yenc_recovery_ready
    ON public.article_header_ingest_payloads (article_header_id, yenc_recovery_retry_after)
    WHERE subject_file_name = '';

CREATE INDEX IF NOT EXISTS idx_binary_parts_binary_part_article
    ON public.binary_parts (binary_id, part_number, id)
    INCLUDE (article_header_id);

CREATE INDEX IF NOT EXISTS idx_binaries_par2_inspection_backlog
    ON public.binaries (updated_at DESC, id)
    WHERE observed_parts > 0
      AND (
        lower(COALESCE(NULLIF(file_name, ''), NULLIF(binary_name, ''), '')) LIKE '%.par2'
        OR COALESCE(recovered_kind, '') = 'par2'
        OR COALESCE(recovered_extension, '') = '.par2'
      );
