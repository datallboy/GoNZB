ALTER TABLE public.binaries
    ADD COLUMN IF NOT EXISTS grouping_summary_kind TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS grouping_summary_status TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS grouping_summary_fallback_used BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS par2_target_base_stem TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS par2_target_file_name TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS par2_source_binary_id BIGINT,
    ADD COLUMN IF NOT EXISTS par2_target_coverage_source TEXT NOT NULL DEFAULT '';

UPDATE public.binaries
SET grouping_summary_kind = COALESCE(grouping_evidence_json->'summary'->>'kind', ''),
    grouping_summary_status = COALESCE(grouping_evidence_json->'summary'->>'status', ''),
    grouping_summary_fallback_used = COALESCE((grouping_evidence_json->'summary'->>'fallback_used')::boolean, FALSE),
    par2_target_base_stem = COALESCE(grouping_evidence_json->>'par2_target_base_stem', ''),
    par2_target_file_name = COALESCE(grouping_evidence_json->>'par2_target_file_name', ''),
    par2_source_binary_id = CASE
        WHEN COALESCE(grouping_evidence_json->>'par2_source_binary_id', '') ~ '^[0-9]+$'
        THEN (grouping_evidence_json->>'par2_source_binary_id')::bigint
        ELSE par2_source_binary_id
    END,
    par2_target_coverage_source = COALESCE(grouping_evidence_json->>'par2_target_coverage_source', '')
WHERE grouping_evidence_json <> '{}'::jsonb;

UPDATE public.binaries
SET grouping_evidence_json = '{}'::jsonb
WHERE grouping_evidence_json <> '{}'::jsonb;
