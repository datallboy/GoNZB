DO $$
DECLARE
    table_name text;
    row_count bigint;
BEGIN
    FOREACH table_name IN ARRAY ARRAY[
        'article_headers',
        'article_header_ingest_payloads',
        'article_header_crosspost_groups',
        'article_header_poster_refs',
        'article_header_assembly_queue',
        'poster_materialization_queue',
        'yenc_recovery_work_items',
        'binary_parts'
    ]
    LOOP
        EXECUTE format('SELECT COUNT(*) FROM public.%I', table_name) INTO row_count;
        IF row_count > 0 THEN
            RAISE EXCEPTION 'native partition migration requires empty table %, found % rows', table_name, row_count;
        END IF;
    END LOOP;
END $$;

ALTER SEQUENCE IF EXISTS public.article_headers_id_seq OWNED BY NONE;
ALTER SEQUENCE IF EXISTS public.binary_parts_id_seq OWNED BY NONE;

DROP TABLE IF EXISTS public.article_header_ingest_payloads CASCADE;
DROP TABLE IF EXISTS public.article_header_crosspost_groups CASCADE;
DROP TABLE IF EXISTS public.article_header_poster_refs CASCADE;
DROP TABLE IF EXISTS public.article_header_assembly_queue CASCADE;
DROP TABLE IF EXISTS public.poster_materialization_queue CASCADE;
DROP TABLE IF EXISTS public.yenc_recovery_work_items CASCADE;
DROP TABLE IF EXISTS public.binary_parts CASCADE;
DROP TABLE IF EXISTS public.article_headers CASCADE;

CREATE SEQUENCE IF NOT EXISTS public.article_headers_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE TABLE public.article_headers (
    id bigint DEFAULT nextval('public.article_headers_id_seq'::regclass) NOT NULL,
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    article_number bigint NOT NULL,
    message_id text NOT NULL,
    date_utc timestamp with time zone,
    source_posted_at timestamp with time zone NOT NULL,
    bytes bigint DEFAULT 0 NOT NULL,
    lines integer DEFAULT 0 NOT NULL,
    scraped_at timestamp with time zone DEFAULT now() NOT NULL,
    assembled_at timestamp with time zone,
    assembly_claimed_by text DEFAULT ''::text NOT NULL,
    assembly_claimed_until timestamp with time zone,
    CONSTRAINT article_headers_pkey PRIMARY KEY (source_posted_at, id),
    CONSTRAINT article_headers_source_id_key UNIQUE (source_posted_at, id),
    CONSTRAINT article_headers_newsgroup_id_article_number_key UNIQUE (source_posted_at, newsgroup_id, article_number),
    CONSTRAINT article_headers_newsgroup_id_message_id_key UNIQUE (source_posted_at, newsgroup_id, message_id),
    CONSTRAINT article_headers_newsgroup_id_fkey FOREIGN KEY (newsgroup_id) REFERENCES public.newsgroups(id) ON DELETE RESTRICT,
    CONSTRAINT article_headers_provider_id_fkey FOREIGN KEY (provider_id) REFERENCES public.usenet_providers(id) ON DELETE RESTRICT
) PARTITION BY RANGE (source_posted_at);

ALTER SEQUENCE public.article_headers_id_seq OWNED BY public.article_headers.id;

CREATE TABLE public.article_header_ingest_payloads (
    article_header_id bigint NOT NULL,
    source_posted_at timestamp with time zone NOT NULL,
    subject text DEFAULT ''::text NOT NULL,
    poster_id bigint,
    poster text DEFAULT ''::text NOT NULL,
    xref text DEFAULT ''::text NOT NULL,
    subject_file_name text DEFAULT ''::text NOT NULL,
    subject_file_index integer DEFAULT 0 NOT NULL,
    subject_file_total integer DEFAULT 0 NOT NULL,
    yenc_part_number integer DEFAULT 0 NOT NULL,
    yenc_total_parts integer DEFAULT 0 NOT NULL,
    yenc_file_size bigint DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    yenc_recovery_missing_count integer DEFAULT 0 NOT NULL,
    yenc_recovery_last_missing_at timestamp with time zone,
    yenc_recovery_retry_after timestamp with time zone,
    CONSTRAINT article_header_ingest_payloads_pkey PRIMARY KEY (source_posted_at, article_header_id),
    CONSTRAINT article_header_ingest_payloads_article_header_id_fkey FOREIGN KEY (source_posted_at, article_header_id) REFERENCES public.article_headers(source_posted_at, id) ON DELETE CASCADE,
    CONSTRAINT article_header_ingest_payloads_poster_id_fkey FOREIGN KEY (poster_id) REFERENCES public.posters(id) ON DELETE SET NULL
) PARTITION BY RANGE (source_posted_at);

