DO $$
DECLARE
    table_name text;
    row_count bigint;
    total_rows bigint := 0;
BEGIN
    FOREACH table_name IN ARRAY ARRAY[
        'binary_observation_stats',
        'binary_identity_current',
        'binary_recovery_current',
        'binary_lifecycle',
        'binary_completion_keys',
        'binary_grouping_evidence',
        'binary_projection_events',
        'binary_superseded_sources',
        'binary_inspection_ready_queue',
        'binary_inspections',
        'binary_inspection_artifacts',
        'binary_archive_entries',
        'binary_text_evidence',
        'binary_media_streams',
        'binary_par2_sets',
        'binary_par2_targets',
        'release_family_readiness_summaries',
        'release_ready_candidates',
        'release_recovered_file_set_candidates',
        'release_stage_dirty_families'
    ]
    LOOP
        EXECUTE format('SELECT COUNT(*) FROM public.%I', table_name) INTO row_count;
        total_rows := total_rows + row_count;
    END LOOP;

    IF total_rows > 0 THEN
        RAISE EXCEPTION 'native projection/work partition migration requires empty target tables, found % rows; wipe database or run an offline data-copy migration', total_rows;
    END IF;

END $$;

CREATE OR REPLACE FUNCTION public.pgindex_rebuild_empty_source_partition_parent(parent_table text)
RETURNS void
LANGUAGE plpgsql
AS $$
DECLARE
    new_table text := parent_table || '_partition_new';
    row_count bigint;
BEGIN
    EXECUTE format('SELECT COUNT(*) FROM public.%I', parent_table) INTO row_count;
    IF row_count > 0 THEN
        RETURN;
    END IF;

    EXECUTE format('DROP TABLE IF EXISTS public.%I CASCADE', new_table);
    EXECUTE format(
        'CREATE TABLE public.%I (LIKE public.%I INCLUDING DEFAULTS INCLUDING GENERATED INCLUDING STORAGE INCLUDING COMMENTS) PARTITION BY RANGE (source_posted_at)',
        new_table,
        parent_table
    );
    EXECUTE format('DROP TABLE public.%I CASCADE', parent_table);
    EXECUTE format('ALTER TABLE public.%I RENAME TO %I', new_table, parent_table);
    EXECUTE format('ALTER TABLE public.%I ALTER COLUMN source_posted_at SET NOT NULL', parent_table);
END;
$$;

DO $$
DECLARE
    table_name text;
BEGIN
    FOREACH table_name IN ARRAY ARRAY[
        'binary_observation_stats',
        'binary_identity_current',
        'binary_recovery_current',
        'binary_lifecycle',
        'binary_completion_keys',
        'binary_grouping_evidence',
        'binary_projection_events',
        'binary_superseded_sources',
        'binary_inspection_ready_queue',
        'binary_inspections',
        'binary_inspection_artifacts',
        'binary_archive_entries',
        'binary_text_evidence',
        'binary_media_streams',
        'binary_par2_sets',
        'binary_par2_targets',
        'release_family_readiness_summaries',
        'release_ready_candidates',
        'release_recovered_file_set_candidates',
        'release_stage_dirty_families'
    ]
    LOOP
        PERFORM public.pgindex_rebuild_empty_source_partition_parent(table_name);
    END LOOP;
END $$;

ALTER TABLE IF EXISTS public.binary_observation_stats
    ADD CONSTRAINT binary_observation_stats_pkey PRIMARY KEY (source_posted_at, binary_id);

ALTER TABLE IF EXISTS public.binary_identity_current
    ADD CONSTRAINT binary_identity_current_pkey PRIMARY KEY (source_posted_at, binary_id);

ALTER TABLE IF EXISTS public.binary_recovery_current
    ADD CONSTRAINT binary_recovery_current_pkey PRIMARY KEY (source_posted_at, binary_id);

ALTER TABLE IF EXISTS public.binary_lifecycle
    ADD CONSTRAINT binary_lifecycle_pkey PRIMARY KEY (source_posted_at, binary_id);

