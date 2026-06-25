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

    ALTER SEQUENCE IF EXISTS public.binary_projection_events_id_seq OWNED BY NONE;
    ALTER SEQUENCE IF EXISTS public.binary_inspections_id_seq OWNED BY NONE;
    ALTER SEQUENCE IF EXISTS public.binary_inspection_artifacts_id_seq OWNED BY NONE;
    ALTER SEQUENCE IF EXISTS public.binary_archive_entries_id_seq OWNED BY NONE;
    ALTER SEQUENCE IF EXISTS public.binary_text_evidence_id_seq OWNED BY NONE;
    ALTER SEQUENCE IF EXISTS public.binary_media_streams_id_seq OWNED BY NONE;
    ALTER SEQUENCE IF EXISTS public.binary_par2_sets_id_seq OWNED BY NONE;
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

ALTER SEQUENCE IF EXISTS public.binary_projection_events_id_seq OWNED BY public.binary_projection_events.id;
ALTER SEQUENCE IF EXISTS public.binary_inspections_id_seq OWNED BY public.binary_inspections.id;
ALTER SEQUENCE IF EXISTS public.binary_inspection_artifacts_id_seq OWNED BY public.binary_inspection_artifacts.id;
ALTER SEQUENCE IF EXISTS public.binary_archive_entries_id_seq OWNED BY public.binary_archive_entries.id;
ALTER SEQUENCE IF EXISTS public.binary_text_evidence_id_seq OWNED BY public.binary_text_evidence.id;
ALTER SEQUENCE IF EXISTS public.binary_media_streams_id_seq OWNED BY public.binary_media_streams.id;
ALTER SEQUENCE IF EXISTS public.binary_par2_sets_id_seq OWNED BY public.binary_par2_sets.id;
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