CREATE TABLE public.article_header_crosspost_groups (
    article_header_id bigint NOT NULL,
    source_posted_at timestamp with time zone NOT NULL,
    provider_id bigint NOT NULL,
    source_newsgroup_id bigint NOT NULL,
    message_id text DEFAULT ''::text NOT NULL,
    observed_group_name text DEFAULT ''::text NOT NULL,
    observed_article_number bigint DEFAULT 0 NOT NULL,
    observed_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT article_header_crosspost_groups_pkey PRIMARY KEY (source_posted_at, article_header_id, observed_group_name),
    CONSTRAINT article_header_crosspost_groups_article_header_id_fkey FOREIGN KEY (source_posted_at, article_header_id) REFERENCES public.article_headers(source_posted_at, id) ON DELETE CASCADE,
    CONSTRAINT article_header_crosspost_groups_source_newsgroup_id_fkey FOREIGN KEY (source_newsgroup_id) REFERENCES public.newsgroups(id) ON DELETE RESTRICT
) PARTITION BY RANGE (source_posted_at);

CREATE TABLE public.article_header_poster_refs (
    article_header_id bigint NOT NULL,
    source_posted_at timestamp with time zone NOT NULL,
    poster_id bigint NOT NULL,
    poster_name text DEFAULT ''::text NOT NULL,
    poster_key text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT article_header_poster_refs_pkey PRIMARY KEY (source_posted_at, article_header_id),
    CONSTRAINT article_header_poster_refs_article_header_id_fkey FOREIGN KEY (source_posted_at, article_header_id) REFERENCES public.article_headers(source_posted_at, id) ON DELETE CASCADE,
    CONSTRAINT article_header_poster_refs_poster_id_fkey FOREIGN KEY (poster_id) REFERENCES public.posters(id) ON DELETE CASCADE
) PARTITION BY RANGE (source_posted_at);

CREATE TABLE public.article_header_assembly_queue (
    article_header_id bigint NOT NULL,
    source_posted_at timestamp with time zone NOT NULL,
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    article_number bigint NOT NULL,
    message_id text NOT NULL,
    normalized_file_name text DEFAULT ''::text NOT NULL,
    queue_kind text DEFAULT 'general'::text NOT NULL,
    claim_owner text DEFAULT ''::text NOT NULL,
    claim_token uuid,
    claim_until timestamp with time zone,
    attempt_count integer DEFAULT 0 NOT NULL,
    last_error text DEFAULT ''::text NOT NULL,
    queued_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT article_header_assembly_queue_pkey PRIMARY KEY (source_posted_at, article_header_id),
    CONSTRAINT article_header_assembly_queue_check CHECK (((queue_kind = 'general'::text) OR (btrim(normalized_file_name) <> ''::text))),
    CONSTRAINT article_header_assembly_queue_queue_kind_check CHECK ((queue_kind = ANY (ARRAY['structured'::text, 'general'::text]))),
    CONSTRAINT article_header_assembly_queue_article_header_id_fkey FOREIGN KEY (source_posted_at, article_header_id) REFERENCES public.article_headers(source_posted_at, id) ON DELETE CASCADE
) PARTITION BY RANGE (source_posted_at);

CREATE TABLE public.poster_materialization_queue (
    article_header_id bigint NOT NULL,
    source_posted_at timestamp with time zone NOT NULL,
    poster_name text DEFAULT ''::text NOT NULL,
    poster_key text DEFAULT ''::text NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    ready_at timestamp with time zone DEFAULT now() NOT NULL,
    lease_owner text DEFAULT ''::text NOT NULL,
    lease_expires_at timestamp with time zone,
    attempt_count integer DEFAULT 0 NOT NULL,
    last_error text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT poster_materialization_queue_pkey PRIMARY KEY (source_posted_at, article_header_id),
    CONSTRAINT poster_materialization_queue_article_header_id_fkey FOREIGN KEY (source_posted_at, article_header_id) REFERENCES public.article_headers(source_posted_at, id) ON DELETE CASCADE
) PARTITION BY RANGE (source_posted_at);

