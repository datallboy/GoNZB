CREATE OR REPLACE FUNCTION public.pgindex_ensure_daily_partition(parent_table text, day_start date)
RETURNS void
LANGUAGE plpgsql
AS $$
DECLARE
    partition_name text;
    default_name text;
    utc_start timestamptz;
    utc_end timestamptz;
BEGIN
    PERFORM pg_advisory_xact_lock(hashtext('pgindex-ensure-daily-partition:' || parent_table || ':' || day_start::text));

    default_name := parent_table || '_default';
    EXECUTE format(
        'CREATE TABLE IF NOT EXISTS public.%I PARTITION OF public.%I DEFAULT',
        default_name,
        parent_table
    );

    partition_name := parent_table || '_' || to_char(day_start, 'YYYYMMDD');
    utc_start := day_start::timestamp AT TIME ZONE 'UTC';
    utc_end := (day_start + 1)::timestamp AT TIME ZONE 'UTC';
    EXECUTE format(
        'CREATE TABLE IF NOT EXISTS public.%I PARTITION OF public.%I FOR VALUES FROM (%L) TO (%L)',
        partition_name,
        parent_table,
        utc_start,
        utc_end
    );
END;
$$;

DO $$
DECLARE
    child record;
    row_count bigint;
BEGIN
    FOR child IN
        SELECT
            parent.relname AS parent_name,
            part.relname AS child_name
        FROM pg_inherits i
        JOIN pg_class parent ON parent.oid = i.inhparent
        JOIN pg_namespace parent_ns ON parent_ns.oid = parent.relnamespace
        JOIN pg_class part ON part.oid = i.inhrelid
        JOIN pg_namespace part_ns ON part_ns.oid = part.relnamespace
        WHERE parent_ns.nspname = 'public'
          AND part_ns.nspname = 'public'
          AND part.relname ~ '_[0-9]{8}$'
          AND parent.relname = ANY (ARRAY[
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
            'article_cohort_candidates',
            'article_cohort_assembly_queue',
            'article_cohort_yenc_queue',
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
          ])
    LOOP
        EXECUTE format('SELECT count(*) FROM public.%I', child.child_name) INTO row_count;
        IF row_count = 0 THEN
            EXECUTE format('ALTER TABLE public.%I DETACH PARTITION public.%I', child.parent_name, child.child_name);
            EXECUTE format('DROP TABLE public.%I', child.child_name);
        ELSE
            RAISE NOTICE 'leaving non-empty partition %.% in place; run an offline repartition if UTC bounds are required for existing rows', 'public', child.child_name;
        END IF;
    END LOOP;
END $$;

SELECT public.pgindex_ensure_source_work_partitions(CURRENT_DATE - 21, 30);
