CREATE INDEX IF NOT EXISTS idx_article_header_ingest_payloads_structured_name
ON public.article_header_ingest_payloads (lower(btrim(subject_file_name)), article_header_id)
WHERE btrim(subject_file_name) <> '';

CREATE INDEX IF NOT EXISTS idx_binaries_normalized_file_identity
ON public.binaries (
  provider_id,
  newsgroup_id,
  lower(btrim(COALESCE(NULLIF(file_name, ''), NULLIF(binary_name, ''))))
)
WHERE btrim(COALESCE(NULLIF(file_name, ''), NULLIF(binary_name, ''))) <> '';

CREATE INDEX IF NOT EXISTS idx_binaries_base_stem_family_lookup
ON public.binaries (
  provider_id,
  newsgroup_id,
  lower(btrim(base_stem))
)
WHERE expected_file_count > 1 AND btrim(base_stem) <> '';
