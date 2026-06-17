CREATE SEQUENCE IF NOT EXISTS public.binary_core_binary_id_seq;

ALTER TABLE public.binary_core
    ALTER COLUMN binary_id SET DEFAULT nextval('public.binary_core_binary_id_seq');

ALTER SEQUENCE public.binary_core_binary_id_seq
    OWNED BY public.binary_core.binary_id;

SELECT setval(
    'public.binary_core_binary_id_seq',
    GREATEST(COALESCE((SELECT MAX(binary_id) FROM public.binary_core), 0), 1),
    COALESCE((SELECT MAX(binary_id) FROM public.binary_core), 0) > 0
);

ALTER TABLE IF EXISTS public.binary_core
    DROP CONSTRAINT IF EXISTS binary_core_binary_id_fkey;

ALTER TABLE IF EXISTS public.binary_observation_stats
    DROP CONSTRAINT IF EXISTS binary_observation_stats_binary_id_fkey,
    ADD CONSTRAINT binary_observation_stats_binary_id_fkey
        FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;

ALTER TABLE IF EXISTS public.binary_identity_current
    DROP CONSTRAINT IF EXISTS binary_identity_current_binary_id_fkey,
    ADD CONSTRAINT binary_identity_current_binary_id_fkey
        FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;

ALTER TABLE IF EXISTS public.binary_recovery_current
    DROP CONSTRAINT IF EXISTS binary_recovery_current_binary_id_fkey,
    ADD CONSTRAINT binary_recovery_current_binary_id_fkey
        FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;

ALTER TABLE IF EXISTS public.binary_lifecycle
    DROP CONSTRAINT IF EXISTS binary_lifecycle_binary_id_fkey,
    ADD CONSTRAINT binary_lifecycle_binary_id_fkey
        FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;

ALTER TABLE IF EXISTS public.binary_projection_events
    DROP CONSTRAINT IF EXISTS binary_projection_events_binary_id_fkey,
    ADD CONSTRAINT binary_projection_events_binary_id_fkey
        FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;

ALTER TABLE IF EXISTS public.binary_parts
    DROP CONSTRAINT IF EXISTS binary_parts_binary_id_fkey,
    ADD CONSTRAINT binary_parts_binary_id_fkey
        FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;

ALTER TABLE IF EXISTS public.yenc_recovery_work_items
    DROP CONSTRAINT IF EXISTS yenc_recovery_work_items_binary_id_fkey,
    ADD CONSTRAINT yenc_recovery_work_items_binary_id_fkey
        FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;

ALTER TABLE IF EXISTS public.binary_par2_targets
    DROP CONSTRAINT IF EXISTS binary_par2_targets_binary_id_fkey,
    ADD CONSTRAINT binary_par2_targets_binary_id_fkey
        FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;

ALTER TABLE IF EXISTS public.binary_archive_entries
    DROP CONSTRAINT IF EXISTS binary_archive_entries_binary_id_fkey,
    ADD CONSTRAINT binary_archive_entries_binary_id_fkey
        FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;

ALTER TABLE IF EXISTS public.binary_grouping_evidence
    DROP CONSTRAINT IF EXISTS binary_grouping_evidence_binary_id_fkey,
    ADD CONSTRAINT binary_grouping_evidence_binary_id_fkey
        FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;

ALTER TABLE IF EXISTS public.binary_inspection_artifacts
    DROP CONSTRAINT IF EXISTS binary_inspection_artifacts_binary_id_fkey,
    ADD CONSTRAINT binary_inspection_artifacts_binary_id_fkey
        FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;

ALTER TABLE IF EXISTS public.binary_inspections
    DROP CONSTRAINT IF EXISTS binary_inspections_binary_id_fkey,
    ADD CONSTRAINT binary_inspections_binary_id_fkey
        FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;

ALTER TABLE IF EXISTS public.binary_media_streams
    DROP CONSTRAINT IF EXISTS binary_media_streams_binary_id_fkey,
    ADD CONSTRAINT binary_media_streams_binary_id_fkey
        FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;

ALTER TABLE IF EXISTS public.binary_par2_sets
    DROP CONSTRAINT IF EXISTS binary_par2_sets_binary_id_fkey,
    ADD CONSTRAINT binary_par2_sets_binary_id_fkey
        FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;

ALTER TABLE IF EXISTS public.binary_text_evidence
    DROP CONSTRAINT IF EXISTS binary_text_evidence_binary_id_fkey,
    ADD CONSTRAINT binary_text_evidence_binary_id_fkey
        FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;

ALTER TABLE IF EXISTS public.release_files
    DROP CONSTRAINT IF EXISTS release_files_binary_id_fkey,
    ADD CONSTRAINT release_files_binary_id_fkey
        FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE SET NULL;

ALTER TABLE IF EXISTS public.release_password_candidates
    DROP CONSTRAINT IF EXISTS release_password_candidates_binary_id_fkey,
    ADD CONSTRAINT release_password_candidates_binary_id_fkey
        FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE SET NULL;

INSERT INTO public.indexer_table_write_ownership (table_name, owner_stage, allowed_writer_stages, notes)
VALUES
    ('binary_core', 'assemble', ARRAY['assemble_lane_a', 'assemble_lane_b'], 'Canonical binary anchor. Phase C removes public.binaries as the FK root and ID source.')
ON CONFLICT (table_name) DO UPDATE
SET owner_stage = EXCLUDED.owner_stage,
    allowed_writer_stages = EXCLUDED.allowed_writer_stages,
    notes = EXCLUDED.notes,
    updated_at = now();
