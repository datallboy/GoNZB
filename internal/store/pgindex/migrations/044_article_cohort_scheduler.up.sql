CREATE TABLE IF NOT EXISTS public.article_cohort_candidates (
    source_posted_at timestamp with time zone NOT NULL,
    cohort_key text NOT NULL,
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    cohort_kind text NOT NULL,
    priority_rank integer DEFAULT 1 NOT NULL,
    admission_reason text DEFAULT ''::text NOT NULL,
    score double precision DEFAULT 0 NOT NULL,
    status text DEFAULT 'ready'::text NOT NULL,
    bucket_start timestamp with time zone NOT NULL,
    bucket_end timestamp with time zone NOT NULL,
    article_count integer DEFAULT 0 NOT NULL,
    unassembled_count integer DEFAULT 0 NOT NULL,
    singleton_count integer DEFAULT 0 NOT NULL,
    yenc_ready_count integer DEFAULT 0 NOT NULL,
    yenc_running_count integer DEFAULT 0 NOT NULL,
    yenc_done_count integer DEFAULT 0 NOT NULL,
    yenc_recovered_count integer DEFAULT 0 NOT NULL,
    yenc_no_identity_count integer DEFAULT 0 NOT NULL,
    subject_file_name text DEFAULT ''::text NOT NULL,
    subject_file_index integer DEFAULT 0 NOT NULL,
    subject_file_total integer DEFAULT 0 NOT NULL,
    yenc_total_parts integer DEFAULT 0 NOT NULL,
    yenc_file_size bigint DEFAULT 0 NOT NULL,
    first_article_number bigint DEFAULT 0 NOT NULL,
    last_article_number bigint DEFAULT 0 NOT NULL,
    last_scheduled_at timestamp with time zone,
    cooldown_until timestamp with time zone,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT article_cohort_candidates_pkey PRIMARY KEY (source_posted_at, cohort_key),
    CONSTRAINT article_cohort_candidates_status_check CHECK (status = ANY (ARRAY['ready'::text, 'active'::text, 'cooldown'::text, 'done'::text])),
    CONSTRAINT article_cohort_candidates_kind_check CHECK (cohort_kind = ANY (ARRAY['subject_complete'::text, 'opaque_near_time'::text, 'yenc_proven'::text]))
) PARTITION BY RANGE (source_posted_at);

CREATE TABLE IF NOT EXISTS public.article_cohort_assembly_queue (
    source_posted_at timestamp with time zone NOT NULL,
    article_header_id bigint NOT NULL,
    cohort_key text NOT NULL,
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    cohort_kind text NOT NULL,
    priority_rank integer DEFAULT 0 NOT NULL,
    score double precision DEFAULT 0 NOT NULL,
    queue_reason text DEFAULT ''::text NOT NULL,
    status text DEFAULT 'ready'::text NOT NULL,
    claim_owner text DEFAULT ''::text NOT NULL,
    claim_token uuid,
    claim_until timestamp with time zone,
    attempt_count integer DEFAULT 0 NOT NULL,
    queued_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT article_cohort_assembly_queue_pkey PRIMARY KEY (source_posted_at, article_header_id),
    CONSTRAINT article_cohort_assembly_queue_status_check CHECK (status = ANY (ARRAY['ready'::text, 'running'::text, 'done'::text, 'stale'::text])),
    CONSTRAINT article_cohort_assembly_queue_header_fkey FOREIGN KEY (source_posted_at, article_header_id) REFERENCES public.article_headers(source_posted_at, id) ON DELETE CASCADE
) PARTITION BY RANGE (source_posted_at);

CREATE TABLE IF NOT EXISTS public.article_cohort_yenc_queue (
    source_posted_at timestamp with time zone NOT NULL,
    binary_id bigint NOT NULL,
    article_header_id bigint NOT NULL,
    cohort_key text NOT NULL,
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    cohort_kind text NOT NULL,
    priority_rank integer DEFAULT 0 NOT NULL,
    admission_reason text DEFAULT ''::text NOT NULL,
    score double precision DEFAULT 0 NOT NULL,
    status text DEFAULT 'ready'::text NOT NULL,
    queued_at timestamp with time zone DEFAULT now() NOT NULL,
    admitted_at timestamp with time zone,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT article_cohort_yenc_queue_pkey PRIMARY KEY (source_posted_at, binary_id),
    CONSTRAINT article_cohort_yenc_queue_article_key UNIQUE (source_posted_at, article_header_id),
    CONSTRAINT article_cohort_yenc_queue_status_check CHECK (status = ANY (ARRAY['ready'::text, 'admitted'::text, 'done'::text, 'stale'::text])),
    CONSTRAINT article_cohort_yenc_queue_binary_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE,
    CONSTRAINT article_cohort_yenc_queue_header_fkey FOREIGN KEY (source_posted_at, article_header_id) REFERENCES public.article_headers(source_posted_at, id) ON DELETE CASCADE
) PARTITION BY RANGE (source_posted_at);

CREATE INDEX IF NOT EXISTS idx_article_cohort_candidates_ready
    ON public.article_cohort_candidates (status, priority_rank, score DESC, source_posted_at DESC, cohort_key)
    WHERE status IN ('ready', 'active');

CREATE INDEX IF NOT EXISTS idx_article_cohort_candidates_lookup
    ON public.article_cohort_candidates (provider_id, newsgroup_id, cohort_kind, bucket_start DESC, source_posted_at, cohort_key);

CREATE INDEX IF NOT EXISTS idx_article_cohort_assembly_queue_claim
    ON public.article_cohort_assembly_queue (status, priority_rank, score DESC, source_posted_at DESC, article_header_id DESC);

CREATE INDEX IF NOT EXISTS idx_article_cohort_assembly_queue_claim_until
    ON public.article_cohort_assembly_queue (claim_until, priority_rank, source_posted_at, article_header_id)
    WHERE status IN ('ready', 'running');

CREATE INDEX IF NOT EXISTS idx_article_cohort_yenc_queue_ready
    ON public.article_cohort_yenc_queue (status, priority_rank, score DESC, source_posted_at DESC, binary_id)
    WHERE status = 'ready';

CREATE INDEX IF NOT EXISTS idx_article_cohort_yenc_queue_cohort
    ON public.article_cohort_yenc_queue (cohort_key, status, source_posted_at, binary_id);

CREATE OR REPLACE FUNCTION public.pgindex_ensure_source_work_partitions(start_day date DEFAULT (CURRENT_DATE - 1), days_ahead integer DEFAULT 9)
RETURNS integer
LANGUAGE plpgsql
AS $$
DECLARE
    parent_table text;
    day_offset integer;
    created_count integer := 0;
BEGIN
    PERFORM pg_advisory_xact_lock(hashtext('pgindex-ensure-source-work-partitions'));

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

SELECT public.pgindex_ensure_source_work_partitions(CURRENT_DATE - 21, 30);

ALTER TABLE public.indexer_recovery_capacity_state
    ADD COLUMN IF NOT EXISTS priority0_reservoir_batches integer DEFAULT 5 NOT NULL;

UPDATE public.indexer_recovery_capacity_state
SET priority0_reservoir_batches = 5
WHERE priority0_reservoir_batches <= 0;