CREATE SEQUENCE IF NOT EXISTS public.binary_parts_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE TABLE public.binary_parts (
    id bigint DEFAULT nextval('public.binary_parts_id_seq'::regclass) NOT NULL,
    binary_id bigint NOT NULL,
    article_header_id bigint NOT NULL,
    source_posted_at timestamp with time zone NOT NULL,
    message_id text DEFAULT ''::text NOT NULL,
    part_number integer NOT NULL,
    total_parts integer DEFAULT 0 NOT NULL,
    segment_bytes bigint DEFAULT 0 NOT NULL,
    file_name text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT binary_parts_pkey PRIMARY KEY (source_posted_at, id),
    CONSTRAINT binary_parts_article_header_id_key UNIQUE (source_posted_at, article_header_id),
    CONSTRAINT binary_parts_binary_id_part_number_key UNIQUE (source_posted_at, binary_id, part_number),
    CONSTRAINT binary_parts_article_header_id_fkey FOREIGN KEY (source_posted_at, article_header_id) REFERENCES public.article_headers(source_posted_at, id) ON DELETE CASCADE,
    CONSTRAINT binary_parts_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE
) PARTITION BY RANGE (source_posted_at);

ALTER SEQUENCE public.binary_parts_id_seq OWNED BY public.binary_parts.id;

CREATE TABLE public.yenc_recovery_work_items (
    binary_id bigint NOT NULL,
    article_header_id bigint NOT NULL,
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    message_id text DEFAULT ''::text NOT NULL,
    status text DEFAULT 'ready'::text NOT NULL,
    ready_at timestamp with time zone DEFAULT now() NOT NULL,
    priority_rank integer DEFAULT 0 NOT NULL,
    missing_count integer DEFAULT 0 NOT NULL,
    current_binary_key text DEFAULT ''::text NOT NULL,
    current_release_family_key text DEFAULT ''::text NOT NULL,
    current_base_stem text DEFAULT ''::text NOT NULL,
    current_readiness_bucket text DEFAULT ''::text NOT NULL,
    structured_identity_binary_matched boolean DEFAULT false NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    newsgroup_name text DEFAULT ''::text NOT NULL,
    article_number bigint DEFAULT 0 NOT NULL,
    subject text DEFAULT ''::text NOT NULL,
    poster text DEFAULT ''::text NOT NULL,
    date_utc timestamp with time zone,
    article_bytes bigint DEFAULT 0 NOT NULL,
    article_lines integer DEFAULT 0 NOT NULL,
    xref text DEFAULT ''::text NOT NULL,
    subject_file_name text DEFAULT ''::text NOT NULL,
    subject_file_index integer DEFAULT 0 NOT NULL,
    subject_file_total integer DEFAULT 0 NOT NULL,
    yenc_part_number integer DEFAULT 0 NOT NULL,
    yenc_total_parts integer DEFAULT 0 NOT NULL,
    yenc_file_size bigint DEFAULT 0 NOT NULL,
    lease_owner text DEFAULT ''::text NOT NULL,
    lease_expires_at timestamp with time zone,
    group_tier text DEFAULT 'warm'::text NOT NULL,
    admission_reason text DEFAULT ''::text NOT NULL,
    admission_score double precision DEFAULT 0 NOT NULL,
    deferred_range_id bigint REFERENCES public.deferred_article_ranges(id) ON DELETE SET NULL,
    source_posted_at timestamp with time zone DEFAULT now() NOT NULL,
    partition_day date DEFAULT CURRENT_DATE NOT NULL,
    CONSTRAINT yenc_recovery_work_items_pkey PRIMARY KEY (source_posted_at, binary_id),
    CONSTRAINT yenc_recovery_work_items_article_header_id_key UNIQUE (source_posted_at, article_header_id),
    CONSTRAINT yenc_recovery_work_items_article_header_source_posted_fkey FOREIGN KEY (source_posted_at, article_header_id) REFERENCES public.article_headers(source_posted_at, id) ON DELETE CASCADE,
    CONSTRAINT yenc_recovery_work_items_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE
) PARTITION BY RANGE (source_posted_at);

CREATE INDEX IF NOT EXISTS idx_article_headers_provider_group_date_article
    ON public.article_headers (provider_id, newsgroup_id, date_utc, article_number);

