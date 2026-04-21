CREATE INDEX IF NOT EXISTS idx_binaries_base_stem_expected_family
ON binaries(provider_id, newsgroup_id, expected_file_count, lower(btrim(base_stem)))
WHERE btrim(base_stem) <> '';
