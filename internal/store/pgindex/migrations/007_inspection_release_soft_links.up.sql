ALTER TABLE IF EXISTS public.binary_inspections
    DROP CONSTRAINT IF EXISTS binary_inspections_release_id_fkey;

ALTER TABLE IF EXISTS public.binary_inspection_artifacts
    DROP CONSTRAINT IF EXISTS binary_inspection_artifacts_release_id_fkey;

ALTER TABLE IF EXISTS public.binary_archive_entries
    DROP CONSTRAINT IF EXISTS binary_archive_entries_release_id_fkey;

ALTER TABLE IF EXISTS public.binary_media_streams
    DROP CONSTRAINT IF EXISTS binary_media_streams_release_id_fkey;

ALTER TABLE IF EXISTS public.binary_par2_sets
    DROP CONSTRAINT IF EXISTS binary_par2_sets_release_id_fkey;

ALTER TABLE IF EXISTS public.binary_text_evidence
    DROP CONSTRAINT IF EXISTS binary_text_evidence_release_id_fkey;
