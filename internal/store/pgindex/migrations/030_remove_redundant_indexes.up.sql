-- These indexes duplicate primary-key or unique-constraint indexes exactly.
-- Keeping both copies doubles index maintenance without providing another
-- access path. Dropping a partitioned parent index also removes its attached
-- child indexes.
DROP INDEX IF EXISTS public.idx_binary_grouping_evidence_source_posted;
DROP INDEX IF EXISTS public.idx_federation_events_author_sequence;
DROP INDEX IF EXISTS public.idx_release_archive_detail_subtitle_release;
DROP INDEX IF EXISTS public.idx_release_family_readiness_source_posted;
DROP INDEX IF EXISTS public.idx_release_ready_candidates_source_posted;
DROP INDEX IF EXISTS public.idx_release_recovered_file_set_candidates_source_posted;
DROP INDEX IF EXISTS public.idx_release_stage_dirty_families_source_posted;
DROP INDEX IF EXISTS public.idx_scrape_checkpoints_provider_newsgroup;