CREATE INDEX IF NOT EXISTS idx_article_headers_provider_group_article_desc
    ON public.article_headers (provider_id, newsgroup_id, article_number DESC);

CREATE INDEX IF NOT EXISTS idx_article_headers_source_posted_group_article
    ON public.article_headers (source_posted_at, provider_id, newsgroup_id, article_number);

CREATE INDEX IF NOT EXISTS idx_article_headers_unassembled
    ON public.article_headers (assembled_at, assembly_claimed_until, id)
    WHERE assembled_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_article_header_assembly_queue_source_posted
    ON public.article_header_assembly_queue (source_posted_at, claim_until, article_header_id);

CREATE INDEX IF NOT EXISTS idx_article_assembly_queue_claim
    ON public.article_header_assembly_queue (claim_until, queued_at, article_header_id);

CREATE INDEX IF NOT EXISTS idx_poster_materialization_queue_ready
    ON public.poster_materialization_queue (status, ready_at, lease_expires_at, article_header_id);

CREATE INDEX IF NOT EXISTS idx_binary_parts_binary_id
    ON public.binary_parts (binary_id);

CREATE INDEX IF NOT EXISTS idx_binary_parts_article_header_id
    ON public.binary_parts (article_header_id);

CREATE INDEX IF NOT EXISTS idx_binary_parts_source_posted
    ON public.binary_parts (source_posted_at, binary_id, article_header_id);

CREATE INDEX IF NOT EXISTS idx_yenc_recovery_work_items_article_header_id
    ON public.yenc_recovery_work_items (article_header_id);

CREATE INDEX IF NOT EXISTS idx_yenc_recovery_work_items_expired_running
    ON public.yenc_recovery_work_items (lease_expires_at, priority_rank, updated_at DESC, binary_id)
    WHERE status = 'running';

CREATE INDEX IF NOT EXISTS idx_yenc_recovery_work_items_ready
    ON public.yenc_recovery_work_items (status, ready_at, priority_rank, updated_at DESC, binary_id)
    WHERE status = 'ready';

CREATE INDEX IF NOT EXISTS idx_yenc_recovery_work_items_ready_order
    ON public.yenc_recovery_work_items (priority_rank, updated_at DESC, binary_id)
    WHERE status = 'ready';

CREATE INDEX IF NOT EXISTS idx_yenc_recovery_work_items_ready_posted_nulls_last
    ON public.yenc_recovery_work_items (priority_rank, date_utc DESC NULLS LAST, updated_at DESC, binary_id)
    WHERE status = 'ready';

CREATE INDEX IF NOT EXISTS idx_yenc_recovery_work_items_ready_date_range
    ON public.yenc_recovery_work_items (date_utc DESC NULLS LAST, priority_rank, newsgroup_id, article_number, binary_id)
    WHERE status = 'ready';

CREATE INDEX IF NOT EXISTS idx_yenc_recovery_work_items_blank_message_retire
    ON public.yenc_recovery_work_items (updated_at, binary_id)
    WHERE status IN ('ready', 'running') AND btrim(message_id) = '';

CREATE INDEX IF NOT EXISTS idx_yenc_recovery_work_items_admission_pressure
    ON public.yenc_recovery_work_items (status, group_tier, priority_rank, source_posted_at DESC, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_yenc_recovery_work_items_partition_day
    ON public.yenc_recovery_work_items (partition_day, status, provider_id, newsgroup_id);

CREATE OR REPLACE FUNCTION public.pgindex_ensure_daily_partition(parent_table text, day_start date)
RETURNS void
LANGUAGE plpgsql
AS $$
DECLARE
    partition_name text;
    default_name text;
BEGIN
    default_name := parent_table || '_default';
    EXECUTE format(
        'CREATE TABLE IF NOT EXISTS public.%I PARTITION OF public.%I DEFAULT',
        default_name,
        parent_table
    );

    partition_name := parent_table || '_' || to_char(day_start, 'YYYYMMDD');
    EXECUTE format(
        'CREATE TABLE IF NOT EXISTS public.%I PARTITION OF public.%I FOR VALUES FROM (%L) TO (%L)',
        partition_name,
        parent_table,
        day_start::timestamptz,
        (day_start + 1)::timestamptz
    );
END;
$$;

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
        'yenc_recovery_work_items',
        'binary_parts'
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