ALTER TABLE IF EXISTS public.binary_completion_keys
    ADD CONSTRAINT binary_completion_keys_pkey PRIMARY KEY (source_posted_at, binary_id);

ALTER TABLE IF EXISTS public.binary_grouping_evidence
    ADD CONSTRAINT binary_grouping_evidence_pkey PRIMARY KEY (source_posted_at, binary_id);

ALTER TABLE IF EXISTS public.binary_projection_events
    ADD CONSTRAINT binary_projection_events_pkey PRIMARY KEY (source_posted_at, id);

ALTER TABLE IF EXISTS public.binary_superseded_sources
    ADD CONSTRAINT binary_superseded_sources_pkey PRIMARY KEY (source_posted_at, source_binary_id);

ALTER TABLE IF EXISTS public.binary_inspection_ready_queue
    ADD CONSTRAINT binary_inspection_ready_queue_pkey PRIMARY KEY (source_posted_at, stage_name, binary_id);

ALTER TABLE IF EXISTS public.binary_inspections
    ADD CONSTRAINT binary_inspections_pkey PRIMARY KEY (source_posted_at, id),
    ADD CONSTRAINT binary_inspections_stage_name_binary_id_key UNIQUE (source_posted_at, stage_name, binary_id);

ALTER TABLE IF EXISTS public.binary_inspection_artifacts
    ADD CONSTRAINT binary_inspection_artifacts_pkey PRIMARY KEY (source_posted_at, id),
    ADD CONSTRAINT binary_inspection_artifacts_binary_id_stage_name_artifact_r_key UNIQUE (source_posted_at, binary_id, stage_name, artifact_role, artifact_name);

ALTER TABLE IF EXISTS public.binary_archive_entries
    ADD CONSTRAINT binary_archive_entries_pkey PRIMARY KEY (source_posted_at, id),
    ADD CONSTRAINT binary_archive_entries_binary_id_entry_name_key UNIQUE (source_posted_at, binary_id, entry_name);

ALTER TABLE IF EXISTS public.binary_text_evidence
    ADD CONSTRAINT binary_text_evidence_pkey PRIMARY KEY (source_posted_at, id),
    ADD CONSTRAINT binary_text_evidence_binary_id_stage_name_evidence_kind_key UNIQUE (source_posted_at, binary_id, stage_name, evidence_kind);

ALTER TABLE IF EXISTS public.binary_media_streams
    ADD CONSTRAINT binary_media_streams_pkey PRIMARY KEY (source_posted_at, id),
    ADD CONSTRAINT binary_media_streams_binary_id_stream_index_stream_type_key UNIQUE (source_posted_at, binary_id, stream_index, stream_type);

ALTER TABLE IF EXISTS public.binary_par2_sets
    ADD CONSTRAINT binary_par2_sets_pkey PRIMARY KEY (source_posted_at, id),
    ADD CONSTRAINT binary_par2_sets_binary_id_set_name_key UNIQUE (source_posted_at, binary_id, set_name);

ALTER TABLE IF EXISTS public.binary_par2_targets
    ADD CONSTRAINT binary_par2_targets_pkey PRIMARY KEY (source_posted_at, id),
    ADD CONSTRAINT binary_par2_targets_binary_id_file_name_key UNIQUE (source_posted_at, binary_id, file_name);

ALTER TABLE IF EXISTS public.release_family_readiness_summaries
    ADD CONSTRAINT release_family_readiness_summaries_pkey PRIMARY KEY (source_posted_at, provider_id, newsgroup_id, key_kind, family_key);

ALTER TABLE IF EXISTS public.release_ready_candidates
    ADD CONSTRAINT release_ready_candidates_pkey PRIMARY KEY (source_posted_at, provider_id, newsgroup_id, key_kind, family_key);

ALTER TABLE IF EXISTS public.release_recovered_file_set_candidates
    ADD CONSTRAINT release_recovered_file_set_candidates_pkey PRIMARY KEY (source_posted_at, provider_id, file_set_key);

ALTER TABLE IF EXISTS public.release_stage_dirty_families
    ADD CONSTRAINT release_stage_dirty_families_pkey PRIMARY KEY (source_posted_at, provider_id, newsgroup_id, key_kind, family_key);

ALTER TABLE IF EXISTS public.binary_observation_stats
    ADD CONSTRAINT binary_observation_stats_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;

ALTER TABLE IF EXISTS public.binary_identity_current
    ADD CONSTRAINT binary_identity_current_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;

ALTER TABLE IF EXISTS public.binary_recovery_current
    ADD CONSTRAINT binary_recovery_current_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;

ALTER TABLE IF EXISTS public.binary_lifecycle
    ADD CONSTRAINT binary_lifecycle_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;

ALTER TABLE IF EXISTS public.binary_completion_keys
    ADD CONSTRAINT binary_completion_keys_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;

ALTER TABLE IF EXISTS public.binary_grouping_evidence
    ADD CONSTRAINT binary_grouping_evidence_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;

ALTER TABLE IF EXISTS public.binary_projection_events
    ADD CONSTRAINT binary_projection_events_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;

ALTER TABLE IF EXISTS public.binary_superseded_sources
    ADD CONSTRAINT binary_superseded_sources_source_fkey FOREIGN KEY (source_binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE,
    ADD CONSTRAINT binary_superseded_sources_target_fkey FOREIGN KEY (target_binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;

ALTER TABLE IF EXISTS public.binary_inspection_ready_queue
    ADD CONSTRAINT binary_inspection_ready_queue_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;

ALTER TABLE IF EXISTS public.binary_inspections
    ADD CONSTRAINT binary_inspections_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;

ALTER TABLE IF EXISTS public.binary_inspection_artifacts
    ADD CONSTRAINT binary_inspection_artifacts_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;

ALTER TABLE IF EXISTS public.binary_archive_entries
    ADD CONSTRAINT binary_archive_entries_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;

ALTER TABLE IF EXISTS public.binary_text_evidence
    ADD CONSTRAINT binary_text_evidence_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;

ALTER TABLE IF EXISTS public.binary_media_streams
    ADD CONSTRAINT binary_media_streams_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;

ALTER TABLE IF EXISTS public.binary_par2_sets
    ADD CONSTRAINT binary_par2_sets_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;

ALTER TABLE IF EXISTS public.binary_par2_targets
    ADD CONSTRAINT binary_par2_targets_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;

ALTER TABLE IF EXISTS public.release_family_readiness_summaries
    ADD CONSTRAINT release_family_readiness_summaries_provider_id_fkey FOREIGN KEY (provider_id) REFERENCES public.usenet_providers(id) ON DELETE CASCADE,
    ADD CONSTRAINT release_family_readiness_summaries_newsgroup_id_fkey FOREIGN KEY (newsgroup_id) REFERENCES public.newsgroups(id) ON DELETE CASCADE;

ALTER TABLE IF EXISTS public.release_ready_candidates
    ADD CONSTRAINT release_ready_candidates_provider_id_fkey FOREIGN KEY (provider_id) REFERENCES public.usenet_providers(id) ON DELETE CASCADE;

ALTER TABLE IF EXISTS public.release_recovered_file_set_candidates
    ADD CONSTRAINT release_recovered_file_set_candidates_provider_id_fkey FOREIGN KEY (provider_id) REFERENCES public.usenet_providers(id) ON DELETE CASCADE;

ALTER TABLE IF EXISTS public.release_stage_dirty_families
    ADD CONSTRAINT release_stage_dirty_families_provider_id_fkey FOREIGN KEY (provider_id) REFERENCES public.usenet_providers(id) ON DELETE CASCADE,
    ADD CONSTRAINT release_stage_dirty_families_newsgroup_id_fkey FOREIGN KEY (newsgroup_id) REFERENCES public.newsgroups(id) ON DELETE CASCADE;

CREATE INDEX IF NOT EXISTS idx_binary_observation_stats_source_posted
    ON public.binary_observation_stats (source_posted_at, provider_id, newsgroup_id, binary_id);

CREATE INDEX IF NOT EXISTS idx_binary_observation_completeness
    ON public.binary_observation_stats (source_posted_at, provider_id, newsgroup_id, observed_parts, total_parts);

CREATE INDEX IF NOT EXISTS idx_binary_observation_incomplete_rank
    ON public.binary_observation_stats (source_posted_at, observed_parts DESC, binary_id DESC)
    INCLUDE (provider_id, newsgroup_id, total_parts)
    WHERE total_parts > 0 AND observed_parts < total_parts;

CREATE INDEX IF NOT EXISTS idx_binary_identity_current_source_posted
    ON public.binary_identity_current (source_posted_at, provider_id, newsgroup_id, binary_id);

CREATE INDEX IF NOT EXISTS idx_binary_identity_release_family
    ON public.binary_identity_current (source_posted_at, provider_id, newsgroup_id, release_family_key);

CREATE INDEX IF NOT EXISTS idx_binary_identity_file_set
    ON public.binary_identity_current (source_posted_at, provider_id, file_set_key, newsgroup_id)
    WHERE btrim(file_set_key) <> '';

CREATE INDEX IF NOT EXISTS idx_binary_identity_inspect_discovery_backlog
    ON public.binary_identity_current (source_posted_at, updated_at DESC, binary_id DESC)
    INCLUDE (release_family_key, base_stem, release_name, binary_name, file_name, file_index, expected_file_count, expected_archive_file_count, is_auxiliary, is_main_payload, match_confidence, match_status)
    WHERE ((is_main_payload = true) OR (is_auxiliary = false))
      AND (
        lower(COALESCE(NULLIF(file_name, ''), NULLIF(binary_name, ''), '')) LIKE '%.bin'
        OR COALESCE(NULLIF(file_name, ''), NULLIF(binary_name, ''), '') !~ '\.[A-Za-z0-9]{1,8}$'
      );

CREATE INDEX IF NOT EXISTS idx_binary_identity_inspect_par2_backlog
    ON public.binary_identity_current (source_posted_at, updated_at DESC, binary_id DESC)
    INCLUDE (release_family_key, release_name, binary_name, file_name, match_confidence)
    WHERE lower(COALESCE(NULLIF(file_name, ''), NULLIF(binary_name, ''), '')) LIKE '%.par2';

CREATE INDEX IF NOT EXISTS idx_binary_recovery_current_source_posted
    ON public.binary_recovery_current (source_posted_at, provider_id, newsgroup_id, binary_id);

CREATE INDEX IF NOT EXISTS idx_binary_recovery_backlog
    ON public.binary_recovery_current (source_posted_at, provider_id, newsgroup_id, recovered_source, recovered_confidence);

CREATE INDEX IF NOT EXISTS idx_binary_lifecycle_source_posted
    ON public.binary_lifecycle (source_posted_at, provider_id, newsgroup_id, binary_id);

CREATE INDEX IF NOT EXISTS idx_binary_lifecycle_release
    ON public.binary_lifecycle (source_posted_at, release_id, lifecycle_status);

CREATE INDEX IF NOT EXISTS idx_binary_completion_keys_match
    ON public.binary_completion_keys (source_posted_at, provider_id, newsgroup_id, normalized_file_name, is_main_payload DESC, observed_parts DESC, binary_id DESC);

CREATE INDEX IF NOT EXISTS idx_binary_completion_keys_match_rank
    ON public.binary_completion_keys (source_posted_at, provider_id, newsgroup_id, normalized_file_name, is_main_payload DESC, completion_ratio DESC, observed_parts DESC, binary_id DESC)
    INCLUDE (posted_at);

CREATE INDEX IF NOT EXISTS idx_binary_completion_keys_rank
    ON public.binary_completion_keys (source_posted_at, is_main_payload DESC, completion_ratio DESC, observed_parts DESC, binary_id DESC);

CREATE INDEX IF NOT EXISTS idx_binary_grouping_evidence_source_posted
    ON public.binary_grouping_evidence (source_posted_at, binary_id);

CREATE INDEX IF NOT EXISTS idx_binary_projection_events_source_posted
    ON public.binary_projection_events (source_posted_at, event_stage, event_kind, binary_id);

CREATE INDEX IF NOT EXISTS idx_binary_projection_events_stage
    ON public.binary_projection_events (source_posted_at, event_stage, event_kind, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_binary_superseded_sources_source_posted
    ON public.binary_superseded_sources (source_posted_at, provider_id, newsgroup_id, source_binary_id);

CREATE INDEX IF NOT EXISTS idx_binary_superseded_sources_target
    ON public.binary_superseded_sources (source_posted_at, target_binary_id);

CREATE INDEX IF NOT EXISTS idx_binary_superseded_sources_release_family
    ON public.binary_superseded_sources (source_posted_at, provider_id, newsgroup_id, release_family_key);

CREATE INDEX IF NOT EXISTS idx_binary_inspection_ready_queue_source_posted
    ON public.binary_inspection_ready_queue (source_posted_at, stage_name, status, binary_id);

CREATE INDEX IF NOT EXISTS idx_binary_inspections_source_posted
    ON public.binary_inspections (source_posted_at, stage_name, status, binary_id);

CREATE INDEX IF NOT EXISTS idx_binary_inspections_status
    ON public.binary_inspections (source_posted_at, stage_name, status, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_binary_inspection_artifacts_source_posted
    ON public.binary_inspection_artifacts (source_posted_at, stage_name, binary_id);

CREATE INDEX IF NOT EXISTS idx_binary_archive_entries_source_posted
    ON public.binary_archive_entries (source_posted_at, binary_id);

CREATE INDEX IF NOT EXISTS idx_binary_text_evidence_source_posted
    ON public.binary_text_evidence (source_posted_at, stage_name, binary_id);

CREATE INDEX IF NOT EXISTS idx_binary_media_streams_source_posted
    ON public.binary_media_streams (source_posted_at, binary_id);

CREATE INDEX IF NOT EXISTS idx_binary_par2_sets_source_posted
    ON public.binary_par2_sets (source_posted_at, binary_id);

CREATE INDEX IF NOT EXISTS idx_binary_par2_targets_source_posted
    ON public.binary_par2_targets (source_posted_at, binary_id);

CREATE INDEX IF NOT EXISTS idx_release_family_readiness_source_posted
    ON public.release_family_readiness_summaries (source_posted_at, provider_id, newsgroup_id, key_kind, family_key);

CREATE INDEX IF NOT EXISTS idx_release_family_readiness_bucket_lookup
    ON public.release_family_readiness_summaries (source_posted_at, readiness_bucket, provider_id, newsgroup_id, key_kind, family_key);

CREATE INDEX IF NOT EXISTS idx_release_family_readiness_summaries_pending
    ON public.release_family_readiness_summaries (source_posted_at, updated_at, provider_id, newsgroup_id)
    WHERE updated_at > COALESCE(processed_at, updated_at);

CREATE INDEX IF NOT EXISTS idx_release_family_readiness_summaries_release_queue
    ON public.release_family_readiness_summaries (source_posted_at, updated_at, provider_id, newsgroup_id, key_kind, family_key)
    WHERE recover_pending = false;

CREATE INDEX IF NOT EXISTS idx_release_family_yenc_recovery_candidates
    ON public.release_family_readiness_summaries (source_posted_at, provider_id, newsgroup_id, family_key)
    WHERE key_kind = 'release_family'
      AND readiness_bucket = ANY (ARRAY['overgrouped_contextual'::text, 'weak_single_binary'::text, 'weak_obfuscated_set'::text]);

CREATE INDEX IF NOT EXISTS idx_release_ready_candidates_source_posted
    ON public.release_ready_candidates (source_posted_at, provider_id, newsgroup_id, key_kind, family_key);

CREATE INDEX IF NOT EXISTS idx_release_ready_candidates_queue
    ON public.release_ready_candidates (source_posted_at, updated_at, provider_id, newsgroup_id, key_kind, family_key);

CREATE INDEX IF NOT EXISTS idx_release_recovered_file_set_candidates_source_posted
    ON public.release_recovered_file_set_candidates (source_posted_at, provider_id, file_set_key);

CREATE INDEX IF NOT EXISTS idx_release_recovered_file_set_candidates_queue
    ON public.release_recovered_file_set_candidates (source_posted_at, updated_at, provider_id, representative_newsgroup_id, file_set_key);

CREATE INDEX IF NOT EXISTS idx_release_stage_dirty_families_source_posted
    ON public.release_stage_dirty_families (source_posted_at, provider_id, newsgroup_id, key_kind, family_key);

CREATE INDEX IF NOT EXISTS idx_release_stage_dirty_families_updated_at
    ON public.release_stage_dirty_families (source_posted_at, updated_at, provider_id, newsgroup_id);

DO $$
DECLARE
    item record;
BEGIN
    FOR item IN
        SELECT *
        FROM (VALUES
            ('binary_projection_events', 'binary_projection_events_id_seq'),
            ('binary_inspections', 'binary_inspections_id_seq'),
            ('binary_inspection_artifacts', 'binary_inspection_artifacts_id_seq'),
            ('binary_archive_entries', 'binary_archive_entries_id_seq'),
            ('binary_text_evidence', 'binary_text_evidence_id_seq'),
            ('binary_media_streams', 'binary_media_streams_id_seq'),
            ('binary_par2_sets', 'binary_par2_sets_id_seq'),
            ('binary_par2_targets', 'binary_par2_targets_id_seq')
        ) AS t(table_name, sequence_name)
    LOOP
        EXECUTE format('ALTER TABLE public.%I ALTER COLUMN id DROP IDENTITY IF EXISTS', item.table_name);
        EXECUTE format('CREATE SEQUENCE IF NOT EXISTS public.%I', item.sequence_name);
        EXECUTE format(
            'ALTER TABLE public.%I ALTER COLUMN id SET DEFAULT nextval(%L::regclass)',
            item.table_name,
            'public.' || item.sequence_name
        );
        EXECUTE format(
            'ALTER SEQUENCE public.%I OWNED BY public.%I.id',
            item.sequence_name,
            item.table_name
        );
    END LOOP;
END $$;
CREATE OR REPLACE FUNCTION public.pgindex_ensure_source_work_partitions(start_day date DEFAULT (CURRENT_DATE - 1), days_ahead integer DEFAULT 9)
RETURNS integer
LANGUAGE plpgsql
AS $$
DECLARE
    parent_table text;
    day_offset integer;
    created_count integer := 0;
BEGIN
    FOREACH parent_table IN ARRAY ARRAY[
        'article_headers',
        'article_header_ingest_payloads',
        'article_header_crosspost_groups',
        'article_header_poster_refs',
        'article_header_assembly_queue',
        'poster_materialization_queue',
        'binary_parts',
        'binary_observation_stats',
        'binary_identity_current',
        'binary_recovery_current',
        'binary_lifecycle',
        'binary_completion_keys',
        'binary_grouping_evidence',
        'binary_projection_events',
        'binary_superseded_sources',
        'yenc_recovery_work_items',
        'binary_inspection_ready_queue',
        'binary_inspections',
        'binary_inspection_artifacts',
        'binary_archive_entries',
        'binary_text_evidence',
        'binary_media_streams',
        'binary_par2_sets',
        'binary_par2_targets',
        'release_family_readiness_summaries',
        'release_ready_candidates',
        'release_recovered_file_set_candidates',
        'release_stage_dirty_families'
    ]
    LOOP
        FOR day_offset IN 0..days_ahead LOOP
            PERFORM public.pgindex_ensure_daily_partition(parent_table, start_day + day_offset);
            created_count := created_count + 1;
        END LOOP;
    END LOOP;
    RETURN created_count;
END;
$$;

SELECT public.pgindex_ensure_source_work_partitions(CURRENT_DATE - 1, 9);
