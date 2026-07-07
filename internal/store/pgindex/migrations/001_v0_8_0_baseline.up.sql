-- v0.8.0 PostgreSQL baseline generated from the final pre-release pgindex schema.
-- Runtime-created daily child partitions are intentionally omitted; startup provisioning creates them.

--
-- PostgreSQL database dump
--


-- Dumped from database version 17.10 (Debian 17.10-1.pgdg13+1)
-- Dumped by pg_dump version 17.10 (Debian 17.10-1.pgdg13+1)

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SET search_path = public, pg_catalog;
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

--
-- Name: public; Type: SCHEMA; Schema: -; Owner: -
--



--
-- Name: SCHEMA public; Type: COMMENT; Schema: -; Owner: -
--



--
-- Name: pgindex_ensure_daily_partition(text, date); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.pgindex_ensure_daily_partition(parent_table text, day_start date) RETURNS void
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


SET default_tablespace = '';

--
-- Name: article_cohort_assembly_queue; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.article_cohort_assembly_queue (
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
    CONSTRAINT article_cohort_assembly_queue_status_check CHECK ((status = ANY (ARRAY['ready'::text, 'running'::text, 'done'::text, 'stale'::text])))
)
PARTITION BY RANGE (source_posted_at);


SET default_table_access_method = heap;

--
-- Name: article_cohort_assembly_queue_default; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.article_cohort_assembly_queue_default (
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
    CONSTRAINT article_cohort_assembly_queue_status_check CHECK ((status = ANY (ARRAY['ready'::text, 'running'::text, 'done'::text, 'stale'::text])))
);


--
-- Name: article_cohort_candidates; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.article_cohort_candidates (
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
    CONSTRAINT article_cohort_candidates_kind_check CHECK ((cohort_kind = ANY (ARRAY['subject_complete'::text, 'opaque_near_time'::text, 'yenc_proven'::text]))),
    CONSTRAINT article_cohort_candidates_status_check CHECK ((status = ANY (ARRAY['ready'::text, 'active'::text, 'cooldown'::text, 'done'::text])))
)
PARTITION BY RANGE (source_posted_at);


--
-- Name: article_cohort_candidates_default; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.article_cohort_candidates_default (
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
    CONSTRAINT article_cohort_candidates_kind_check CHECK ((cohort_kind = ANY (ARRAY['subject_complete'::text, 'opaque_near_time'::text, 'yenc_proven'::text]))),
    CONSTRAINT article_cohort_candidates_status_check CHECK ((status = ANY (ARRAY['ready'::text, 'active'::text, 'cooldown'::text, 'done'::text])))
);


--
-- Name: article_cohort_yenc_queue; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.article_cohort_yenc_queue (
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
    CONSTRAINT article_cohort_yenc_queue_status_check CHECK ((status = ANY (ARRAY['ready'::text, 'admitted'::text, 'done'::text, 'stale'::text])))
)
PARTITION BY RANGE (source_posted_at);


--
-- Name: article_cohort_yenc_queue_default; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.article_cohort_yenc_queue_default (
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
    CONSTRAINT article_cohort_yenc_queue_status_check CHECK ((status = ANY (ARRAY['ready'::text, 'admitted'::text, 'done'::text, 'stale'::text])))
);


--
-- Name: article_header_assembly_queue; Type: TABLE; Schema: public; Owner: -
--

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
    CONSTRAINT article_header_assembly_queue_check CHECK (((queue_kind = 'general'::text) OR (btrim(normalized_file_name) <> ''::text))),
    CONSTRAINT article_header_assembly_queue_queue_kind_check CHECK ((queue_kind = ANY (ARRAY['structured'::text, 'general'::text])))
)
PARTITION BY RANGE (source_posted_at);


--
-- Name: article_header_assembly_queue_default; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.article_header_assembly_queue_default (
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
    CONSTRAINT article_header_assembly_queue_check CHECK (((queue_kind = 'general'::text) OR (btrim(normalized_file_name) <> ''::text))),
    CONSTRAINT article_header_assembly_queue_queue_kind_check CHECK ((queue_kind = ANY (ARRAY['structured'::text, 'general'::text])))
);


--
-- Name: article_header_crosspost_group_summary; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.article_header_crosspost_group_summary (
    observed_group_name text NOT NULL,
    observed_article_count bigint DEFAULT 0 NOT NULL,
    distinct_message_count bigint DEFAULT 0 NOT NULL,
    distinct_source_group_count bigint DEFAULT 0 NOT NULL,
    last_seen_at timestamp with time zone,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    last_refreshed_article_header_id bigint DEFAULT 0 NOT NULL
);


--
-- Name: article_header_crosspost_groups; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.article_header_crosspost_groups (
    article_header_id bigint NOT NULL,
    source_posted_at timestamp with time zone NOT NULL,
    provider_id bigint NOT NULL,
    source_newsgroup_id bigint NOT NULL,
    message_id text DEFAULT ''::text NOT NULL,
    observed_group_name text DEFAULT ''::text NOT NULL,
    observed_article_number bigint DEFAULT 0 NOT NULL,
    observed_at timestamp with time zone DEFAULT now() NOT NULL
)
PARTITION BY RANGE (source_posted_at);


--
-- Name: article_header_crosspost_groups_default; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.article_header_crosspost_groups_default (
    article_header_id bigint NOT NULL,
    source_posted_at timestamp with time zone NOT NULL,
    provider_id bigint NOT NULL,
    source_newsgroup_id bigint NOT NULL,
    message_id text DEFAULT ''::text NOT NULL,
    observed_group_name text DEFAULT ''::text NOT NULL,
    observed_article_number bigint DEFAULT 0 NOT NULL,
    observed_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: article_header_ingest_payloads; Type: TABLE; Schema: public; Owner: -
--

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
    yenc_recovery_retry_after timestamp with time zone
)
PARTITION BY RANGE (source_posted_at);


--
-- Name: article_header_ingest_payloads_default; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.article_header_ingest_payloads_default (
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
    yenc_recovery_retry_after timestamp with time zone
);


--
-- Name: article_header_poster_refs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.article_header_poster_refs (
    article_header_id bigint NOT NULL,
    source_posted_at timestamp with time zone NOT NULL,
    poster_id bigint NOT NULL,
    poster_name text DEFAULT ''::text NOT NULL,
    poster_key text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
)
PARTITION BY RANGE (source_posted_at);


--
-- Name: article_header_poster_refs_default; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.article_header_poster_refs_default (
    article_header_id bigint NOT NULL,
    source_posted_at timestamp with time zone NOT NULL,
    poster_id bigint NOT NULL,
    poster_name text DEFAULT ''::text NOT NULL,
    poster_key text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: article_headers; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.article_headers (
    id bigint NOT NULL,
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    article_number bigint NOT NULL,
    message_id text NOT NULL,
    date_utc timestamp with time zone,
    source_posted_at timestamp with time zone NOT NULL,
    bytes bigint DEFAULT 0 NOT NULL,
    lines integer DEFAULT 0 NOT NULL,
    scraped_at timestamp with time zone DEFAULT now() NOT NULL
)
PARTITION BY RANGE (source_posted_at);


--
-- Name: article_headers_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.article_headers_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: article_headers_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.article_headers_id_seq OWNED BY public.article_headers.id;


--
-- Name: article_headers_default; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.article_headers_default (
    id bigint DEFAULT nextval('public.article_headers_id_seq'::regclass) NOT NULL,
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    article_number bigint NOT NULL,
    message_id text NOT NULL,
    date_utc timestamp with time zone,
    source_posted_at timestamp with time zone NOT NULL,
    bytes bigint DEFAULT 0 NOT NULL,
    lines integer DEFAULT 0 NOT NULL,
    scraped_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: binary_archive_entries; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.binary_archive_entries (
    id bigint NOT NULL,
    binary_id bigint NOT NULL,
    release_id text,
    entry_name text DEFAULT ''::text NOT NULL,
    is_dir boolean DEFAULT false NOT NULL,
    uncompressed_bytes bigint DEFAULT 0 NOT NULL,
    compressed_bytes bigint DEFAULT 0 NOT NULL,
    encrypted boolean DEFAULT false NOT NULL,
    comment_text text DEFAULT ''::text NOT NULL,
    media_type text DEFAULT ''::text NOT NULL,
    signature text DEFAULT ''::text NOT NULL,
    metadata_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    source_posted_at timestamp with time zone NOT NULL
)
PARTITION BY RANGE (source_posted_at);


--
-- Name: binary_archive_entries_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.binary_archive_entries_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: binary_archive_entries_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.binary_archive_entries_id_seq OWNED BY public.binary_archive_entries.id;


--
-- Name: binary_archive_entries_default; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.binary_archive_entries_default (
    id bigint DEFAULT nextval('public.binary_archive_entries_id_seq'::regclass) NOT NULL,
    binary_id bigint NOT NULL,
    release_id text,
    entry_name text DEFAULT ''::text NOT NULL,
    is_dir boolean DEFAULT false NOT NULL,
    uncompressed_bytes bigint DEFAULT 0 NOT NULL,
    compressed_bytes bigint DEFAULT 0 NOT NULL,
    encrypted boolean DEFAULT false NOT NULL,
    comment_text text DEFAULT ''::text NOT NULL,
    media_type text DEFAULT ''::text NOT NULL,
    signature text DEFAULT ''::text NOT NULL,
    metadata_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    source_posted_at timestamp with time zone NOT NULL
);


--
-- Name: binary_completion_keys; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.binary_completion_keys (
    binary_id bigint NOT NULL,
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    normalized_file_name text NOT NULL,
    is_main_payload boolean DEFAULT false NOT NULL,
    observed_parts integer DEFAULT 0 NOT NULL,
    total_parts integer DEFAULT 0 NOT NULL,
    completion_ratio double precision DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    posted_at timestamp with time zone,
    source_posted_at timestamp with time zone NOT NULL
)
PARTITION BY RANGE (source_posted_at);


--
-- Name: binary_completion_keys_default; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.binary_completion_keys_default (
    binary_id bigint NOT NULL,
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    normalized_file_name text NOT NULL,
    is_main_payload boolean DEFAULT false NOT NULL,
    observed_parts integer DEFAULT 0 NOT NULL,
    total_parts integer DEFAULT 0 NOT NULL,
    completion_ratio double precision DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    posted_at timestamp with time zone,
    source_posted_at timestamp with time zone NOT NULL
);


--
-- Name: binary_core; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.binary_core (
    binary_id bigint NOT NULL,
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    poster_id bigint,
    binary_key text NOT NULL,
    original_binary_name text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    source_posted_at timestamp with time zone
)
WITH (fillfactor='100');


--
-- Name: binary_core_binary_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.binary_core_binary_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: binary_core_binary_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.binary_core_binary_id_seq OWNED BY public.binary_core.binary_id;


--
-- Name: binary_grouping_evidence; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.binary_grouping_evidence (
    binary_id bigint NOT NULL,
    evidence_source text DEFAULT 'matcher'::text NOT NULL,
    evidence_version text DEFAULT 'v1'::text NOT NULL,
    payload_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    source_posted_at timestamp with time zone NOT NULL
)
PARTITION BY RANGE (source_posted_at);
ALTER TABLE ONLY public.binary_grouping_evidence ALTER COLUMN payload_json SET STORAGE EXTERNAL;


--
-- Name: binary_grouping_evidence_default; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.binary_grouping_evidence_default (
    binary_id bigint NOT NULL,
    evidence_source text DEFAULT 'matcher'::text NOT NULL,
    evidence_version text DEFAULT 'v1'::text NOT NULL,
    payload_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    source_posted_at timestamp with time zone NOT NULL
);
ALTER TABLE ONLY public.binary_grouping_evidence_default ALTER COLUMN payload_json SET STORAGE EXTERNAL;


--
-- Name: binary_identity_current; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.binary_identity_current (
    binary_id bigint NOT NULL,
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    source_release_key text DEFAULT ''::text NOT NULL,
    release_family_key text DEFAULT ''::text NOT NULL,
    file_set_key text DEFAULT ''::text NOT NULL,
    file_family_key text DEFAULT ''::text NOT NULL,
    identity_strength text DEFAULT ''::text NOT NULL,
    identity_reason text DEFAULT ''::text NOT NULL,
    subject_set_token text DEFAULT ''::text NOT NULL,
    subject_set_kind text DEFAULT ''::text NOT NULL,
    family_kind text DEFAULT ''::text NOT NULL,
    base_stem text DEFAULT ''::text NOT NULL,
    release_key text DEFAULT ''::text NOT NULL,
    release_name text DEFAULT ''::text NOT NULL,
    binary_name text DEFAULT ''::text NOT NULL,
    file_name text DEFAULT ''::text NOT NULL,
    file_index integer DEFAULT 0 NOT NULL,
    expected_file_count integer DEFAULT 0 NOT NULL,
    expected_archive_file_count integer DEFAULT 0 NOT NULL,
    is_auxiliary boolean DEFAULT false NOT NULL,
    is_main_payload boolean DEFAULT false NOT NULL,
    match_confidence double precision DEFAULT 0 NOT NULL,
    match_status text DEFAULT 'low_confidence'::text NOT NULL,
    grouping_summary_kind text DEFAULT ''::text NOT NULL,
    grouping_summary_status text DEFAULT ''::text NOT NULL,
    grouping_summary_fallback_used boolean DEFAULT false NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    source_posted_at timestamp with time zone NOT NULL
)
PARTITION BY RANGE (source_posted_at);


--
-- Name: binary_identity_current_default; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.binary_identity_current_default (
    binary_id bigint NOT NULL,
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    source_release_key text DEFAULT ''::text NOT NULL,
    release_family_key text DEFAULT ''::text NOT NULL,
    file_set_key text DEFAULT ''::text NOT NULL,
    file_family_key text DEFAULT ''::text NOT NULL,
    identity_strength text DEFAULT ''::text NOT NULL,
    identity_reason text DEFAULT ''::text NOT NULL,
    subject_set_token text DEFAULT ''::text NOT NULL,
    subject_set_kind text DEFAULT ''::text NOT NULL,
    family_kind text DEFAULT ''::text NOT NULL,
    base_stem text DEFAULT ''::text NOT NULL,
    release_key text DEFAULT ''::text NOT NULL,
    release_name text DEFAULT ''::text NOT NULL,
    binary_name text DEFAULT ''::text NOT NULL,
    file_name text DEFAULT ''::text NOT NULL,
    file_index integer DEFAULT 0 NOT NULL,
    expected_file_count integer DEFAULT 0 NOT NULL,
    expected_archive_file_count integer DEFAULT 0 NOT NULL,
    is_auxiliary boolean DEFAULT false NOT NULL,
    is_main_payload boolean DEFAULT false NOT NULL,
    match_confidence double precision DEFAULT 0 NOT NULL,
    match_status text DEFAULT 'low_confidence'::text NOT NULL,
    grouping_summary_kind text DEFAULT ''::text NOT NULL,
    grouping_summary_status text DEFAULT ''::text NOT NULL,
    grouping_summary_fallback_used boolean DEFAULT false NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    source_posted_at timestamp with time zone NOT NULL
);


--
-- Name: binary_inspection_artifacts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.binary_inspection_artifacts (
    id bigint NOT NULL,
    binary_id bigint NOT NULL,
    release_id text,
    stage_name text DEFAULT ''::text NOT NULL,
    artifact_role text DEFAULT ''::text NOT NULL,
    artifact_name text DEFAULT ''::text NOT NULL,
    artifact_path text DEFAULT ''::text NOT NULL,
    bytes_total bigint DEFAULT 0 NOT NULL,
    mime_type text DEFAULT ''::text NOT NULL,
    signature text DEFAULT ''::text NOT NULL,
    source_kind text DEFAULT ''::text NOT NULL,
    metadata_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    source_posted_at timestamp with time zone NOT NULL
)
PARTITION BY RANGE (source_posted_at);


--
-- Name: binary_inspection_artifacts_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.binary_inspection_artifacts_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: binary_inspection_artifacts_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.binary_inspection_artifacts_id_seq OWNED BY public.binary_inspection_artifacts.id;


--
-- Name: binary_inspection_artifacts_default; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.binary_inspection_artifacts_default (
    id bigint DEFAULT nextval('public.binary_inspection_artifacts_id_seq'::regclass) NOT NULL,
    binary_id bigint NOT NULL,
    release_id text,
    stage_name text DEFAULT ''::text NOT NULL,
    artifact_role text DEFAULT ''::text NOT NULL,
    artifact_name text DEFAULT ''::text NOT NULL,
    artifact_path text DEFAULT ''::text NOT NULL,
    bytes_total bigint DEFAULT 0 NOT NULL,
    mime_type text DEFAULT ''::text NOT NULL,
    signature text DEFAULT ''::text NOT NULL,
    source_kind text DEFAULT ''::text NOT NULL,
    metadata_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    source_posted_at timestamp with time zone NOT NULL
);


--
-- Name: binary_inspection_ready_queue; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.binary_inspection_ready_queue (
    stage_name text NOT NULL,
    binary_id bigint NOT NULL,
    release_id text DEFAULT ''::text NOT NULL,
    status text DEFAULT 'ready'::text NOT NULL,
    ready_at timestamp with time zone DEFAULT now() NOT NULL,
    source_updated_at timestamp with time zone,
    claimed_by text DEFAULT ''::text NOT NULL,
    claimed_until timestamp with time zone,
    last_error text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    source_posted_at timestamp with time zone NOT NULL
)
PARTITION BY RANGE (source_posted_at);


--
-- Name: binary_inspection_ready_queue_default; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.binary_inspection_ready_queue_default (
    stage_name text NOT NULL,
    binary_id bigint NOT NULL,
    release_id text DEFAULT ''::text NOT NULL,
    status text DEFAULT 'ready'::text NOT NULL,
    ready_at timestamp with time zone DEFAULT now() NOT NULL,
    source_updated_at timestamp with time zone,
    claimed_by text DEFAULT ''::text NOT NULL,
    claimed_until timestamp with time zone,
    last_error text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    source_posted_at timestamp with time zone NOT NULL
);


--
-- Name: binary_inspections; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.binary_inspections (
    id bigint NOT NULL,
    stage_name text NOT NULL,
    binary_id bigint NOT NULL,
    release_id text,
    status text DEFAULT 'pending'::text NOT NULL,
    started_at timestamp with time zone,
    finished_at timestamp with time zone,
    error_text text DEFAULT ''::text NOT NULL,
    materialized_bytes bigint DEFAULT 0 NOT NULL,
    tool_provenance_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    summary_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    source_updated_at timestamp with time zone,
    inspection_claimed_by text DEFAULT ''::text NOT NULL,
    inspection_claimed_until timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    source_posted_at timestamp with time zone NOT NULL
)
PARTITION BY RANGE (source_posted_at);


--
-- Name: binary_inspections_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.binary_inspections_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: binary_inspections_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.binary_inspections_id_seq OWNED BY public.binary_inspections.id;


--
-- Name: binary_inspections_default; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.binary_inspections_default (
    id bigint DEFAULT nextval('public.binary_inspections_id_seq'::regclass) NOT NULL,
    stage_name text NOT NULL,
    binary_id bigint NOT NULL,
    release_id text,
    status text DEFAULT 'pending'::text NOT NULL,
    started_at timestamp with time zone,
    finished_at timestamp with time zone,
    error_text text DEFAULT ''::text NOT NULL,
    materialized_bytes bigint DEFAULT 0 NOT NULL,
    tool_provenance_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    summary_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    source_updated_at timestamp with time zone,
    inspection_claimed_by text DEFAULT ''::text NOT NULL,
    inspection_claimed_until timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    source_posted_at timestamp with time zone NOT NULL
);


--
-- Name: binary_lifecycle; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.binary_lifecycle (
    binary_id bigint NOT NULL,
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    release_id text DEFAULT ''::text NOT NULL,
    lifecycle_status text DEFAULT 'active'::text NOT NULL,
    archived_at timestamp with time zone,
    purge_eligible_at timestamp with time zone,
    purged_at timestamp with time zone,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    source_posted_at timestamp with time zone NOT NULL
)
PARTITION BY RANGE (source_posted_at);


--
-- Name: binary_lifecycle_default; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.binary_lifecycle_default (
    binary_id bigint NOT NULL,
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    release_id text DEFAULT ''::text NOT NULL,
    lifecycle_status text DEFAULT 'active'::text NOT NULL,
    archived_at timestamp with time zone,
    purge_eligible_at timestamp with time zone,
    purged_at timestamp with time zone,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    source_posted_at timestamp with time zone NOT NULL
);


--
-- Name: binary_media_streams; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.binary_media_streams (
    id bigint NOT NULL,
    binary_id bigint NOT NULL,
    release_id text,
    stream_index integer DEFAULT 0 NOT NULL,
    stream_type text DEFAULT ''::text NOT NULL,
    codec_name text DEFAULT ''::text NOT NULL,
    codec_long_name text DEFAULT ''::text NOT NULL,
    profile text DEFAULT ''::text NOT NULL,
    width integer DEFAULT 0 NOT NULL,
    height integer DEFAULT 0 NOT NULL,
    channels integer DEFAULT 0 NOT NULL,
    language text DEFAULT ''::text NOT NULL,
    duration_seconds double precision DEFAULT 0 NOT NULL,
    bit_rate bigint DEFAULT 0 NOT NULL,
    default_disposition boolean DEFAULT false NOT NULL,
    forced_disposition boolean DEFAULT false NOT NULL,
    metadata_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    source_posted_at timestamp with time zone NOT NULL
)
PARTITION BY RANGE (source_posted_at);


--
-- Name: binary_media_streams_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.binary_media_streams_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: binary_media_streams_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.binary_media_streams_id_seq OWNED BY public.binary_media_streams.id;


--
-- Name: binary_media_streams_default; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.binary_media_streams_default (
    id bigint DEFAULT nextval('public.binary_media_streams_id_seq'::regclass) NOT NULL,
    binary_id bigint NOT NULL,
    release_id text,
    stream_index integer DEFAULT 0 NOT NULL,
    stream_type text DEFAULT ''::text NOT NULL,
    codec_name text DEFAULT ''::text NOT NULL,
    codec_long_name text DEFAULT ''::text NOT NULL,
    profile text DEFAULT ''::text NOT NULL,
    width integer DEFAULT 0 NOT NULL,
    height integer DEFAULT 0 NOT NULL,
    channels integer DEFAULT 0 NOT NULL,
    language text DEFAULT ''::text NOT NULL,
    duration_seconds double precision DEFAULT 0 NOT NULL,
    bit_rate bigint DEFAULT 0 NOT NULL,
    default_disposition boolean DEFAULT false NOT NULL,
    forced_disposition boolean DEFAULT false NOT NULL,
    metadata_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    source_posted_at timestamp with time zone NOT NULL
);


--
-- Name: binary_observation_stats; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.binary_observation_stats (
    binary_id bigint NOT NULL,
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    total_parts integer DEFAULT 0 NOT NULL,
    observed_parts integer DEFAULT 0 NOT NULL,
    total_bytes bigint DEFAULT 0 NOT NULL,
    first_article_number bigint DEFAULT 0 NOT NULL,
    last_article_number bigint DEFAULT 0 NOT NULL,
    posted_at timestamp with time zone,
    refreshed_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    source_posted_at timestamp with time zone NOT NULL,
    part_source_posted_at_min timestamp with time zone,
    part_source_posted_at_max timestamp with time zone
)
PARTITION BY RANGE (source_posted_at);


--
-- Name: binary_observation_stats_default; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.binary_observation_stats_default (
    binary_id bigint NOT NULL,
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    total_parts integer DEFAULT 0 NOT NULL,
    observed_parts integer DEFAULT 0 NOT NULL,
    total_bytes bigint DEFAULT 0 NOT NULL,
    first_article_number bigint DEFAULT 0 NOT NULL,
    last_article_number bigint DEFAULT 0 NOT NULL,
    posted_at timestamp with time zone,
    refreshed_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    source_posted_at timestamp with time zone NOT NULL,
    part_source_posted_at_min timestamp with time zone,
    part_source_posted_at_max timestamp with time zone
);


--
-- Name: binary_par2_sets; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.binary_par2_sets (
    id bigint NOT NULL,
    binary_id bigint NOT NULL,
    release_id text,
    set_name text DEFAULT ''::text NOT NULL,
    base_name text DEFAULT ''::text NOT NULL,
    is_volume boolean DEFAULT false NOT NULL,
    volume_number integer DEFAULT 0 NOT NULL,
    recovery_blocks integer DEFAULT 0 NOT NULL,
    signature_ok boolean DEFAULT false NOT NULL,
    metadata_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    source_posted_at timestamp with time zone NOT NULL
)
PARTITION BY RANGE (source_posted_at);


--
-- Name: binary_par2_sets_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.binary_par2_sets_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: binary_par2_sets_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.binary_par2_sets_id_seq OWNED BY public.binary_par2_sets.id;


--
-- Name: binary_par2_sets_default; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.binary_par2_sets_default (
    id bigint DEFAULT nextval('public.binary_par2_sets_id_seq'::regclass) NOT NULL,
    binary_id bigint NOT NULL,
    release_id text,
    set_name text DEFAULT ''::text NOT NULL,
    base_name text DEFAULT ''::text NOT NULL,
    is_volume boolean DEFAULT false NOT NULL,
    volume_number integer DEFAULT 0 NOT NULL,
    recovery_blocks integer DEFAULT 0 NOT NULL,
    signature_ok boolean DEFAULT false NOT NULL,
    metadata_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    source_posted_at timestamp with time zone NOT NULL
);


--
-- Name: binary_par2_targets; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.binary_par2_targets (
    id bigint NOT NULL,
    binary_id bigint NOT NULL,
    release_id text DEFAULT ''::text NOT NULL,
    file_name text NOT NULL,
    file_size bigint DEFAULT 0 NOT NULL,
    metadata_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    source_posted_at timestamp with time zone NOT NULL
)
PARTITION BY RANGE (source_posted_at);


--
-- Name: binary_par2_targets_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.binary_par2_targets_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: binary_par2_targets_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.binary_par2_targets_id_seq OWNED BY public.binary_par2_targets.id;


--
-- Name: binary_par2_targets_default; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.binary_par2_targets_default (
    id bigint DEFAULT nextval('public.binary_par2_targets_id_seq'::regclass) NOT NULL,
    binary_id bigint NOT NULL,
    release_id text DEFAULT ''::text NOT NULL,
    file_name text NOT NULL,
    file_size bigint DEFAULT 0 NOT NULL,
    metadata_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    source_posted_at timestamp with time zone NOT NULL
);


--
-- Name: binary_parts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.binary_parts (
    id bigint NOT NULL,
    binary_id bigint NOT NULL,
    article_header_id bigint NOT NULL,
    source_posted_at timestamp with time zone NOT NULL,
    message_id text DEFAULT ''::text NOT NULL,
    part_number integer NOT NULL,
    total_parts integer DEFAULT 0 NOT NULL,
    segment_bytes bigint DEFAULT 0 NOT NULL,
    file_name text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
)
PARTITION BY RANGE (source_posted_at);


--
-- Name: binary_parts_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.binary_parts_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: binary_parts_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.binary_parts_id_seq OWNED BY public.binary_parts.id;


--
-- Name: binary_parts_default; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.binary_parts_default (
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
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: binary_projection_events; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.binary_projection_events (
    id bigint NOT NULL,
    binary_id bigint,
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    event_stage text NOT NULL,
    event_kind text NOT NULL,
    event_key text DEFAULT ''::text NOT NULL,
    event_value text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    source_posted_at timestamp with time zone NOT NULL
)
PARTITION BY RANGE (source_posted_at);


--
-- Name: binary_projection_events_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.binary_projection_events_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: binary_projection_events_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.binary_projection_events_id_seq OWNED BY public.binary_projection_events.id;


--
-- Name: binary_projection_events_default; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.binary_projection_events_default (
    id bigint DEFAULT nextval('public.binary_projection_events_id_seq'::regclass) NOT NULL,
    binary_id bigint,
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    event_stage text NOT NULL,
    event_kind text NOT NULL,
    event_key text DEFAULT ''::text NOT NULL,
    event_value text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    source_posted_at timestamp with time zone NOT NULL
);


--
-- Name: binary_recovery_current; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.binary_recovery_current (
    binary_id bigint NOT NULL,
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    recovered_kind text DEFAULT ''::text NOT NULL,
    recovered_extension text DEFAULT ''::text NOT NULL,
    recovered_source text DEFAULT ''::text NOT NULL,
    recovered_confidence double precision DEFAULT 0 NOT NULL,
    recovered_file_name text DEFAULT ''::text NOT NULL,
    recovered_at timestamp with time zone,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    source_posted_at timestamp with time zone NOT NULL
)
PARTITION BY RANGE (source_posted_at);


--
-- Name: binary_recovery_current_default; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.binary_recovery_current_default (
    binary_id bigint NOT NULL,
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    recovered_kind text DEFAULT ''::text NOT NULL,
    recovered_extension text DEFAULT ''::text NOT NULL,
    recovered_source text DEFAULT ''::text NOT NULL,
    recovered_confidence double precision DEFAULT 0 NOT NULL,
    recovered_file_name text DEFAULT ''::text NOT NULL,
    recovered_at timestamp with time zone,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    source_posted_at timestamp with time zone NOT NULL
);


--
-- Name: binary_superseded_sources; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.binary_superseded_sources (
    source_binary_id bigint NOT NULL,
    target_binary_id bigint NOT NULL,
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    release_family_key text DEFAULT ''::text NOT NULL,
    source_binary_key text DEFAULT ''::text NOT NULL,
    target_binary_key text DEFAULT ''::text NOT NULL,
    superseded_reason text DEFAULT 'yenc_recovery_merge'::text NOT NULL,
    superseded_at timestamp with time zone DEFAULT now() NOT NULL,
    purged_at timestamp with time zone,
    source_posted_at timestamp with time zone NOT NULL
)
PARTITION BY RANGE (source_posted_at);


--
-- Name: binary_superseded_sources_default; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.binary_superseded_sources_default (
    source_binary_id bigint NOT NULL,
    target_binary_id bigint NOT NULL,
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    release_family_key text DEFAULT ''::text NOT NULL,
    source_binary_key text DEFAULT ''::text NOT NULL,
    target_binary_key text DEFAULT ''::text NOT NULL,
    superseded_reason text DEFAULT 'yenc_recovery_merge'::text NOT NULL,
    superseded_at timestamp with time zone DEFAULT now() NOT NULL,
    purged_at timestamp with time zone,
    source_posted_at timestamp with time zone NOT NULL
);


--
-- Name: binary_text_evidence; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.binary_text_evidence (
    id bigint NOT NULL,
    binary_id bigint NOT NULL,
    release_id text,
    stage_name text DEFAULT ''::text NOT NULL,
    evidence_kind text DEFAULT ''::text NOT NULL,
    text_value text DEFAULT ''::text NOT NULL,
    tokens_json jsonb DEFAULT '[]'::jsonb NOT NULL,
    metadata_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    source_posted_at timestamp with time zone NOT NULL
)
PARTITION BY RANGE (source_posted_at);


--
-- Name: binary_text_evidence_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.binary_text_evidence_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: binary_text_evidence_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.binary_text_evidence_id_seq OWNED BY public.binary_text_evidence.id;


--
-- Name: binary_text_evidence_default; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.binary_text_evidence_default (
    id bigint DEFAULT nextval('public.binary_text_evidence_id_seq'::regclass) NOT NULL,
    binary_id bigint NOT NULL,
    release_id text,
    stage_name text DEFAULT ''::text NOT NULL,
    evidence_kind text DEFAULT ''::text NOT NULL,
    text_value text DEFAULT ''::text NOT NULL,
    tokens_json jsonb DEFAULT '[]'::jsonb NOT NULL,
    metadata_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    source_posted_at timestamp with time zone NOT NULL
);


--
-- Name: crosspost_popularity_refresh_queue; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.crosspost_popularity_refresh_queue (
    observed_group_name text NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    ready_at timestamp with time zone DEFAULT now() NOT NULL,
    lease_owner text DEFAULT ''::text NOT NULL,
    lease_expires_at timestamp with time zone,
    attempt_count integer DEFAULT 0 NOT NULL,
    last_error text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
)
WITH (fillfactor='80', autovacuum_vacuum_scale_factor='0.01', autovacuum_analyze_scale_factor='0.02', autovacuum_vacuum_threshold='5000', autovacuum_analyze_threshold='5000');


--
-- Name: deferred_article_ranges; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.deferred_article_ranges (
    id bigint NOT NULL,
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    article_low bigint NOT NULL,
    article_high bigint NOT NULL,
    posted_at_min timestamp with time zone,
    posted_at_max timestamp with time zone,
    observed_at timestamp with time zone DEFAULT now() NOT NULL,
    estimated_article_count bigint DEFAULT 0 NOT NULL,
    estimated_obfuscated_count bigint DEFAULT 0 NOT NULL,
    reason text DEFAULT ''::text NOT NULL,
    priority_score double precision DEFAULT 0 NOT NULL,
    state text DEFAULT 'ready'::text NOT NULL,
    attempts integer DEFAULT 0 NOT NULL,
    last_attempt_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT deferred_article_ranges_article_low_check CHECK ((article_low > 0)),
    CONSTRAINT deferred_article_ranges_check CHECK ((article_high >= article_low)),
    CONSTRAINT deferred_article_ranges_state_check CHECK ((state = ANY (ARRAY['ready'::text, 'running'::text, 'completed'::text, 'abandoned'::text])))
);


--
-- Name: deferred_article_ranges_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.deferred_article_ranges_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: deferred_article_ranges_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.deferred_article_ranges_id_seq OWNED BY public.deferred_article_ranges.id;


--
-- Name: indexer_daily_bucket_stats; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.indexer_daily_bucket_stats (
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    bucket_day date NOT NULL,
    tier text DEFAULT 'warm'::text NOT NULL,
    scrape_progress_known boolean DEFAULT false NOT NULL,
    lower_boundary_crossed boolean DEFAULT false NOT NULL,
    upper_boundary_crossed boolean DEFAULT false NOT NULL,
    bucket_article_low bigint DEFAULT 0 NOT NULL,
    bucket_article_high bigint DEFAULT 0 NOT NULL,
    scrape_cursor_low bigint DEFAULT 0 NOT NULL,
    scrape_cursor_high bigint DEFAULT 0 NOT NULL,
    headers_staged bigint DEFAULT 0 NOT NULL,
    unassembled_headers bigint DEFAULT 0 NOT NULL,
    yenc_ready bigint DEFAULT 0 NOT NULL,
    yenc_running bigint DEFAULT 0 NOT NULL,
    yenc_done bigint DEFAULT 0 NOT NULL,
    yenc_stale bigint DEFAULT 0 NOT NULL,
    binaries_total bigint DEFAULT 0 NOT NULL,
    binaries_complete bigint DEFAULT 0 NOT NULL,
    binaries_weak bigint DEFAULT 0 NOT NULL,
    releases_created bigint DEFAULT 0 NOT NULL,
    archive_pending bigint DEFAULT 0 NOT NULL,
    purge_pending bigint DEFAULT 0 NOT NULL,
    blocker_count bigint DEFAULT 0 NOT NULL,
    last_refreshed_at timestamp with time zone DEFAULT now() NOT NULL,
    scrape_progress_pct double precision DEFAULT 0 NOT NULL
);


--
-- Name: indexer_dashboard_stats; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.indexer_dashboard_stats (
    stat_key text NOT NULL,
    int_value bigint DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone,
    refresh_attempted_at timestamp with time zone,
    last_error text DEFAULT ''::text NOT NULL
);


--
-- Name: indexer_group_profiles; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.indexer_group_profiles (
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    tier text DEFAULT 'warm'::text NOT NULL,
    tier_override text,
    score double precision DEFAULT 0 NOT NULL,
    articles_scraped_1d bigint DEFAULT 0 NOT NULL,
    recovery_queued_1d bigint DEFAULT 0 NOT NULL,
    yenc_probes_attempted_1d bigint DEFAULT 0 NOT NULL,
    yenc_probes_successful_1d bigint DEFAULT 0 NOT NULL,
    binaries_completed_1d bigint DEFAULT 0 NOT NULL,
    releases_created_1d bigint DEFAULT 0 NOT NULL,
    avg_recovery_lag_seconds double precision DEFAULT 0 NOT NULL,
    max_recovery_lag_seconds double precision DEFAULT 0 NOT NULL,
    last_scored_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT indexer_group_profiles_tier_check CHECK ((tier = ANY (ARRAY['hot'::text, 'warm'::text, 'cold'::text, 'disabled'::text]))),
    CONSTRAINT indexer_group_profiles_tier_override_check CHECK (((tier_override IS NULL) OR (tier_override = ANY (ARRAY['hot'::text, 'warm'::text, 'cold'::text, 'disabled'::text]))))
);


--
-- Name: indexer_nntp_runtime_snapshots; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.indexer_nntp_runtime_snapshots (
    publisher_id text NOT NULL,
    module_name text NOT NULL,
    scope text NOT NULL,
    payload jsonb NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);





--
-- Name: indexer_provider_group_inventory; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.indexer_provider_group_inventory (
    provider_id text NOT NULL,
    provider_name text DEFAULT ''::text NOT NULL,
    group_name text NOT NULL,
    high bigint DEFAULT 0 NOT NULL,
    low bigint DEFAULT 0 NOT NULL,
    status text DEFAULT ''::text NOT NULL,
    scanned_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: indexer_recovery_capacity_state; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.indexer_recovery_capacity_state (
    id boolean DEFAULT true NOT NULL,
    probes_per_hour_ewma double precision DEFAULT 25000 NOT NULL,
    soft_cap bigint DEFAULT 100000 NOT NULL,
    hard_cap bigint DEFAULT 200000 NOT NULL,
    open_ready bigint DEFAULT 0 NOT NULL,
    open_running bigint DEFAULT 0 NOT NULL,
    oldest_ready_at timestamp with time zone,
    newest_ready_at timestamp with time zone,
    calculated_at timestamp with time zone DEFAULT now() NOT NULL,
    soft_queue_hours integer DEFAULT 4 NOT NULL,
    hard_queue_multiplier integer DEFAULT 2 NOT NULL,
    absolute_hard_queue_cap bigint DEFAULT 250000 NOT NULL,
    bootstrap_probes_per_hour double precision DEFAULT 25000 NOT NULL,
    ewma_window_minutes integer DEFAULT 30 NOT NULL,
    priority0_overflow_cap bigint DEFAULT 25000 NOT NULL,
    config_updated_at timestamp with time zone DEFAULT now() NOT NULL,
    near_time_cohort_bucket_minutes integer DEFAULT 5 NOT NULL,
    priority0_reservoir_batches integer DEFAULT 5 NOT NULL,
    CONSTRAINT indexer_recovery_capacity_state_id_check CHECK (id)
);


--
-- Name: indexer_scrape_day_boundaries; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.indexer_scrape_day_boundaries (
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    bucket_day date NOT NULL,
    lower_boundary_crossed boolean DEFAULT false NOT NULL,
    upper_boundary_crossed boolean DEFAULT false NOT NULL,
    bucket_article_low bigint DEFAULT 0 NOT NULL,
    bucket_article_high bigint DEFAULT 0 NOT NULL,
    observed_article_count bigint DEFAULT 0 NOT NULL,
    first_observed_at timestamp with time zone DEFAULT now() NOT NULL,
    last_observed_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: indexer_stage_runs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.indexer_stage_runs (
    id bigint NOT NULL,
    stage_name text NOT NULL,
    trigger_kind text DEFAULT 'scheduled'::text NOT NULL,
    status text DEFAULT 'running'::text NOT NULL,
    claimed_by text DEFAULT ''::text NOT NULL,
    started_at timestamp with time zone DEFAULT now() NOT NULL,
    heartbeat_at timestamp with time zone,
    finished_at timestamp with time zone,
    error_text text DEFAULT ''::text NOT NULL,
    metrics_json jsonb DEFAULT '{}'::jsonb NOT NULL
);


--
-- Name: indexer_stage_runs_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.indexer_stage_runs_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: indexer_stage_runs_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.indexer_stage_runs_id_seq OWNED BY public.indexer_stage_runs.id;


--
-- Name: indexer_stage_state; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.indexer_stage_state (
    stage_name text NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    paused boolean DEFAULT false NOT NULL,
    interval_seconds integer DEFAULT 600 NOT NULL,
    batch_size integer DEFAULT 0 NOT NULL,
    concurrency integer DEFAULT 1 NOT NULL,
    backoff_seconds integer DEFAULT 0 NOT NULL,
    lease_owner text DEFAULT ''::text NOT NULL,
    lease_expires_at timestamp with time zone,
    last_heartbeat_at timestamp with time zone,
    last_run_id bigint,
    last_success_at timestamp with time zone,
    last_error text DEFAULT ''::text NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);



--
-- Name: newsgroups; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.newsgroups (
    id bigint NOT NULL,
    group_name text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: newsgroups_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.newsgroups_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: newsgroups_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.newsgroups_id_seq OWNED BY public.newsgroups.id;


--
-- Name: nzb_cache; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.nzb_cache (
    release_id text NOT NULL,
    generation_status text DEFAULT 'pending'::text NOT NULL,
    nzb_hash_sha256 text DEFAULT ''::text NOT NULL,
    generated_at timestamp with time zone,
    last_error text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: poster_materialization_queue; Type: TABLE; Schema: public; Owner: -
--

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
    updated_at timestamp with time zone DEFAULT now() NOT NULL
)
PARTITION BY RANGE (source_posted_at);


--
-- Name: poster_materialization_queue_default; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.poster_materialization_queue_default (
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
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: posters; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.posters (
    id bigint NOT NULL,
    poster_name text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: posters_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.posters_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: posters_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.posters_id_seq OWNED BY public.posters.id;


--
-- Name: predb_backfill_checkpoints; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.predb_backfill_checkpoints (
    provider text NOT NULL,
    offset_hint integer DEFAULT 0 NOT NULL,
    oldest_posted_at timestamp with time zone,
    oldest_normalized_title text DEFAULT ''::text NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: predb_entries; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.predb_entries (
    id bigint NOT NULL,
    normalized_title text NOT NULL,
    title text NOT NULL,
    category text DEFAULT ''::text NOT NULL,
    source text DEFAULT ''::text NOT NULL,
    posted_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    external_id bigint DEFAULT 0 NOT NULL,
    team text DEFAULT ''::text NOT NULL,
    genre text DEFAULT ''::text NOT NULL,
    url text DEFAULT ''::text NOT NULL,
    size_kb double precision DEFAULT 0 NOT NULL,
    file_count integer DEFAULT 0 NOT NULL,
    payload_json jsonb DEFAULT '{}'::jsonb NOT NULL
);


--
-- Name: predb_entries_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.predb_entries_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: predb_entries_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.predb_entries_id_seq OWNED BY public.predb_entries.id;


--
-- Name: release_archive_detail_files; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.release_archive_detail_files (
    release_id text NOT NULL,
    file_name text NOT NULL,
    size_bytes bigint DEFAULT 0 NOT NULL,
    file_index integer DEFAULT 0 NOT NULL,
    is_pars boolean DEFAULT false NOT NULL,
    posted_at timestamp with time zone,
    article_count integer DEFAULT 0 NOT NULL,
    total_parts integer DEFAULT 0 NOT NULL,
    observed_parts integer DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: release_archive_detail_snapshots; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.release_archive_detail_snapshots (
    release_id text NOT NULL,
    guid text DEFAULT ''::text NOT NULL,
    title text DEFAULT ''::text NOT NULL,
    posted_at timestamp with time zone,
    added_at timestamp with time zone,
    size_bytes bigint DEFAULT 0 NOT NULL,
    file_count integer DEFAULT 0 NOT NULL,
    completion_pct double precision DEFAULT 0 NOT NULL,
    category_id integer DEFAULT 0 NOT NULL,
    category text DEFAULT ''::text NOT NULL,
    classification text DEFAULT ''::text NOT NULL,
    has_par2 boolean DEFAULT false NOT NULL,
    has_nfo boolean DEFAULT false NOT NULL,
    password_state text DEFAULT ''::text NOT NULL,
    availability_score double precision DEFAULT 0 NOT NULL,
    availability_tier text DEFAULT ''::text NOT NULL,
    media_quality_score double precision DEFAULT 0 NOT NULL,
    media_quality_tier text DEFAULT ''::text NOT NULL,
    tmdb_id bigint DEFAULT 0 NOT NULL,
    tvdb_id bigint DEFAULT 0 NOT NULL,
    imdb_id text DEFAULT ''::text NOT NULL,
    external_media_type text DEFAULT ''::text NOT NULL,
    external_title text DEFAULT ''::text NOT NULL,
    external_year integer DEFAULT 0 NOT NULL,
    metadata_updated_at timestamp with time zone,
    runtime_seconds integer DEFAULT 0 NOT NULL,
    primary_resolution text DEFAULT ''::text NOT NULL,
    primary_video_codec text DEFAULT ''::text NOT NULL,
    primary_audio_codec text DEFAULT ''::text NOT NULL,
    sample_present boolean DEFAULT false NOT NULL,
    archive_count integer DEFAULT 0 NOT NULL,
    video_count integer DEFAULT 0 NOT NULL,
    audio_count integer DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: release_archive_detail_subtitle_languages; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.release_archive_detail_subtitle_languages (
    release_id text NOT NULL,
    ordinal integer NOT NULL,
    language text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: release_archive_lineage_article_headers; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.release_archive_lineage_article_headers (
    release_id text NOT NULL,
    article_header_id bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: release_archive_lineage_binaries; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.release_archive_lineage_binaries (
    release_id text NOT NULL,
    binary_id bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: release_archive_state; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.release_archive_state (
    release_id text NOT NULL,
    archive_status text DEFAULT 'active'::text NOT NULL,
    archive_store text DEFAULT 'indexer_archive'::text NOT NULL,
    object_store_kind text DEFAULT 'fs'::text NOT NULL,
    object_key text DEFAULT ''::text NOT NULL,
    content_hash_sha256 text DEFAULT ''::text NOT NULL,
    object_size_bytes bigint DEFAULT 0 NOT NULL,
    content_encoding text DEFAULT 'identity'::text NOT NULL,
    source_module text DEFAULT 'usenet_index'::text NOT NULL,
    archived_at timestamp with time zone,
    purge_eligible_at timestamp with time zone,
    purge_completed_at timestamp with time zone,
    last_archive_error text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    preview_object_key text DEFAULT ''::text NOT NULL,
    preview_content_type text DEFAULT ''::text NOT NULL,
    preview_source_kind text DEFAULT ''::text NOT NULL,
    preview_updated_at timestamp with time zone
);


--
-- Name: release_catalog_files; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.release_catalog_files (
    id bigint NOT NULL,
    release_id text NOT NULL,
    file_name text NOT NULL,
    size_bytes bigint DEFAULT 0 NOT NULL,
    file_index integer DEFAULT 0 NOT NULL,
    is_pars boolean DEFAULT false NOT NULL,
    subject text DEFAULT ''::text NOT NULL,
    poster text DEFAULT ''::text NOT NULL,
    posted_at timestamp with time zone,
    article_count integer DEFAULT 0 NOT NULL,
    total_parts integer DEFAULT 0 NOT NULL,
    observed_parts integer DEFAULT 0 NOT NULL,
    match_confidence double precision DEFAULT 0 NOT NULL,
    match_status text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: release_catalog_files_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.release_catalog_files_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: release_catalog_files_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.release_catalog_files_id_seq OWNED BY public.release_catalog_files.id;


--
-- Name: release_family_readiness_acks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.release_family_readiness_acks (
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    key_kind text NOT NULL,
    family_key text NOT NULL,
    processed_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: release_family_readiness_summaries; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.release_family_readiness_summaries (
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    key_kind text NOT NULL,
    family_key text NOT NULL,
    source_release_key text DEFAULT ''::text NOT NULL,
    release_key text DEFAULT ''::text NOT NULL,
    release_name text DEFAULT ''::text NOT NULL,
    binary_count integer DEFAULT 0 NOT NULL,
    complete_binary_count integer DEFAULT 0 NOT NULL,
    incomplete_binary_count integer DEFAULT 0 NOT NULL,
    has_expected_file_count boolean DEFAULT false NOT NULL,
    total_bytes bigint DEFAULT 0 NOT NULL,
    earliest_posted_at timestamp with time zone,
    readiness_bucket text DEFAULT 'fragment_only'::text NOT NULL,
    processed_at timestamp with time zone,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    expected_file_count integer DEFAULT 0 NOT NULL,
    complete_main_payload_binary_count integer DEFAULT 0 NOT NULL,
    expected_file_coverage_pct double precision DEFAULT 0 NOT NULL,
    dominant_family_kind text DEFAULT ''::text NOT NULL,
    dominant_file_name text DEFAULT ''::text NOT NULL,
    dominant_match_confidence double precision DEFAULT 0 NOT NULL,
    expected_archive_file_count integer DEFAULT 0 NOT NULL,
    has_expected_archive_file_count boolean DEFAULT false NOT NULL,
    archive_file_coverage_pct double precision DEFAULT 0 NOT NULL,
    recover_pending boolean DEFAULT false NOT NULL,
    source_posted_at timestamp with time zone NOT NULL
)
PARTITION BY RANGE (source_posted_at);


--
-- Name: release_family_readiness_summaries_default; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.release_family_readiness_summaries_default (
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    key_kind text NOT NULL,
    family_key text NOT NULL,
    source_release_key text DEFAULT ''::text NOT NULL,
    release_key text DEFAULT ''::text NOT NULL,
    release_name text DEFAULT ''::text NOT NULL,
    binary_count integer DEFAULT 0 NOT NULL,
    complete_binary_count integer DEFAULT 0 NOT NULL,
    incomplete_binary_count integer DEFAULT 0 NOT NULL,
    has_expected_file_count boolean DEFAULT false NOT NULL,
    total_bytes bigint DEFAULT 0 NOT NULL,
    earliest_posted_at timestamp with time zone,
    readiness_bucket text DEFAULT 'fragment_only'::text NOT NULL,
    processed_at timestamp with time zone,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    expected_file_count integer DEFAULT 0 NOT NULL,
    complete_main_payload_binary_count integer DEFAULT 0 NOT NULL,
    expected_file_coverage_pct double precision DEFAULT 0 NOT NULL,
    dominant_family_kind text DEFAULT ''::text NOT NULL,
    dominant_file_name text DEFAULT ''::text NOT NULL,
    dominant_match_confidence double precision DEFAULT 0 NOT NULL,
    expected_archive_file_count integer DEFAULT 0 NOT NULL,
    has_expected_archive_file_count boolean DEFAULT false NOT NULL,
    archive_file_coverage_pct double precision DEFAULT 0 NOT NULL,
    recover_pending boolean DEFAULT false NOT NULL,
    source_posted_at timestamp with time zone NOT NULL
);


--
-- Name: release_family_summary_refresh_queue; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.release_family_summary_refresh_queue (
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    key_kind text NOT NULL,
    family_key text NOT NULL,
    queued_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: release_files; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.release_files (
    id bigint NOT NULL,
    release_id text NOT NULL,
    binary_id bigint,
    file_name text NOT NULL,
    size_bytes bigint DEFAULT 0 NOT NULL,
    file_index integer DEFAULT 0 NOT NULL,
    is_pars boolean DEFAULT false NOT NULL,
    subject text DEFAULT ''::text NOT NULL,
    poster text DEFAULT ''::text NOT NULL,
    posted_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: release_files_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.release_files_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: release_files_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.release_files_id_seq OWNED BY public.release_files.id;


--
-- Name: release_newsgroups; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.release_newsgroups (
    release_id text NOT NULL,
    newsgroup_id bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: release_overrides; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.release_overrides (
    release_id text NOT NULL,
    display_title text DEFAULT ''::text NOT NULL,
    classification_override text DEFAULT ''::text NOT NULL,
    tmdb_id_override bigint DEFAULT 0 NOT NULL,
    tvdb_id_override bigint DEFAULT 0 NOT NULL,
    imdb_id_override text DEFAULT ''::text NOT NULL,
    hidden boolean DEFAULT false NOT NULL,
    notes text DEFAULT ''::text NOT NULL,
    tags_json jsonb DEFAULT '[]'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: release_password_candidates; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.release_password_candidates (
    id bigint NOT NULL,
    release_id text NOT NULL,
    binary_id bigint,
    artifact_id bigint,
    password_value text NOT NULL,
    source_kind text DEFAULT ''::text NOT NULL,
    source_ref text DEFAULT ''::text NOT NULL,
    confidence double precision DEFAULT 0 NOT NULL,
    verification_status text DEFAULT 'pending'::text NOT NULL,
    last_verified_at timestamp with time zone,
    last_error text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: release_password_candidates_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.release_password_candidates_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: release_password_candidates_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.release_password_candidates_id_seq OWNED BY public.release_password_candidates.id;


--
-- Name: release_predb_matches; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.release_predb_matches (
    release_id text NOT NULL,
    predb_entry_id bigint NOT NULL,
    confidence double precision DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    chosen boolean DEFAULT false NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: release_ready_candidate_acks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.release_ready_candidate_acks (
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    key_kind text NOT NULL,
    family_key text NOT NULL,
    processed_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: release_ready_candidates; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.release_ready_candidates (
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    key_kind text NOT NULL,
    family_key text NOT NULL,
    source_release_key text DEFAULT ''::text NOT NULL,
    release_key text DEFAULT ''::text NOT NULL,
    release_name text DEFAULT ''::text NOT NULL,
    binary_count integer DEFAULT 0 NOT NULL,
    complete_binary_count integer DEFAULT 0 NOT NULL,
    complete_main_payload_binary_count integer DEFAULT 0 NOT NULL,
    expected_file_count integer DEFAULT 0 NOT NULL,
    expected_archive_file_count integer DEFAULT 0 NOT NULL,
    has_expected_file_count boolean DEFAULT false NOT NULL,
    has_expected_archive_file_count boolean DEFAULT false NOT NULL,
    expected_file_coverage_pct double precision DEFAULT 0 NOT NULL,
    archive_file_coverage_pct double precision DEFAULT 0 NOT NULL,
    total_bytes bigint DEFAULT 0 NOT NULL,
    earliest_posted_at timestamp with time zone,
    ready_reason text DEFAULT 'actionable'::text NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    source_posted_at timestamp with time zone NOT NULL
)
PARTITION BY RANGE (source_posted_at);


--
-- Name: release_ready_candidates_default; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.release_ready_candidates_default (
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    key_kind text NOT NULL,
    family_key text NOT NULL,
    source_release_key text DEFAULT ''::text NOT NULL,
    release_key text DEFAULT ''::text NOT NULL,
    release_name text DEFAULT ''::text NOT NULL,
    binary_count integer DEFAULT 0 NOT NULL,
    complete_binary_count integer DEFAULT 0 NOT NULL,
    complete_main_payload_binary_count integer DEFAULT 0 NOT NULL,
    expected_file_count integer DEFAULT 0 NOT NULL,
    expected_archive_file_count integer DEFAULT 0 NOT NULL,
    has_expected_file_count boolean DEFAULT false NOT NULL,
    has_expected_archive_file_count boolean DEFAULT false NOT NULL,
    expected_file_coverage_pct double precision DEFAULT 0 NOT NULL,
    archive_file_coverage_pct double precision DEFAULT 0 NOT NULL,
    total_bytes bigint DEFAULT 0 NOT NULL,
    earliest_posted_at timestamp with time zone,
    ready_reason text DEFAULT 'actionable'::text NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    source_posted_at timestamp with time zone NOT NULL
);



--
-- Name: release_recovered_file_set_candidates; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.release_recovered_file_set_candidates (
    provider_id bigint NOT NULL,
    file_set_key text NOT NULL,
    representative_newsgroup_id bigint DEFAULT 0 NOT NULL,
    source_release_key text DEFAULT ''::text NOT NULL,
    release_key text DEFAULT ''::text NOT NULL,
    release_name text DEFAULT ''::text NOT NULL,
    binary_count integer DEFAULT 0 NOT NULL,
    complete_binary_count integer DEFAULT 0 NOT NULL,
    complete_main_payload_binary_count integer DEFAULT 0 NOT NULL,
    expected_file_count integer DEFAULT 0 NOT NULL,
    expected_archive_file_count integer DEFAULT 0 NOT NULL,
    has_expected_file_count boolean DEFAULT false NOT NULL,
    has_expected_archive_file_count boolean DEFAULT false NOT NULL,
    total_bytes bigint DEFAULT 0 NOT NULL,
    earliest_posted_at timestamp with time zone,
    expected_file_coverage_pct double precision DEFAULT 0 NOT NULL,
    archive_file_coverage_pct double precision DEFAULT 0 NOT NULL,
    readiness_bucket text DEFAULT 'fragment_only'::text NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    source_posted_at timestamp with time zone NOT NULL
)
PARTITION BY RANGE (source_posted_at);


--
-- Name: release_recovered_file_set_candidates_default; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.release_recovered_file_set_candidates_default (
    provider_id bigint NOT NULL,
    file_set_key text NOT NULL,
    representative_newsgroup_id bigint DEFAULT 0 NOT NULL,
    source_release_key text DEFAULT ''::text NOT NULL,
    release_key text DEFAULT ''::text NOT NULL,
    release_name text DEFAULT ''::text NOT NULL,
    binary_count integer DEFAULT 0 NOT NULL,
    complete_binary_count integer DEFAULT 0 NOT NULL,
    complete_main_payload_binary_count integer DEFAULT 0 NOT NULL,
    expected_file_count integer DEFAULT 0 NOT NULL,
    expected_archive_file_count integer DEFAULT 0 NOT NULL,
    has_expected_file_count boolean DEFAULT false NOT NULL,
    has_expected_archive_file_count boolean DEFAULT false NOT NULL,
    total_bytes bigint DEFAULT 0 NOT NULL,
    earliest_posted_at timestamp with time zone,
    expected_file_coverage_pct double precision DEFAULT 0 NOT NULL,
    archive_file_coverage_pct double precision DEFAULT 0 NOT NULL,
    readiness_bucket text DEFAULT 'fragment_only'::text NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    source_posted_at timestamp with time zone NOT NULL
);


--
-- Name: release_stage_dirty_families; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.release_stage_dirty_families (
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    key_kind text NOT NULL,
    family_key text NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    source_posted_at timestamp with time zone NOT NULL
)
PARTITION BY RANGE (source_posted_at);


--
-- Name: release_stage_dirty_families_default; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.release_stage_dirty_families_default (
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    key_kind text NOT NULL,
    family_key text NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    source_posted_at timestamp with time zone NOT NULL
);


--
-- Name: release_tmdb_matches; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.release_tmdb_matches (
    release_id text NOT NULL,
    tmdb_id bigint NOT NULL,
    media_type text DEFAULT ''::text NOT NULL,
    title text DEFAULT ''::text NOT NULL,
    original_title text DEFAULT ''::text NOT NULL,
    year integer DEFAULT 0 NOT NULL,
    confidence double precision DEFAULT 0 NOT NULL,
    chosen boolean DEFAULT false NOT NULL,
    payload_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: release_tvdb_matches; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.release_tvdb_matches (
    release_id text NOT NULL,
    tvdb_id bigint NOT NULL,
    media_type text DEFAULT 'tv'::text NOT NULL,
    title text DEFAULT ''::text NOT NULL,
    original_title text DEFAULT ''::text NOT NULL,
    year integer DEFAULT 0 NOT NULL,
    confidence double precision DEFAULT 0 NOT NULL,
    chosen boolean DEFAULT false NOT NULL,
    payload_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: releases; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.releases (
    release_id text NOT NULL,
    guid text NOT NULL,
    provider_id bigint NOT NULL,
    release_key text NOT NULL,
    title text DEFAULT ''::text NOT NULL,
    search_title text DEFAULT ''::text NOT NULL,
    category text DEFAULT 'usenet'::text NOT NULL,
    poster text DEFAULT ''::text NOT NULL,
    size_bytes bigint DEFAULT 0 NOT NULL,
    posted_at timestamp with time zone,
    file_count integer DEFAULT 0 NOT NULL,
    par_file_count integer DEFAULT 0 NOT NULL,
    completion_pct double precision DEFAULT 0 NOT NULL,
    source_kind text DEFAULT 'usenet_index'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    source_title text DEFAULT ''::text NOT NULL,
    deobfuscated_title text DEFAULT ''::text NOT NULL,
    classification text DEFAULT ''::text NOT NULL,
    match_confidence double precision DEFAULT 0 NOT NULL,
    identity_status text DEFAULT 'unknown'::text NOT NULL,
    group_name text NOT NULL,
    passworded boolean DEFAULT false NOT NULL,
    passworded_known boolean DEFAULT false NOT NULL,
    passworded_unknown boolean DEFAULT false NOT NULL,
    password_state text DEFAULT 'unknown'::text NOT NULL,
    preferred_password_id bigint,
    encrypted boolean DEFAULT false NOT NULL,
    has_par2 boolean DEFAULT false NOT NULL,
    has_nfo boolean DEFAULT false NOT NULL,
    archive_count integer DEFAULT 0 NOT NULL,
    video_count integer DEFAULT 0 NOT NULL,
    audio_count integer DEFAULT 0 NOT NULL,
    sample_present boolean DEFAULT false NOT NULL,
    availability_score double precision DEFAULT 0 NOT NULL,
    availability_tier text DEFAULT 'low'::text NOT NULL,
    media_quality_score double precision DEFAULT 0 NOT NULL,
    media_quality_tier text DEFAULT 'unknown'::text NOT NULL,
    identity_confidence_score double precision DEFAULT 0 NOT NULL,
    runtime_seconds integer DEFAULT 0 NOT NULL,
    primary_resolution text DEFAULT ''::text NOT NULL,
    primary_video_codec text DEFAULT ''::text NOT NULL,
    primary_audio_codec text DEFAULT ''::text NOT NULL,
    subtitle_languages_json jsonb DEFAULT '[]'::jsonb NOT NULL,
    media_tags_json jsonb DEFAULT '[]'::jsonb NOT NULL,
    metadata_updated_at timestamp with time zone DEFAULT now() NOT NULL,
    expected_file_count integer DEFAULT 0 NOT NULL,
    expected_archive_file_count integer DEFAULT 0 NOT NULL,
    matched_media_title text DEFAULT ''::text NOT NULL,
    title_source text DEFAULT 'source'::text NOT NULL,
    title_confidence double precision DEFAULT 0 NOT NULL,
    tmdb_id bigint DEFAULT 0 NOT NULL,
    tvdb_id bigint DEFAULT 0 NOT NULL,
    external_media_type text DEFAULT ''::text NOT NULL,
    original_media_title text DEFAULT ''::text NOT NULL,
    external_year integer DEFAULT 0 NOT NULL,
    season_number integer DEFAULT 0 NOT NULL,
    episode_number integer DEFAULT 0 NOT NULL,
    season_episode_source text DEFAULT ''::text NOT NULL,
    season_episode_confidence double precision DEFAULT 0 NOT NULL,
    source_release_key text DEFAULT ''::text NOT NULL,
    release_family_key text DEFAULT ''::text NOT NULL,
    category_id integer DEFAULT 8010 NOT NULL
);


--
-- Name: scrape_checkpoints; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.scrape_checkpoints (
    id bigint NOT NULL,
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    last_article_number bigint DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    backfill_article_number bigint DEFAULT 0 NOT NULL,
    backfill_until_date timestamp with time zone,
    backfill_cutoff_reached boolean DEFAULT false NOT NULL,
    backfill_stopped_reason text DEFAULT ''::text NOT NULL
);


--
-- Name: scrape_checkpoints_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.scrape_checkpoints_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: scrape_checkpoints_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.scrape_checkpoints_id_seq OWNED BY public.scrape_checkpoints.id;


--
-- Name: scrape_runs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.scrape_runs (
    id bigint NOT NULL,
    provider_id bigint NOT NULL,
    started_at timestamp with time zone DEFAULT now() NOT NULL,
    finished_at timestamp with time zone,
    status text DEFAULT 'running'::text NOT NULL,
    error_text text DEFAULT ''::text NOT NULL
);


--
-- Name: scrape_runs_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.scrape_runs_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: scrape_runs_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.scrape_runs_id_seq OWNED BY public.scrape_runs.id;


--
-- Name: usenet_providers; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.usenet_providers (
    id bigint NOT NULL,
    provider_key text NOT NULL,
    display_name text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: usenet_providers_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.usenet_providers_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: usenet_providers_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.usenet_providers_id_seq OWNED BY public.usenet_providers.id;


--
-- Name: yenc_recovery_fairness_state; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.yenc_recovery_fairness_state (
    stage_name text NOT NULL,
    cursor_before timestamp with time zone,
    bucket_start timestamp with time zone,
    bucket_end timestamp with time zone,
    quota_percent integer DEFAULT 25 NOT NULL,
    repeat_full_count integer DEFAULT 0 NOT NULL,
    wrapped_count bigint DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: yenc_recovery_work_items; Type: TABLE; Schema: public; Owner: -
--

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
    deferred_range_id bigint,
    source_posted_at timestamp with time zone DEFAULT now() NOT NULL,
    partition_day date DEFAULT CURRENT_DATE NOT NULL
)
PARTITION BY RANGE (source_posted_at);


--
-- Name: yenc_recovery_work_items_default; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.yenc_recovery_work_items_default (
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
    deferred_range_id bigint,
    source_posted_at timestamp with time zone DEFAULT now() NOT NULL,
    partition_day date DEFAULT CURRENT_DATE NOT NULL
);


--
-- Name: article_cohort_assembly_queue_default; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_cohort_assembly_queue ATTACH PARTITION public.article_cohort_assembly_queue_default DEFAULT;


--
-- Name: article_cohort_candidates_default; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_cohort_candidates ATTACH PARTITION public.article_cohort_candidates_default DEFAULT;


--
-- Name: article_cohort_yenc_queue_default; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_cohort_yenc_queue ATTACH PARTITION public.article_cohort_yenc_queue_default DEFAULT;


--
-- Name: article_header_assembly_queue_default; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_header_assembly_queue ATTACH PARTITION public.article_header_assembly_queue_default DEFAULT;


--
-- Name: article_header_crosspost_groups_default; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_header_crosspost_groups ATTACH PARTITION public.article_header_crosspost_groups_default DEFAULT;


--
-- Name: article_header_ingest_payloads_default; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_header_ingest_payloads ATTACH PARTITION public.article_header_ingest_payloads_default DEFAULT;


--
-- Name: article_header_poster_refs_default; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_header_poster_refs ATTACH PARTITION public.article_header_poster_refs_default DEFAULT;


--
-- Name: article_headers_default; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_headers ATTACH PARTITION public.article_headers_default DEFAULT;


--
-- Name: binary_archive_entries_default; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_archive_entries ATTACH PARTITION public.binary_archive_entries_default DEFAULT;


--
-- Name: binary_completion_keys_default; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_completion_keys ATTACH PARTITION public.binary_completion_keys_default DEFAULT;


--
-- Name: binary_grouping_evidence_default; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_grouping_evidence ATTACH PARTITION public.binary_grouping_evidence_default DEFAULT;


--
-- Name: binary_identity_current_default; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_identity_current ATTACH PARTITION public.binary_identity_current_default DEFAULT;


--
-- Name: binary_inspection_artifacts_default; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_inspection_artifacts ATTACH PARTITION public.binary_inspection_artifacts_default DEFAULT;


--
-- Name: binary_inspection_ready_queue_default; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_inspection_ready_queue ATTACH PARTITION public.binary_inspection_ready_queue_default DEFAULT;


--
-- Name: binary_inspections_default; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_inspections ATTACH PARTITION public.binary_inspections_default DEFAULT;


--
-- Name: binary_lifecycle_default; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_lifecycle ATTACH PARTITION public.binary_lifecycle_default DEFAULT;


--
-- Name: binary_media_streams_default; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_media_streams ATTACH PARTITION public.binary_media_streams_default DEFAULT;


--
-- Name: binary_observation_stats_default; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_observation_stats ATTACH PARTITION public.binary_observation_stats_default DEFAULT;


--
-- Name: binary_par2_sets_default; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_par2_sets ATTACH PARTITION public.binary_par2_sets_default DEFAULT;


--
-- Name: binary_par2_targets_default; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_par2_targets ATTACH PARTITION public.binary_par2_targets_default DEFAULT;


--
-- Name: binary_parts_default; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_parts ATTACH PARTITION public.binary_parts_default DEFAULT;


--
-- Name: binary_projection_events_default; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_projection_events ATTACH PARTITION public.binary_projection_events_default DEFAULT;


--
-- Name: binary_recovery_current_default; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_recovery_current ATTACH PARTITION public.binary_recovery_current_default DEFAULT;


--
-- Name: binary_superseded_sources_default; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_superseded_sources ATTACH PARTITION public.binary_superseded_sources_default DEFAULT;


--
-- Name: binary_text_evidence_default; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_text_evidence ATTACH PARTITION public.binary_text_evidence_default DEFAULT;


--
-- Name: poster_materialization_queue_default; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.poster_materialization_queue ATTACH PARTITION public.poster_materialization_queue_default DEFAULT;


--
-- Name: release_family_readiness_summaries_default; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_family_readiness_summaries ATTACH PARTITION public.release_family_readiness_summaries_default DEFAULT;


--
-- Name: release_ready_candidates_default; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_ready_candidates ATTACH PARTITION public.release_ready_candidates_default DEFAULT;


--
-- Name: release_recovered_file_set_candidates_default; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_recovered_file_set_candidates ATTACH PARTITION public.release_recovered_file_set_candidates_default DEFAULT;


--
-- Name: release_stage_dirty_families_default; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_stage_dirty_families ATTACH PARTITION public.release_stage_dirty_families_default DEFAULT;


--
-- Name: yenc_recovery_work_items_default; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.yenc_recovery_work_items ATTACH PARTITION public.yenc_recovery_work_items_default DEFAULT;


--
-- Name: article_headers id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_headers ALTER COLUMN id SET DEFAULT nextval('public.article_headers_id_seq'::regclass);


--
-- Name: binary_archive_entries id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_archive_entries ALTER COLUMN id SET DEFAULT nextval('public.binary_archive_entries_id_seq'::regclass);


--
-- Name: binary_core binary_id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_core ALTER COLUMN binary_id SET DEFAULT nextval('public.binary_core_binary_id_seq'::regclass);


--
-- Name: binary_inspection_artifacts id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_inspection_artifacts ALTER COLUMN id SET DEFAULT nextval('public.binary_inspection_artifacts_id_seq'::regclass);


--
-- Name: binary_inspections id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_inspections ALTER COLUMN id SET DEFAULT nextval('public.binary_inspections_id_seq'::regclass);


--
-- Name: binary_media_streams id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_media_streams ALTER COLUMN id SET DEFAULT nextval('public.binary_media_streams_id_seq'::regclass);


--
-- Name: binary_par2_sets id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_par2_sets ALTER COLUMN id SET DEFAULT nextval('public.binary_par2_sets_id_seq'::regclass);


--
-- Name: binary_par2_targets id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_par2_targets ALTER COLUMN id SET DEFAULT nextval('public.binary_par2_targets_id_seq'::regclass);


--
-- Name: binary_parts id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_parts ALTER COLUMN id SET DEFAULT nextval('public.binary_parts_id_seq'::regclass);


--
-- Name: binary_projection_events id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_projection_events ALTER COLUMN id SET DEFAULT nextval('public.binary_projection_events_id_seq'::regclass);


--
-- Name: binary_text_evidence id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_text_evidence ALTER COLUMN id SET DEFAULT nextval('public.binary_text_evidence_id_seq'::regclass);


--
-- Name: deferred_article_ranges id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.deferred_article_ranges ALTER COLUMN id SET DEFAULT nextval('public.deferred_article_ranges_id_seq'::regclass);



--
-- Name: indexer_stage_runs id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.indexer_stage_runs ALTER COLUMN id SET DEFAULT nextval('public.indexer_stage_runs_id_seq'::regclass);


--
-- Name: newsgroups id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.newsgroups ALTER COLUMN id SET DEFAULT nextval('public.newsgroups_id_seq'::regclass);


--
-- Name: posters id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.posters ALTER COLUMN id SET DEFAULT nextval('public.posters_id_seq'::regclass);


--
-- Name: predb_entries id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.predb_entries ALTER COLUMN id SET DEFAULT nextval('public.predb_entries_id_seq'::regclass);


--
-- Name: release_catalog_files id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_catalog_files ALTER COLUMN id SET DEFAULT nextval('public.release_catalog_files_id_seq'::regclass);


--
-- Name: release_files id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_files ALTER COLUMN id SET DEFAULT nextval('public.release_files_id_seq'::regclass);


--
-- Name: release_password_candidates id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_password_candidates ALTER COLUMN id SET DEFAULT nextval('public.release_password_candidates_id_seq'::regclass);


--
-- Name: scrape_checkpoints id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.scrape_checkpoints ALTER COLUMN id SET DEFAULT nextval('public.scrape_checkpoints_id_seq'::regclass);


--
-- Name: scrape_runs id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.scrape_runs ALTER COLUMN id SET DEFAULT nextval('public.scrape_runs_id_seq'::regclass);


--
-- Name: usenet_providers id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.usenet_providers ALTER COLUMN id SET DEFAULT nextval('public.usenet_providers_id_seq'::regclass);


--
-- Name: article_cohort_assembly_queue article_cohort_assembly_queue_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_cohort_assembly_queue
    ADD CONSTRAINT article_cohort_assembly_queue_pkey PRIMARY KEY (source_posted_at, article_header_id);


--
-- Name: article_cohort_assembly_queue_default article_cohort_assembly_queue_default_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_cohort_assembly_queue_default
    ADD CONSTRAINT article_cohort_assembly_queue_default_pkey PRIMARY KEY (source_posted_at, article_header_id);


--
-- Name: article_cohort_candidates article_cohort_candidates_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_cohort_candidates
    ADD CONSTRAINT article_cohort_candidates_pkey PRIMARY KEY (source_posted_at, cohort_key);


--
-- Name: article_cohort_candidates_default article_cohort_candidates_default_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_cohort_candidates_default
    ADD CONSTRAINT article_cohort_candidates_default_pkey PRIMARY KEY (source_posted_at, cohort_key);


--
-- Name: article_cohort_yenc_queue article_cohort_yenc_queue_article_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_cohort_yenc_queue
    ADD CONSTRAINT article_cohort_yenc_queue_article_key UNIQUE (source_posted_at, article_header_id);


--
-- Name: article_cohort_yenc_queue_default article_cohort_yenc_queue_def_source_posted_at_article_head_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_cohort_yenc_queue_default
    ADD CONSTRAINT article_cohort_yenc_queue_def_source_posted_at_article_head_key UNIQUE (source_posted_at, article_header_id);


--
-- Name: article_cohort_yenc_queue article_cohort_yenc_queue_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_cohort_yenc_queue
    ADD CONSTRAINT article_cohort_yenc_queue_pkey PRIMARY KEY (source_posted_at, binary_id);


--
-- Name: article_cohort_yenc_queue_default article_cohort_yenc_queue_default_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_cohort_yenc_queue_default
    ADD CONSTRAINT article_cohort_yenc_queue_default_pkey PRIMARY KEY (source_posted_at, binary_id);


--
-- Name: article_header_assembly_queue article_header_assembly_queue_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_header_assembly_queue
    ADD CONSTRAINT article_header_assembly_queue_pkey PRIMARY KEY (source_posted_at, article_header_id);


--
-- Name: article_header_assembly_queue_default article_header_assembly_queue_default_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_header_assembly_queue_default
    ADD CONSTRAINT article_header_assembly_queue_default_pkey PRIMARY KEY (source_posted_at, article_header_id);


--
-- Name: article_header_crosspost_group_summary article_header_crosspost_group_summary_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_header_crosspost_group_summary
    ADD CONSTRAINT article_header_crosspost_group_summary_pkey PRIMARY KEY (observed_group_name);


--
-- Name: article_header_crosspost_groups article_header_crosspost_groups_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_header_crosspost_groups
    ADD CONSTRAINT article_header_crosspost_groups_pkey PRIMARY KEY (source_posted_at, article_header_id, observed_group_name);


--
-- Name: article_header_crosspost_groups_default article_header_crosspost_groups_default_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_header_crosspost_groups_default
    ADD CONSTRAINT article_header_crosspost_groups_default_pkey PRIMARY KEY (source_posted_at, article_header_id, observed_group_name);


--
-- Name: article_header_ingest_payloads article_header_ingest_payloads_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_header_ingest_payloads
    ADD CONSTRAINT article_header_ingest_payloads_pkey PRIMARY KEY (source_posted_at, article_header_id);


--
-- Name: article_header_ingest_payloads_default article_header_ingest_payloads_default_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_header_ingest_payloads_default
    ADD CONSTRAINT article_header_ingest_payloads_default_pkey PRIMARY KEY (source_posted_at, article_header_id);


--
-- Name: article_header_poster_refs article_header_poster_refs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_header_poster_refs
    ADD CONSTRAINT article_header_poster_refs_pkey PRIMARY KEY (source_posted_at, article_header_id);


--
-- Name: article_header_poster_refs_default article_header_poster_refs_default_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_header_poster_refs_default
    ADD CONSTRAINT article_header_poster_refs_default_pkey PRIMARY KEY (source_posted_at, article_header_id);


--
-- Name: article_headers article_headers_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_headers
    ADD CONSTRAINT article_headers_pkey PRIMARY KEY (source_posted_at, id);


--
-- Name: article_headers_default article_headers_default_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_headers_default
    ADD CONSTRAINT article_headers_default_pkey PRIMARY KEY (source_posted_at, id);


--
-- Name: article_headers article_headers_newsgroup_id_article_number_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_headers
    ADD CONSTRAINT article_headers_newsgroup_id_article_number_key UNIQUE (source_posted_at, newsgroup_id, article_number);


--
-- Name: article_headers_default article_headers_default_source_posted_at_newsgroup_id_artic_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_headers_default
    ADD CONSTRAINT article_headers_default_source_posted_at_newsgroup_id_artic_key UNIQUE (source_posted_at, newsgroup_id, article_number);


--
-- Name: article_headers article_headers_newsgroup_id_message_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_headers
    ADD CONSTRAINT article_headers_newsgroup_id_message_id_key UNIQUE (source_posted_at, newsgroup_id, message_id);


--
-- Name: article_headers_default article_headers_default_source_posted_at_newsgroup_id_messa_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_headers_default
    ADD CONSTRAINT article_headers_default_source_posted_at_newsgroup_id_messa_key UNIQUE (source_posted_at, newsgroup_id, message_id);


--
-- Name: binary_archive_entries binary_archive_entries_binary_id_entry_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_archive_entries
    ADD CONSTRAINT binary_archive_entries_binary_id_entry_name_key UNIQUE (source_posted_at, binary_id, entry_name);


--
-- Name: binary_archive_entries_default binary_archive_entries_defaul_source_posted_at_binary_id_en_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_archive_entries_default
    ADD CONSTRAINT binary_archive_entries_defaul_source_posted_at_binary_id_en_key UNIQUE (source_posted_at, binary_id, entry_name);


--
-- Name: binary_archive_entries binary_archive_entries_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_archive_entries
    ADD CONSTRAINT binary_archive_entries_pkey PRIMARY KEY (source_posted_at, id);


--
-- Name: binary_archive_entries_default binary_archive_entries_default_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_archive_entries_default
    ADD CONSTRAINT binary_archive_entries_default_pkey PRIMARY KEY (source_posted_at, id);


--
-- Name: binary_completion_keys binary_completion_keys_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_completion_keys
    ADD CONSTRAINT binary_completion_keys_pkey PRIMARY KEY (source_posted_at, binary_id);


--
-- Name: binary_completion_keys_default binary_completion_keys_default_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_completion_keys_default
    ADD CONSTRAINT binary_completion_keys_default_pkey PRIMARY KEY (source_posted_at, binary_id);


--
-- Name: binary_core binary_core_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_core
    ADD CONSTRAINT binary_core_pkey PRIMARY KEY (binary_id);


--
-- Name: binary_core binary_core_provider_id_newsgroup_id_binary_key_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_core
    ADD CONSTRAINT binary_core_provider_id_newsgroup_id_binary_key_key UNIQUE (provider_id, newsgroup_id, binary_key);


--
-- Name: binary_grouping_evidence binary_grouping_evidence_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_grouping_evidence
    ADD CONSTRAINT binary_grouping_evidence_pkey PRIMARY KEY (source_posted_at, binary_id);


--
-- Name: binary_grouping_evidence_default binary_grouping_evidence_default_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_grouping_evidence_default
    ADD CONSTRAINT binary_grouping_evidence_default_pkey PRIMARY KEY (source_posted_at, binary_id);


--
-- Name: binary_identity_current binary_identity_current_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_identity_current
    ADD CONSTRAINT binary_identity_current_pkey PRIMARY KEY (source_posted_at, binary_id);


--
-- Name: binary_identity_current_default binary_identity_current_default_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_identity_current_default
    ADD CONSTRAINT binary_identity_current_default_pkey PRIMARY KEY (source_posted_at, binary_id);


--
-- Name: binary_inspection_artifacts binary_inspection_artifacts_binary_id_stage_name_artifact_r_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_inspection_artifacts
    ADD CONSTRAINT binary_inspection_artifacts_binary_id_stage_name_artifact_r_key UNIQUE (source_posted_at, binary_id, stage_name, artifact_role, artifact_name);


--
-- Name: binary_inspection_artifacts_default binary_inspection_artifacts_d_source_posted_at_binary_id_st_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_inspection_artifacts_default
    ADD CONSTRAINT binary_inspection_artifacts_d_source_posted_at_binary_id_st_key UNIQUE (source_posted_at, binary_id, stage_name, artifact_role, artifact_name);


--
-- Name: binary_inspection_artifacts binary_inspection_artifacts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_inspection_artifacts
    ADD CONSTRAINT binary_inspection_artifacts_pkey PRIMARY KEY (source_posted_at, id);


--
-- Name: binary_inspection_artifacts_default binary_inspection_artifacts_default_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_inspection_artifacts_default
    ADD CONSTRAINT binary_inspection_artifacts_default_pkey PRIMARY KEY (source_posted_at, id);


--
-- Name: binary_inspection_ready_queue binary_inspection_ready_queue_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_inspection_ready_queue
    ADD CONSTRAINT binary_inspection_ready_queue_pkey PRIMARY KEY (source_posted_at, stage_name, binary_id);


--
-- Name: binary_inspection_ready_queue_default binary_inspection_ready_queue_default_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_inspection_ready_queue_default
    ADD CONSTRAINT binary_inspection_ready_queue_default_pkey PRIMARY KEY (source_posted_at, stage_name, binary_id);


--
-- Name: binary_inspections binary_inspections_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_inspections
    ADD CONSTRAINT binary_inspections_pkey PRIMARY KEY (source_posted_at, id);


--
-- Name: binary_inspections_default binary_inspections_default_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_inspections_default
    ADD CONSTRAINT binary_inspections_default_pkey PRIMARY KEY (source_posted_at, id);


--
-- Name: binary_inspections binary_inspections_stage_name_binary_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_inspections
    ADD CONSTRAINT binary_inspections_stage_name_binary_id_key UNIQUE (source_posted_at, stage_name, binary_id);


--
-- Name: binary_inspections_default binary_inspections_default_source_posted_at_stage_name_bina_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_inspections_default
    ADD CONSTRAINT binary_inspections_default_source_posted_at_stage_name_bina_key UNIQUE (source_posted_at, stage_name, binary_id);


--
-- Name: binary_lifecycle binary_lifecycle_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_lifecycle
    ADD CONSTRAINT binary_lifecycle_pkey PRIMARY KEY (source_posted_at, binary_id);


--
-- Name: binary_lifecycle_default binary_lifecycle_default_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_lifecycle_default
    ADD CONSTRAINT binary_lifecycle_default_pkey PRIMARY KEY (source_posted_at, binary_id);


--
-- Name: binary_media_streams binary_media_streams_binary_id_stream_index_stream_type_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_media_streams
    ADD CONSTRAINT binary_media_streams_binary_id_stream_index_stream_type_key UNIQUE (source_posted_at, binary_id, stream_index, stream_type);


--
-- Name: binary_media_streams binary_media_streams_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_media_streams
    ADD CONSTRAINT binary_media_streams_pkey PRIMARY KEY (source_posted_at, id);


--
-- Name: binary_media_streams_default binary_media_streams_default_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_media_streams_default
    ADD CONSTRAINT binary_media_streams_default_pkey PRIMARY KEY (source_posted_at, id);


--
-- Name: binary_media_streams_default binary_media_streams_default_source_posted_at_binary_id_str_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_media_streams_default
    ADD CONSTRAINT binary_media_streams_default_source_posted_at_binary_id_str_key UNIQUE (source_posted_at, binary_id, stream_index, stream_type);


--
-- Name: binary_observation_stats binary_observation_stats_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_observation_stats
    ADD CONSTRAINT binary_observation_stats_pkey PRIMARY KEY (source_posted_at, binary_id);


--
-- Name: binary_observation_stats_default binary_observation_stats_default_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_observation_stats_default
    ADD CONSTRAINT binary_observation_stats_default_pkey PRIMARY KEY (source_posted_at, binary_id);


--
-- Name: binary_par2_sets binary_par2_sets_binary_id_set_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_par2_sets
    ADD CONSTRAINT binary_par2_sets_binary_id_set_name_key UNIQUE (source_posted_at, binary_id, set_name);


--
-- Name: binary_par2_sets binary_par2_sets_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_par2_sets
    ADD CONSTRAINT binary_par2_sets_pkey PRIMARY KEY (source_posted_at, id);


--
-- Name: binary_par2_sets_default binary_par2_sets_default_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_par2_sets_default
    ADD CONSTRAINT binary_par2_sets_default_pkey PRIMARY KEY (source_posted_at, id);


--
-- Name: binary_par2_sets_default binary_par2_sets_default_source_posted_at_binary_id_set_nam_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_par2_sets_default
    ADD CONSTRAINT binary_par2_sets_default_source_posted_at_binary_id_set_nam_key UNIQUE (source_posted_at, binary_id, set_name);


--
-- Name: binary_par2_targets binary_par2_targets_binary_id_file_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_par2_targets
    ADD CONSTRAINT binary_par2_targets_binary_id_file_name_key UNIQUE (source_posted_at, binary_id, file_name);


--
-- Name: binary_par2_targets binary_par2_targets_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_par2_targets
    ADD CONSTRAINT binary_par2_targets_pkey PRIMARY KEY (source_posted_at, id);


--
-- Name: binary_par2_targets_default binary_par2_targets_default_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_par2_targets_default
    ADD CONSTRAINT binary_par2_targets_default_pkey PRIMARY KEY (source_posted_at, id);


--
-- Name: binary_par2_targets_default binary_par2_targets_default_source_posted_at_binary_id_file_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_par2_targets_default
    ADD CONSTRAINT binary_par2_targets_default_source_posted_at_binary_id_file_key UNIQUE (source_posted_at, binary_id, file_name);


--
-- Name: binary_parts binary_parts_article_header_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_parts
    ADD CONSTRAINT binary_parts_article_header_id_key UNIQUE (source_posted_at, article_header_id);


--
-- Name: binary_parts binary_parts_binary_id_part_number_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_parts
    ADD CONSTRAINT binary_parts_binary_id_part_number_key UNIQUE (source_posted_at, binary_id, part_number);


--
-- Name: binary_parts binary_parts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_parts
    ADD CONSTRAINT binary_parts_pkey PRIMARY KEY (source_posted_at, id);


--
-- Name: binary_parts_default binary_parts_default_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_parts_default
    ADD CONSTRAINT binary_parts_default_pkey PRIMARY KEY (source_posted_at, id);


--
-- Name: binary_parts_default binary_parts_default_source_posted_at_article_header_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_parts_default
    ADD CONSTRAINT binary_parts_default_source_posted_at_article_header_id_key UNIQUE (source_posted_at, article_header_id);


--
-- Name: binary_parts_default binary_parts_default_source_posted_at_binary_id_part_number_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_parts_default
    ADD CONSTRAINT binary_parts_default_source_posted_at_binary_id_part_number_key UNIQUE (source_posted_at, binary_id, part_number);


--
-- Name: binary_projection_events binary_projection_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_projection_events
    ADD CONSTRAINT binary_projection_events_pkey PRIMARY KEY (source_posted_at, id);


--
-- Name: binary_projection_events_default binary_projection_events_default_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_projection_events_default
    ADD CONSTRAINT binary_projection_events_default_pkey PRIMARY KEY (source_posted_at, id);


--
-- Name: binary_recovery_current binary_recovery_current_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_recovery_current
    ADD CONSTRAINT binary_recovery_current_pkey PRIMARY KEY (source_posted_at, binary_id);


--
-- Name: binary_recovery_current_default binary_recovery_current_default_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_recovery_current_default
    ADD CONSTRAINT binary_recovery_current_default_pkey PRIMARY KEY (source_posted_at, binary_id);


--
-- Name: binary_superseded_sources binary_superseded_sources_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_superseded_sources
    ADD CONSTRAINT binary_superseded_sources_pkey PRIMARY KEY (source_posted_at, source_binary_id);


--
-- Name: binary_superseded_sources_default binary_superseded_sources_default_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_superseded_sources_default
    ADD CONSTRAINT binary_superseded_sources_default_pkey PRIMARY KEY (source_posted_at, source_binary_id);


--
-- Name: binary_text_evidence binary_text_evidence_binary_id_stage_name_evidence_kind_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_text_evidence
    ADD CONSTRAINT binary_text_evidence_binary_id_stage_name_evidence_kind_key UNIQUE (source_posted_at, binary_id, stage_name, evidence_kind);


--
-- Name: binary_text_evidence binary_text_evidence_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_text_evidence
    ADD CONSTRAINT binary_text_evidence_pkey PRIMARY KEY (source_posted_at, id);


--
-- Name: binary_text_evidence_default binary_text_evidence_default_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_text_evidence_default
    ADD CONSTRAINT binary_text_evidence_default_pkey PRIMARY KEY (source_posted_at, id);


--
-- Name: binary_text_evidence_default binary_text_evidence_default_source_posted_at_binary_id_sta_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_text_evidence_default
    ADD CONSTRAINT binary_text_evidence_default_source_posted_at_binary_id_sta_key UNIQUE (source_posted_at, binary_id, stage_name, evidence_kind);


--
-- Name: crosspost_popularity_refresh_queue crosspost_popularity_refresh_queue_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.crosspost_popularity_refresh_queue
    ADD CONSTRAINT crosspost_popularity_refresh_queue_pkey PRIMARY KEY (observed_group_name);


--
-- Name: deferred_article_ranges deferred_article_ranges_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.deferred_article_ranges
    ADD CONSTRAINT deferred_article_ranges_pkey PRIMARY KEY (id);


--
-- Name: deferred_article_ranges deferred_article_ranges_provider_id_newsgroup_id_article_lo_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.deferred_article_ranges
    ADD CONSTRAINT deferred_article_ranges_provider_id_newsgroup_id_article_lo_key UNIQUE (provider_id, newsgroup_id, article_low, article_high);


--
-- Name: indexer_daily_bucket_stats indexer_daily_bucket_stats_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.indexer_daily_bucket_stats
    ADD CONSTRAINT indexer_daily_bucket_stats_pkey PRIMARY KEY (provider_id, newsgroup_id, bucket_day);


--
-- Name: indexer_dashboard_stats indexer_dashboard_stats_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.indexer_dashboard_stats
    ADD CONSTRAINT indexer_dashboard_stats_pkey PRIMARY KEY (stat_key);


--
-- Name: indexer_group_profiles indexer_group_profiles_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.indexer_group_profiles
    ADD CONSTRAINT indexer_group_profiles_pkey PRIMARY KEY (provider_id, newsgroup_id);


--
-- Name: indexer_nntp_runtime_snapshots indexer_nntp_runtime_snapshots_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.indexer_nntp_runtime_snapshots
    ADD CONSTRAINT indexer_nntp_runtime_snapshots_pkey PRIMARY KEY (publisher_id);



--
-- Name: indexer_provider_group_inventory indexer_provider_group_inventory_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.indexer_provider_group_inventory
    ADD CONSTRAINT indexer_provider_group_inventory_pkey PRIMARY KEY (provider_id, group_name);


--
-- Name: indexer_recovery_capacity_state indexer_recovery_capacity_state_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.indexer_recovery_capacity_state
    ADD CONSTRAINT indexer_recovery_capacity_state_pkey PRIMARY KEY (id);


--
-- Name: indexer_scrape_day_boundaries indexer_scrape_day_boundaries_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.indexer_scrape_day_boundaries
    ADD CONSTRAINT indexer_scrape_day_boundaries_pkey PRIMARY KEY (provider_id, newsgroup_id, bucket_day);


--
-- Name: indexer_stage_runs indexer_stage_runs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.indexer_stage_runs
    ADD CONSTRAINT indexer_stage_runs_pkey PRIMARY KEY (id);


--
-- Name: indexer_stage_state indexer_stage_state_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.indexer_stage_state
    ADD CONSTRAINT indexer_stage_state_pkey PRIMARY KEY (stage_name);



--
-- Name: newsgroups newsgroups_group_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.newsgroups
    ADD CONSTRAINT newsgroups_group_name_key UNIQUE (group_name);


--
-- Name: newsgroups newsgroups_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.newsgroups
    ADD CONSTRAINT newsgroups_pkey PRIMARY KEY (id);


--
-- Name: nzb_cache nzb_cache_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.nzb_cache
    ADD CONSTRAINT nzb_cache_pkey PRIMARY KEY (release_id);


--
-- Name: poster_materialization_queue poster_materialization_queue_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.poster_materialization_queue
    ADD CONSTRAINT poster_materialization_queue_pkey PRIMARY KEY (source_posted_at, article_header_id);


--
-- Name: poster_materialization_queue_default poster_materialization_queue_default_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.poster_materialization_queue_default
    ADD CONSTRAINT poster_materialization_queue_default_pkey PRIMARY KEY (source_posted_at, article_header_id);


--
-- Name: posters posters_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.posters
    ADD CONSTRAINT posters_pkey PRIMARY KEY (id);


--
-- Name: posters posters_poster_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.posters
    ADD CONSTRAINT posters_poster_name_key UNIQUE (poster_name);


--
-- Name: predb_backfill_checkpoints predb_backfill_checkpoints_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.predb_backfill_checkpoints
    ADD CONSTRAINT predb_backfill_checkpoints_pkey PRIMARY KEY (provider);


--
-- Name: predb_entries predb_entries_normalized_title_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.predb_entries
    ADD CONSTRAINT predb_entries_normalized_title_key UNIQUE (normalized_title);


--
-- Name: predb_entries predb_entries_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.predb_entries
    ADD CONSTRAINT predb_entries_pkey PRIMARY KEY (id);


--
-- Name: release_archive_detail_files release_archive_detail_files_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_archive_detail_files
    ADD CONSTRAINT release_archive_detail_files_pkey PRIMARY KEY (release_id, file_name);


--
-- Name: release_archive_detail_snapshots release_archive_detail_snapshots_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_archive_detail_snapshots
    ADD CONSTRAINT release_archive_detail_snapshots_pkey PRIMARY KEY (release_id);


--
-- Name: release_archive_detail_subtitle_languages release_archive_detail_subtitle_languages_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_archive_detail_subtitle_languages
    ADD CONSTRAINT release_archive_detail_subtitle_languages_pkey PRIMARY KEY (release_id, ordinal);


--
-- Name: release_archive_lineage_article_headers release_archive_lineage_article_headers_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_archive_lineage_article_headers
    ADD CONSTRAINT release_archive_lineage_article_headers_pkey PRIMARY KEY (release_id, article_header_id);


--
-- Name: release_archive_lineage_binaries release_archive_lineage_binaries_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_archive_lineage_binaries
    ADD CONSTRAINT release_archive_lineage_binaries_pkey PRIMARY KEY (release_id, binary_id);


--
-- Name: release_archive_state release_archive_state_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_archive_state
    ADD CONSTRAINT release_archive_state_pkey PRIMARY KEY (release_id);


--
-- Name: release_catalog_files release_catalog_files_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_catalog_files
    ADD CONSTRAINT release_catalog_files_pkey PRIMARY KEY (id);


--
-- Name: release_catalog_files release_catalog_files_release_id_file_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_catalog_files
    ADD CONSTRAINT release_catalog_files_release_id_file_name_key UNIQUE (release_id, file_name);


--
-- Name: release_family_readiness_acks release_family_readiness_acks_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_family_readiness_acks
    ADD CONSTRAINT release_family_readiness_acks_pkey PRIMARY KEY (provider_id, newsgroup_id, key_kind, family_key);


--
-- Name: release_family_readiness_summaries release_family_readiness_summaries_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_family_readiness_summaries
    ADD CONSTRAINT release_family_readiness_summaries_pkey PRIMARY KEY (source_posted_at, provider_id, newsgroup_id, key_kind, family_key);


--
-- Name: release_family_readiness_summaries_default release_family_readiness_summaries_default_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_family_readiness_summaries_default
    ADD CONSTRAINT release_family_readiness_summaries_default_pkey PRIMARY KEY (source_posted_at, provider_id, newsgroup_id, key_kind, family_key);


--
-- Name: release_files release_files_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_files
    ADD CONSTRAINT release_files_pkey PRIMARY KEY (id);


--
-- Name: release_files release_files_release_id_file_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_files
    ADD CONSTRAINT release_files_release_id_file_name_key UNIQUE (release_id, file_name);


--
-- Name: release_newsgroups release_newsgroups_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_newsgroups
    ADD CONSTRAINT release_newsgroups_pkey PRIMARY KEY (release_id, newsgroup_id);


--
-- Name: release_overrides release_overrides_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_overrides
    ADD CONSTRAINT release_overrides_pkey PRIMARY KEY (release_id);


--
-- Name: release_password_candidates release_password_candidates_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_password_candidates
    ADD CONSTRAINT release_password_candidates_pkey PRIMARY KEY (id);


--
-- Name: release_password_candidates release_password_candidates_release_id_password_value_sourc_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_password_candidates
    ADD CONSTRAINT release_password_candidates_release_id_password_value_sourc_key UNIQUE (release_id, password_value, source_kind, source_ref);


--
-- Name: release_predb_matches release_predb_matches_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_predb_matches
    ADD CONSTRAINT release_predb_matches_pkey PRIMARY KEY (release_id, predb_entry_id);


--
-- Name: release_ready_candidate_acks release_ready_candidate_acks_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_ready_candidate_acks
    ADD CONSTRAINT release_ready_candidate_acks_pkey PRIMARY KEY (provider_id, newsgroup_id, key_kind, family_key);


--
-- Name: release_ready_candidates release_ready_candidates_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_ready_candidates
    ADD CONSTRAINT release_ready_candidates_pkey PRIMARY KEY (source_posted_at, provider_id, newsgroup_id, key_kind, family_key);


--
-- Name: release_ready_candidates_default release_ready_candidates_default_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_ready_candidates_default
    ADD CONSTRAINT release_ready_candidates_default_pkey PRIMARY KEY (source_posted_at, provider_id, newsgroup_id, key_kind, family_key);



--
-- Name: release_recovered_file_set_candidates release_recovered_file_set_candidates_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_recovered_file_set_candidates
    ADD CONSTRAINT release_recovered_file_set_candidates_pkey PRIMARY KEY (source_posted_at, provider_id, file_set_key);


--
-- Name: release_recovered_file_set_candidates_default release_recovered_file_set_candidates_default_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_recovered_file_set_candidates_default
    ADD CONSTRAINT release_recovered_file_set_candidates_default_pkey PRIMARY KEY (source_posted_at, provider_id, file_set_key);


--
-- Name: release_stage_dirty_families release_stage_dirty_families_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_stage_dirty_families
    ADD CONSTRAINT release_stage_dirty_families_pkey PRIMARY KEY (source_posted_at, provider_id, newsgroup_id, key_kind, family_key);


--
-- Name: release_stage_dirty_families_default release_stage_dirty_families_default_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_stage_dirty_families_default
    ADD CONSTRAINT release_stage_dirty_families_default_pkey PRIMARY KEY (source_posted_at, provider_id, newsgroup_id, key_kind, family_key);


--
-- Name: release_tmdb_matches release_tmdb_matches_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_tmdb_matches
    ADD CONSTRAINT release_tmdb_matches_pkey PRIMARY KEY (release_id, tmdb_id, media_type);


--
-- Name: release_tvdb_matches release_tvdb_matches_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_tvdb_matches
    ADD CONSTRAINT release_tvdb_matches_pkey PRIMARY KEY (release_id, tvdb_id);


--
-- Name: releases releases_guid_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.releases
    ADD CONSTRAINT releases_guid_key UNIQUE (guid);


--
-- Name: releases releases_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.releases
    ADD CONSTRAINT releases_pkey PRIMARY KEY (release_id);


--
-- Name: scrape_checkpoints scrape_checkpoints_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.scrape_checkpoints
    ADD CONSTRAINT scrape_checkpoints_pkey PRIMARY KEY (id);


--
-- Name: scrape_checkpoints scrape_checkpoints_provider_id_newsgroup_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.scrape_checkpoints
    ADD CONSTRAINT scrape_checkpoints_provider_id_newsgroup_id_key UNIQUE (provider_id, newsgroup_id);


--
-- Name: scrape_runs scrape_runs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.scrape_runs
    ADD CONSTRAINT scrape_runs_pkey PRIMARY KEY (id);


--
-- Name: usenet_providers usenet_providers_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.usenet_providers
    ADD CONSTRAINT usenet_providers_pkey PRIMARY KEY (id);


--
-- Name: usenet_providers usenet_providers_provider_key_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.usenet_providers
    ADD CONSTRAINT usenet_providers_provider_key_key UNIQUE (provider_key);


--
-- Name: yenc_recovery_fairness_state yenc_recovery_fairness_state_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.yenc_recovery_fairness_state
    ADD CONSTRAINT yenc_recovery_fairness_state_pkey PRIMARY KEY (stage_name);


--
-- Name: yenc_recovery_work_items yenc_recovery_work_items_article_header_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.yenc_recovery_work_items
    ADD CONSTRAINT yenc_recovery_work_items_article_header_id_key UNIQUE (source_posted_at, article_header_id);


--
-- Name: yenc_recovery_work_items_default yenc_recovery_work_items_defa_source_posted_at_article_head_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.yenc_recovery_work_items_default
    ADD CONSTRAINT yenc_recovery_work_items_defa_source_posted_at_article_head_key UNIQUE (source_posted_at, article_header_id);


--
-- Name: yenc_recovery_work_items yenc_recovery_work_items_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.yenc_recovery_work_items
    ADD CONSTRAINT yenc_recovery_work_items_pkey PRIMARY KEY (source_posted_at, binary_id);


--
-- Name: yenc_recovery_work_items_default yenc_recovery_work_items_default_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.yenc_recovery_work_items_default
    ADD CONSTRAINT yenc_recovery_work_items_default_pkey PRIMARY KEY (source_posted_at, binary_id);


--
-- Name: idx_article_cohort_assembly_queue_claim_until; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_article_cohort_assembly_queue_claim_until ON ONLY public.article_cohort_assembly_queue USING btree (claim_until, priority_rank, source_posted_at, article_header_id) WHERE (status = ANY (ARRAY['ready'::text, 'running'::text]));


--
-- Name: article_cohort_assembly_queue_claim_until_priority_rank_sou_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX article_cohort_assembly_queue_claim_until_priority_rank_sou_idx ON public.article_cohort_assembly_queue_default USING btree (claim_until, priority_rank, source_posted_at, article_header_id) WHERE (status = ANY (ARRAY['ready'::text, 'running'::text]));


--
-- Name: idx_article_cohort_assembly_queue_claim; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_article_cohort_assembly_queue_claim ON ONLY public.article_cohort_assembly_queue USING btree (status, priority_rank, score DESC, source_posted_at DESC, article_header_id DESC);


--
-- Name: article_cohort_assembly_queue_status_priority_rank_score_so_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX article_cohort_assembly_queue_status_priority_rank_score_so_idx ON public.article_cohort_assembly_queue_default USING btree (status, priority_rank, score DESC, source_posted_at DESC, article_header_id DESC);


--
-- Name: idx_article_cohort_candidates_lookup; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_article_cohort_candidates_lookup ON ONLY public.article_cohort_candidates USING btree (provider_id, newsgroup_id, cohort_kind, bucket_start DESC, source_posted_at, cohort_key);


--
-- Name: article_cohort_candidates_def_provider_id_newsgroup_id_coho_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX article_cohort_candidates_def_provider_id_newsgroup_id_coho_idx ON public.article_cohort_candidates_default USING btree (provider_id, newsgroup_id, cohort_kind, bucket_start DESC, source_posted_at, cohort_key);


--
-- Name: idx_article_cohort_candidates_ready; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_article_cohort_candidates_ready ON ONLY public.article_cohort_candidates USING btree (status, priority_rank, score DESC, source_posted_at DESC, cohort_key) WHERE (status = ANY (ARRAY['ready'::text, 'active'::text]));


--
-- Name: article_cohort_candidates_def_status_priority_rank_score_so_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX article_cohort_candidates_def_status_priority_rank_score_so_idx ON public.article_cohort_candidates_default USING btree (status, priority_rank, score DESC, source_posted_at DESC, cohort_key) WHERE (status = ANY (ARRAY['ready'::text, 'active'::text]));


--
-- Name: idx_article_cohort_yenc_queue_cohort; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_article_cohort_yenc_queue_cohort ON ONLY public.article_cohort_yenc_queue USING btree (cohort_key, status, source_posted_at, binary_id);


--
-- Name: article_cohort_yenc_queue_def_cohort_key_status_source_post_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX article_cohort_yenc_queue_def_cohort_key_status_source_post_idx ON public.article_cohort_yenc_queue_default USING btree (cohort_key, status, source_posted_at, binary_id);


--
-- Name: idx_article_cohort_yenc_queue_ready; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_article_cohort_yenc_queue_ready ON ONLY public.article_cohort_yenc_queue USING btree (status, priority_rank, score DESC, source_posted_at DESC, binary_id) WHERE (status = 'ready'::text);


--
-- Name: article_cohort_yenc_queue_def_status_priority_rank_score_so_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX article_cohort_yenc_queue_def_status_priority_rank_score_so_idx ON public.article_cohort_yenc_queue_default USING btree (status, priority_rank, score DESC, source_posted_at DESC, binary_id) WHERE (status = 'ready'::text);


--
-- Name: idx_article_assembly_queue_general_lane_claim; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_article_assembly_queue_general_lane_claim ON ONLY public.article_header_assembly_queue USING btree (article_header_id DESC, source_posted_at, claim_until) WHERE (queue_kind <> 'structured'::text);


--
-- Name: article_header_assembly_queu_article_header_id_source_po_idx127; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX article_header_assembly_queu_article_header_id_source_po_idx127 ON public.article_header_assembly_queue_default USING btree (article_header_id DESC, source_posted_at, claim_until) WHERE (queue_kind <> 'structured'::text);


--
-- Name: idx_article_assembly_queue_recent_claimable; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_article_assembly_queue_recent_claimable ON ONLY public.article_header_assembly_queue USING btree (article_header_id DESC, source_posted_at) WHERE (normalized_file_name <> ''::text);


--
-- Name: article_header_assembly_queu_article_header_id_source_pos_idx31; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX article_header_assembly_queu_article_header_id_source_pos_idx31 ON public.article_header_assembly_queue_default USING btree (article_header_id DESC, source_posted_at) WHERE (normalized_file_name <> ''::text);


--
-- Name: idx_article_assembly_queue_general_claimable; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_article_assembly_queue_general_claimable ON ONLY public.article_header_assembly_queue USING btree (article_header_id DESC, source_posted_at, claim_until);


--
-- Name: article_header_assembly_queu_article_header_id_source_pos_idx63; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX article_header_assembly_queu_article_header_id_source_pos_idx63 ON public.article_header_assembly_queue_default USING btree (article_header_id DESC, source_posted_at, claim_until);


--
-- Name: idx_article_assembly_queue_structured_lane_claim; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_article_assembly_queue_structured_lane_claim ON ONLY public.article_header_assembly_queue USING btree (article_header_id DESC, source_posted_at, claim_until) WHERE (queue_kind = 'structured'::text);


--
-- Name: article_header_assembly_queu_article_header_id_source_pos_idx95; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX article_header_assembly_queu_article_header_id_source_pos_idx95 ON public.article_header_assembly_queue_default USING btree (article_header_id DESC, source_posted_at, claim_until) WHERE (queue_kind = 'structured'::text);


--
-- Name: idx_article_assembly_queue_structured_lookup; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_article_assembly_queue_structured_lookup ON ONLY public.article_header_assembly_queue USING btree (provider_id, newsgroup_id, normalized_file_name, article_header_id DESC, source_posted_at) WHERE (normalized_file_name <> ''::text);


--
-- Name: article_header_assembly_queu_provider_id_newsgroup_id_nor_idx31; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX article_header_assembly_queu_provider_id_newsgroup_id_nor_idx31 ON public.article_header_assembly_queue_default USING btree (provider_id, newsgroup_id, normalized_file_name, article_header_id DESC, source_posted_at) WHERE (normalized_file_name <> ''::text);


--
-- Name: idx_article_assembly_queue_claim; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_article_assembly_queue_claim ON ONLY public.article_header_assembly_queue USING btree (claim_until, queued_at, article_header_id);


--
-- Name: article_header_assembly_queue_claim_until_queued_at_article_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX article_header_assembly_queue_claim_until_queued_at_article_idx ON public.article_header_assembly_queue_default USING btree (claim_until, queued_at, article_header_id);


--
-- Name: idx_article_header_assembly_queue_source_posted; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_article_header_assembly_queue_source_posted ON ONLY public.article_header_assembly_queue USING btree (source_posted_at, claim_until, article_header_id);


--
-- Name: article_header_assembly_queue_source_posted_at_claim_until__idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX article_header_assembly_queue_source_posted_at_claim_until__idx ON public.article_header_assembly_queue_default USING btree (source_posted_at, claim_until, article_header_id);


--
-- Name: idx_article_header_crosspost_groups_refresh_lookup; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_article_header_crosspost_groups_refresh_lookup ON ONLY public.article_header_crosspost_groups USING btree (observed_group_name, article_header_id, source_posted_at) WHERE (btrim(observed_group_name) <> ''::text);


--
-- Name: article_header_crosspost_gro_observed_group_name_article__idx31; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX article_header_crosspost_gro_observed_group_name_article__idx31 ON public.article_header_crosspost_groups_default USING btree (observed_group_name, article_header_id, source_posted_at) WHERE (btrim(observed_group_name) <> ''::text);


--
-- Name: idx_article_headers_id_source_posted; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_article_headers_id_source_posted ON ONLY public.article_headers USING btree (id, source_posted_at);


--
-- Name: article_headers_default_id_source_posted_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX article_headers_default_id_source_posted_at_idx ON public.article_headers_default USING btree (id, source_posted_at);


--
-- Name: idx_article_headers_provider_group_article_desc; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_article_headers_provider_group_article_desc ON ONLY public.article_headers USING btree (provider_id, newsgroup_id, article_number DESC);


--
-- Name: article_headers_default_provider_id_newsgroup_id_article_nu_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX article_headers_default_provider_id_newsgroup_id_article_nu_idx ON public.article_headers_default USING btree (provider_id, newsgroup_id, article_number DESC);


--
-- Name: idx_article_headers_provider_group_date_article; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_article_headers_provider_group_date_article ON ONLY public.article_headers USING btree (provider_id, newsgroup_id, date_utc, article_number);


--
-- Name: article_headers_default_provider_id_newsgroup_id_date_utc_a_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX article_headers_default_provider_id_newsgroup_id_date_utc_a_idx ON public.article_headers_default USING btree (provider_id, newsgroup_id, date_utc, article_number);


--
-- Name: idx_article_headers_source_posted_group_article; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_article_headers_source_posted_group_article ON ONLY public.article_headers USING btree (source_posted_at, provider_id, newsgroup_id, article_number);


--
-- Name: article_headers_default_source_posted_at_provider_id_newsgr_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX article_headers_default_source_posted_at_provider_id_newsgr_idx ON public.article_headers_default USING btree (source_posted_at, provider_id, newsgroup_id, article_number);


--
-- Name: idx_binary_archive_entries_source_posted; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_archive_entries_source_posted ON ONLY public.binary_archive_entries USING btree (source_posted_at, binary_id);


--
-- Name: binary_archive_entries_default_source_posted_at_binary_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_archive_entries_default_source_posted_at_binary_id_idx ON public.binary_archive_entries_default USING btree (source_posted_at, binary_id);


--
-- Name: idx_binary_completion_keys_filename_lookup; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_completion_keys_filename_lookup ON ONLY public.binary_completion_keys USING btree (provider_id, newsgroup_id, normalized_file_name, source_posted_at, binary_id) INCLUDE (is_main_payload, observed_parts, completion_ratio, posted_at);


--
-- Name: binary_completion_keys_defaul_provider_id_newsgroup_id_norm_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_completion_keys_defaul_provider_id_newsgroup_id_norm_idx ON public.binary_completion_keys_default USING btree (provider_id, newsgroup_id, normalized_file_name, source_posted_at, binary_id) INCLUDE (is_main_payload, observed_parts, completion_ratio, posted_at);


--
-- Name: idx_binary_completion_keys_rank; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_completion_keys_rank ON ONLY public.binary_completion_keys USING btree (source_posted_at, is_main_payload DESC, completion_ratio DESC, observed_parts DESC, binary_id DESC);


--
-- Name: binary_completion_keys_defaul_source_posted_at_is_main_payl_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_completion_keys_defaul_source_posted_at_is_main_payl_idx ON public.binary_completion_keys_default USING btree (source_posted_at, is_main_payload DESC, completion_ratio DESC, observed_parts DESC, binary_id DESC);


--
-- Name: idx_binary_completion_keys_match; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_completion_keys_match ON ONLY public.binary_completion_keys USING btree (source_posted_at, provider_id, newsgroup_id, normalized_file_name, is_main_payload DESC, observed_parts DESC, binary_id DESC);


--
-- Name: binary_completion_keys_defaul_source_posted_at_provider_id__idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_completion_keys_defaul_source_posted_at_provider_id__idx ON public.binary_completion_keys_default USING btree (source_posted_at, provider_id, newsgroup_id, normalized_file_name, is_main_payload DESC, observed_parts DESC, binary_id DESC);


--
-- Name: idx_binary_completion_keys_match_rank; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_completion_keys_match_rank ON ONLY public.binary_completion_keys USING btree (source_posted_at, provider_id, newsgroup_id, normalized_file_name, is_main_payload DESC, completion_ratio DESC, observed_parts DESC, binary_id DESC) INCLUDE (posted_at);


--
-- Name: binary_completion_keys_defaul_source_posted_at_provider_id_idx1; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_completion_keys_defaul_source_posted_at_provider_id_idx1 ON public.binary_completion_keys_default USING btree (source_posted_at, provider_id, newsgroup_id, normalized_file_name, is_main_payload DESC, completion_ratio DESC, observed_parts DESC, binary_id DESC) INCLUDE (posted_at);


--
-- Name: idx_binary_grouping_evidence_source_posted; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_grouping_evidence_source_posted ON ONLY public.binary_grouping_evidence USING btree (source_posted_at, binary_id);


--
-- Name: binary_grouping_evidence_default_source_posted_at_binary_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_grouping_evidence_default_source_posted_at_binary_id_idx ON public.binary_grouping_evidence_default USING btree (source_posted_at, binary_id);


--
-- Name: idx_binary_identity_strength_updated; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_identity_strength_updated ON ONLY public.binary_identity_current USING btree (lower(COALESCE(identity_strength, ''::text)), updated_at DESC, binary_id DESC, source_posted_at);


--
-- Name: binary_identity_current_defau_lower_updated_at_binary_id_so_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_identity_current_defau_lower_updated_at_binary_id_so_idx ON public.binary_identity_current_default USING btree (lower(COALESCE(identity_strength, ''::text)), updated_at DESC, binary_id DESC, source_posted_at);


--
-- Name: idx_binary_identity_file_set_lookup; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_identity_file_set_lookup ON ONLY public.binary_identity_current USING btree (provider_id, file_set_key, source_posted_at, binary_id) WHERE (btrim(file_set_key) <> ''::text);


--
-- Name: binary_identity_current_defau_provider_id_file_set_key_sour_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_identity_current_defau_provider_id_file_set_key_sour_idx ON public.binary_identity_current_default USING btree (provider_id, file_set_key, source_posted_at, binary_id) WHERE (btrim(file_set_key) <> ''::text);


--
-- Name: idx_binary_identity_opaque_subject_cohort; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_identity_opaque_subject_cohort ON ONLY public.binary_identity_current USING btree (provider_id, newsgroup_id, identity_reason, family_kind, identity_strength, binary_id) WHERE ((family_kind = 'opaque_set'::text) AND (identity_reason = 'opaque_subject_set'::text) AND (is_main_payload = true));


--
-- Name: binary_identity_current_defau_provider_id_newsgroup_id_iden_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_identity_current_defau_provider_id_newsgroup_id_iden_idx ON public.binary_identity_current_default USING btree (provider_id, newsgroup_id, identity_reason, family_kind, identity_strength, binary_id) WHERE ((family_kind = 'opaque_set'::text) AND (identity_reason = 'opaque_subject_set'::text) AND (is_main_payload = true));


--
-- Name: idx_binary_identity_subject_regroup_lookup; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_identity_subject_regroup_lookup ON ONLY public.binary_identity_current USING btree (provider_id, newsgroup_id, lower(btrim(file_name)), source_posted_at, binary_id) WHERE ((family_kind = 'contextual_obfuscated'::text) AND (identity_reason = 'contextual_fallback'::text) AND (btrim(COALESCE(file_name, ''::text)) <> ''::text));


--
-- Name: binary_identity_current_defau_provider_id_newsgroup_id_low_idx1; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_identity_current_defau_provider_id_newsgroup_id_low_idx1 ON public.binary_identity_current_default USING btree (provider_id, newsgroup_id, lower(btrim(file_name)), source_posted_at, binary_id) WHERE ((family_kind = 'contextual_obfuscated'::text) AND (identity_reason = 'contextual_fallback'::text) AND (btrim(COALESCE(file_name, ''::text)) <> ''::text));


--
-- Name: idx_binary_identity_base_stem_lookup; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_identity_base_stem_lookup ON ONLY public.binary_identity_current USING btree (provider_id, newsgroup_id, lower(btrim(base_stem)), source_posted_at, binary_id) WHERE ((btrim(base_stem) <> ''::text) AND (GREATEST(expected_file_count, expected_archive_file_count) > 1));


--
-- Name: binary_identity_current_defau_provider_id_newsgroup_id_lowe_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_identity_current_defau_provider_id_newsgroup_id_lowe_idx ON public.binary_identity_current_default USING btree (provider_id, newsgroup_id, lower(btrim(base_stem)), source_posted_at, binary_id) WHERE ((btrim(base_stem) <> ''::text) AND (GREATEST(expected_file_count, expected_archive_file_count) > 1));


--
-- Name: idx_binary_identity_release_family_lookup; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_identity_release_family_lookup ON ONLY public.binary_identity_current USING btree (provider_id, newsgroup_id, release_family_key, source_posted_at, binary_id) WHERE (btrim(release_family_key) <> ''::text);


--
-- Name: binary_identity_current_defau_provider_id_newsgroup_id_rele_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_identity_current_defau_provider_id_newsgroup_id_rele_idx ON public.binary_identity_current_default USING btree (provider_id, newsgroup_id, release_family_key, source_posted_at, binary_id) WHERE (btrim(release_family_key) <> ''::text);


--
-- Name: idx_binary_identity_subject_regroup_candidates; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_identity_subject_regroup_candidates ON ONLY public.binary_identity_current USING btree (source_posted_at DESC, binary_id DESC) INCLUDE (provider_id, newsgroup_id, release_family_key, base_stem, source_release_key, file_name, expected_file_count) WHERE ((family_kind = 'contextual_obfuscated'::text) AND (identity_reason = 'contextual_fallback'::text) AND (btrim(COALESCE(file_name, ''::text)) <> ''::text));


--
-- Name: binary_identity_current_defau_source_posted_at_binary_id_pr_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_identity_current_defau_source_posted_at_binary_id_pr_idx ON public.binary_identity_current_default USING btree (source_posted_at DESC, binary_id DESC) INCLUDE (provider_id, newsgroup_id, release_family_key, base_stem, source_release_key, file_name, expected_file_count) WHERE ((family_kind = 'contextual_obfuscated'::text) AND (identity_reason = 'contextual_fallback'::text) AND (btrim(COALESCE(file_name, ''::text)) <> ''::text));


--
-- Name: idx_binary_identity_current_source_posted; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_identity_current_source_posted ON ONLY public.binary_identity_current USING btree (source_posted_at, provider_id, newsgroup_id, binary_id);


--
-- Name: binary_identity_current_defau_source_posted_at_provider_id__idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_identity_current_defau_source_posted_at_provider_id__idx ON public.binary_identity_current_default USING btree (source_posted_at, provider_id, newsgroup_id, binary_id);


--
-- Name: idx_binary_identity_release_family; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_identity_release_family ON ONLY public.binary_identity_current USING btree (source_posted_at, provider_id, newsgroup_id, release_family_key);


--
-- Name: binary_identity_current_defau_source_posted_at_provider_id_idx1; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_identity_current_defau_source_posted_at_provider_id_idx1 ON public.binary_identity_current_default USING btree (source_posted_at, provider_id, newsgroup_id, release_family_key);


--
-- Name: idx_binary_identity_file_set; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_identity_file_set ON ONLY public.binary_identity_current USING btree (source_posted_at, provider_id, file_set_key, newsgroup_id) WHERE (btrim(file_set_key) <> ''::text);


--
-- Name: binary_identity_current_defau_source_posted_at_provider_id_idx2; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_identity_current_defau_source_posted_at_provider_id_idx2 ON public.binary_identity_current_default USING btree (source_posted_at, provider_id, file_set_key, newsgroup_id) WHERE (btrim(file_set_key) <> ''::text);


--
-- Name: idx_binary_identity_inspect_par2_backlog; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_identity_inspect_par2_backlog ON ONLY public.binary_identity_current USING btree (source_posted_at, updated_at DESC, binary_id DESC) INCLUDE (release_family_key, release_name, binary_name, file_name, match_confidence) WHERE (lower(COALESCE(NULLIF(file_name, ''::text), NULLIF(binary_name, ''::text), ''::text)) ~~ '%.par2'::text);


--
-- Name: binary_identity_current_defau_source_posted_at_updated_at__idx1; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_identity_current_defau_source_posted_at_updated_at__idx1 ON public.binary_identity_current_default USING btree (source_posted_at, updated_at DESC, binary_id DESC) INCLUDE (release_family_key, release_name, binary_name, file_name, match_confidence) WHERE (lower(COALESCE(NULLIF(file_name, ''::text), NULLIF(binary_name, ''::text), ''::text)) ~~ '%.par2'::text);


--
-- Name: idx_binary_identity_inspect_discovery_backlog; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_identity_inspect_discovery_backlog ON ONLY public.binary_identity_current USING btree (source_posted_at, updated_at DESC, binary_id DESC) INCLUDE (release_family_key, base_stem, release_name, binary_name, file_name, file_index, expected_file_count, expected_archive_file_count, is_auxiliary, is_main_payload, match_confidence, match_status) WHERE (((is_main_payload = true) OR (is_auxiliary = false)) AND ((lower(COALESCE(NULLIF(file_name, ''::text), NULLIF(binary_name, ''::text), ''::text)) ~~ '%.bin'::text) OR (COALESCE(NULLIF(file_name, ''::text), NULLIF(binary_name, ''::text), ''::text) !~ '\.[A-Za-z0-9]{1,8}$'::text)));


--
-- Name: binary_identity_current_defau_source_posted_at_updated_at_b_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_identity_current_defau_source_posted_at_updated_at_b_idx ON public.binary_identity_current_default USING btree (source_posted_at, updated_at DESC, binary_id DESC) INCLUDE (release_family_key, base_stem, release_name, binary_name, file_name, file_index, expected_file_count, expected_archive_file_count, is_auxiliary, is_main_payload, match_confidence, match_status) WHERE (((is_main_payload = true) OR (is_auxiliary = false)) AND ((lower(COALESCE(NULLIF(file_name, ''::text), NULLIF(binary_name, ''::text), ''::text)) ~~ '%.bin'::text) OR (COALESCE(NULLIF(file_name, ''::text), NULLIF(binary_name, ''::text), ''::text) !~ '\.[A-Za-z0-9]{1,8}$'::text)));


--
-- Name: idx_binary_identity_subject_multipart_stale; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_identity_subject_multipart_stale ON ONLY public.binary_identity_current USING btree (source_posted_at DESC, binary_id) WHERE (identity_reason = 'subject_multipart_obfuscated'::text);


--
-- Name: binary_identity_current_default_source_posted_at_binary_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_identity_current_default_source_posted_at_binary_id_idx ON public.binary_identity_current_default USING btree (source_posted_at DESC, binary_id) WHERE (identity_reason = 'subject_multipart_obfuscated'::text);


--
-- Name: idx_binary_inspection_artifacts_source_posted; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_inspection_artifacts_source_posted ON ONLY public.binary_inspection_artifacts USING btree (source_posted_at, stage_name, binary_id);


--
-- Name: binary_inspection_artifacts_d_source_posted_at_stage_name_b_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_inspection_artifacts_d_source_posted_at_stage_name_b_idx ON public.binary_inspection_artifacts_default USING btree (source_posted_at, stage_name, binary_id);


--
-- Name: idx_binary_inspection_ready_queue_source_posted; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_inspection_ready_queue_source_posted ON ONLY public.binary_inspection_ready_queue USING btree (source_posted_at, stage_name, status, binary_id);


--
-- Name: binary_inspection_ready_queue_source_posted_at_stage_name_s_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_inspection_ready_queue_source_posted_at_stage_name_s_idx ON public.binary_inspection_ready_queue_default USING btree (source_posted_at, stage_name, status, binary_id);


--
-- Name: idx_binary_inspections_release_status_lookup; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_inspections_release_status_lookup ON ONLY public.binary_inspections USING btree (release_id, status, updated_at DESC, source_posted_at) WHERE (release_id IS NOT NULL);


--
-- Name: binary_inspections_default_release_id_status_updated_at_sou_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_inspections_default_release_id_status_updated_at_sou_idx ON public.binary_inspections_default USING btree (release_id, status, updated_at DESC, source_posted_at) WHERE (release_id IS NOT NULL);


--
-- Name: idx_binary_inspections_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_inspections_status ON ONLY public.binary_inspections USING btree (source_posted_at, stage_name, status, updated_at DESC);


--
-- Name: binary_inspections_default_source_posted_at_stage_name_sta_idx1; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_inspections_default_source_posted_at_stage_name_sta_idx1 ON public.binary_inspections_default USING btree (source_posted_at, stage_name, status, updated_at DESC);


--
-- Name: idx_binary_inspections_source_posted; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_inspections_source_posted ON ONLY public.binary_inspections USING btree (source_posted_at, stage_name, status, binary_id);


--
-- Name: binary_inspections_default_source_posted_at_stage_name_stat_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_inspections_default_source_posted_at_stage_name_stat_idx ON public.binary_inspections_default USING btree (source_posted_at, stage_name, status, binary_id);


--
-- Name: idx_binary_lifecycle_status_lookup; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_lifecycle_status_lookup ON ONLY public.binary_lifecycle USING btree (source_posted_at, binary_id, lifecycle_status);


--
-- Name: binary_lifecycle_default_source_posted_at_binary_id_lifecyc_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_lifecycle_default_source_posted_at_binary_id_lifecyc_idx ON public.binary_lifecycle_default USING btree (source_posted_at, binary_id, lifecycle_status);


--
-- Name: idx_binary_lifecycle_source_posted; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_lifecycle_source_posted ON ONLY public.binary_lifecycle USING btree (source_posted_at, provider_id, newsgroup_id, binary_id);


--
-- Name: binary_lifecycle_default_source_posted_at_provider_id_newsg_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_lifecycle_default_source_posted_at_provider_id_newsg_idx ON public.binary_lifecycle_default USING btree (source_posted_at, provider_id, newsgroup_id, binary_id);


--
-- Name: idx_binary_lifecycle_release; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_lifecycle_release ON ONLY public.binary_lifecycle USING btree (source_posted_at, release_id, lifecycle_status);


--
-- Name: binary_lifecycle_default_source_posted_at_release_id_lifecy_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_lifecycle_default_source_posted_at_release_id_lifecy_idx ON public.binary_lifecycle_default USING btree (source_posted_at, release_id, lifecycle_status);


--
-- Name: idx_binary_media_streams_source_posted; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_media_streams_source_posted ON ONLY public.binary_media_streams USING btree (source_posted_at, binary_id);


--
-- Name: binary_media_streams_default_source_posted_at_binary_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_media_streams_default_source_posted_at_binary_id_idx ON public.binary_media_streams_default USING btree (source_posted_at, binary_id);


--
-- Name: idx_binary_observation_stats_opaque_posted_admission; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_observation_stats_opaque_posted_admission ON ONLY public.binary_observation_stats USING btree (posted_at DESC, source_posted_at, provider_id, newsgroup_id, binary_id) INCLUDE (total_bytes, updated_at) WHERE ((total_parts <= 1) AND (observed_parts <= 1) AND (posted_at IS NOT NULL));


--
-- Name: binary_observation_stats_defa_posted_at_source_posted_at_pr_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_observation_stats_defa_posted_at_source_posted_at_pr_idx ON public.binary_observation_stats_default USING btree (posted_at DESC, source_posted_at, provider_id, newsgroup_id, binary_id) INCLUDE (total_bytes, updated_at) WHERE ((total_parts <= 1) AND (observed_parts <= 1) AND (posted_at IS NOT NULL));


--
-- Name: idx_binary_observation_stats_posted_cohort; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_observation_stats_posted_cohort ON ONLY public.binary_observation_stats USING btree (provider_id, newsgroup_id, posted_at DESC, binary_id) WHERE ((total_parts <= 1) AND (observed_parts <= 1));


--
-- Name: binary_observation_stats_defa_provider_id_newsgroup_id_post_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_observation_stats_defa_provider_id_newsgroup_id_post_idx ON public.binary_observation_stats_default USING btree (provider_id, newsgroup_id, posted_at DESC, binary_id) WHERE ((total_parts <= 1) AND (observed_parts <= 1));


--
-- Name: idx_binary_observation_incomplete_rank; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_observation_incomplete_rank ON ONLY public.binary_observation_stats USING btree (source_posted_at, observed_parts DESC, binary_id DESC) INCLUDE (provider_id, newsgroup_id, total_parts) WHERE ((total_parts > 0) AND (observed_parts < total_parts));


--
-- Name: binary_observation_stats_defa_source_posted_at_observed_par_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_observation_stats_defa_source_posted_at_observed_par_idx ON public.binary_observation_stats_default USING btree (source_posted_at, observed_parts DESC, binary_id DESC) INCLUDE (provider_id, newsgroup_id, total_parts) WHERE ((total_parts > 0) AND (observed_parts < total_parts));


--
-- Name: idx_binary_observation_stats_source_posted; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_observation_stats_source_posted ON ONLY public.binary_observation_stats USING btree (source_posted_at, provider_id, newsgroup_id, binary_id);


--
-- Name: binary_observation_stats_defa_source_posted_at_provider_id__idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_observation_stats_defa_source_posted_at_provider_id__idx ON public.binary_observation_stats_default USING btree (source_posted_at, provider_id, newsgroup_id, binary_id);


--
-- Name: idx_binary_observation_completeness; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_observation_completeness ON ONLY public.binary_observation_stats USING btree (source_posted_at, provider_id, newsgroup_id, observed_parts, total_parts);


--
-- Name: binary_observation_stats_defa_source_posted_at_provider_id_idx1; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_observation_stats_defa_source_posted_at_provider_id_idx1 ON public.binary_observation_stats_default USING btree (source_posted_at, provider_id, newsgroup_id, observed_parts, total_parts);


--
-- Name: idx_binary_observation_stats_singleton_updated; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_observation_stats_singleton_updated ON ONLY public.binary_observation_stats USING btree (updated_at DESC, binary_id DESC) INCLUDE (source_posted_at, provider_id, newsgroup_id, posted_at, total_bytes) WHERE ((total_parts <= 1) AND (observed_parts <= 1) AND (posted_at IS NOT NULL));


--
-- Name: binary_observation_stats_defa_updated_at_binary_id_source_p_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_observation_stats_defa_updated_at_binary_id_source_p_idx ON public.binary_observation_stats_default USING btree (updated_at DESC, binary_id DESC) INCLUDE (source_posted_at, provider_id, newsgroup_id, posted_at, total_bytes) WHERE ((total_parts <= 1) AND (observed_parts <= 1) AND (posted_at IS NOT NULL));


--
-- Name: idx_binary_observation_stats_incomplete_updated; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_observation_stats_incomplete_updated ON ONLY public.binary_observation_stats USING btree (updated_at DESC, binary_id) WHERE ((total_parts > 0) AND (observed_parts < total_parts));


--
-- Name: binary_observation_stats_default_updated_at_binary_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_observation_stats_default_updated_at_binary_id_idx ON public.binary_observation_stats_default USING btree (updated_at DESC, binary_id) WHERE ((total_parts > 0) AND (observed_parts < total_parts));


--
-- Name: idx_binary_par2_sets_source_posted; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_par2_sets_source_posted ON ONLY public.binary_par2_sets USING btree (source_posted_at, binary_id);


--
-- Name: binary_par2_sets_default_source_posted_at_binary_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_par2_sets_default_source_posted_at_binary_id_idx ON public.binary_par2_sets_default USING btree (source_posted_at, binary_id);


--
-- Name: idx_binary_par2_targets_source_posted; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_par2_targets_source_posted ON ONLY public.binary_par2_targets USING btree (source_posted_at, binary_id);


--
-- Name: binary_par2_targets_default_source_posted_at_binary_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_par2_targets_default_source_posted_at_binary_id_idx ON public.binary_par2_targets_default USING btree (source_posted_at, binary_id);


--
-- Name: idx_binary_parts_article_header_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_parts_article_header_id ON ONLY public.binary_parts USING btree (article_header_id);


--
-- Name: binary_parts_default_article_header_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_parts_default_article_header_id_idx ON public.binary_parts_default USING btree (article_header_id);


--
-- Name: idx_binary_parts_binary_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_parts_binary_id ON ONLY public.binary_parts USING btree (binary_id);


--
-- Name: binary_parts_default_binary_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_parts_default_binary_id_idx ON public.binary_parts_default USING btree (binary_id);


--
-- Name: idx_binary_parts_binary_source_part; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_parts_binary_source_part ON ONLY public.binary_parts USING btree (binary_id, source_posted_at, part_number);


--
-- Name: binary_parts_default_binary_id_source_posted_at_part_number_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_parts_default_binary_id_source_posted_at_part_number_idx ON public.binary_parts_default USING btree (binary_id, source_posted_at, part_number);


--
-- Name: idx_binary_parts_source_posted; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_parts_source_posted ON ONLY public.binary_parts USING btree (source_posted_at, binary_id, article_header_id);


--
-- Name: binary_parts_default_source_posted_at_binary_id_article_hea_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_parts_default_source_posted_at_binary_id_article_hea_idx ON public.binary_parts_default USING btree (source_posted_at, binary_id, article_header_id);


--
-- Name: idx_binary_projection_events_source_posted; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_projection_events_source_posted ON ONLY public.binary_projection_events USING btree (source_posted_at, event_stage, event_kind, binary_id);


--
-- Name: binary_projection_events_defa_source_posted_at_event_stage__idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_projection_events_defa_source_posted_at_event_stage__idx ON public.binary_projection_events_default USING btree (source_posted_at, event_stage, event_kind, binary_id);


--
-- Name: idx_binary_projection_events_stage; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_projection_events_stage ON ONLY public.binary_projection_events USING btree (source_posted_at, event_stage, event_kind, created_at DESC);


--
-- Name: binary_projection_events_defa_source_posted_at_event_stage_idx1; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_projection_events_defa_source_posted_at_event_stage_idx1 ON public.binary_projection_events_default USING btree (source_posted_at, event_stage, event_kind, created_at DESC);


--
-- Name: idx_binary_recovery_current_source_posted; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_recovery_current_source_posted ON ONLY public.binary_recovery_current USING btree (source_posted_at, provider_id, newsgroup_id, binary_id);


--
-- Name: binary_recovery_current_defau_source_posted_at_provider_id__idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_recovery_current_defau_source_posted_at_provider_id__idx ON public.binary_recovery_current_default USING btree (source_posted_at, provider_id, newsgroup_id, binary_id);


--
-- Name: idx_binary_recovery_backlog; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_recovery_backlog ON ONLY public.binary_recovery_current USING btree (source_posted_at, provider_id, newsgroup_id, recovered_source, recovered_confidence);


--
-- Name: binary_recovery_current_defau_source_posted_at_provider_id_idx1; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_recovery_current_defau_source_posted_at_provider_id_idx1 ON public.binary_recovery_current_default USING btree (source_posted_at, provider_id, newsgroup_id, recovered_source, recovered_confidence);


--
-- Name: idx_binary_superseded_sources_source_posted; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_superseded_sources_source_posted ON ONLY public.binary_superseded_sources USING btree (source_posted_at, provider_id, newsgroup_id, source_binary_id);


--
-- Name: binary_superseded_sources_def_source_posted_at_provider_id__idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_superseded_sources_def_source_posted_at_provider_id__idx ON public.binary_superseded_sources_default USING btree (source_posted_at, provider_id, newsgroup_id, source_binary_id);


--
-- Name: idx_binary_superseded_sources_release_family; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_superseded_sources_release_family ON ONLY public.binary_superseded_sources USING btree (source_posted_at, provider_id, newsgroup_id, release_family_key);


--
-- Name: binary_superseded_sources_def_source_posted_at_provider_id_idx1; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_superseded_sources_def_source_posted_at_provider_id_idx1 ON public.binary_superseded_sources_default USING btree (source_posted_at, provider_id, newsgroup_id, release_family_key);


--
-- Name: idx_binary_superseded_sources_target; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_superseded_sources_target ON ONLY public.binary_superseded_sources USING btree (source_posted_at, target_binary_id);


--
-- Name: binary_superseded_sources_def_source_posted_at_target_binar_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_superseded_sources_def_source_posted_at_target_binar_idx ON public.binary_superseded_sources_default USING btree (source_posted_at, target_binary_id);


--
-- Name: idx_binary_text_evidence_source_posted; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_text_evidence_source_posted ON ONLY public.binary_text_evidence USING btree (source_posted_at, stage_name, binary_id);


--
-- Name: binary_text_evidence_default_source_posted_at_stage_name_bi_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX binary_text_evidence_default_source_posted_at_stage_name_bi_idx ON public.binary_text_evidence_default USING btree (source_posted_at, stage_name, binary_id);


--
-- Name: idx_article_header_crosspost_group_summary_rank; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_article_header_crosspost_group_summary_rank ON public.article_header_crosspost_group_summary USING btree (observed_article_count DESC, distinct_message_count DESC, last_seen_at DESC, observed_group_name);


--
-- Name: idx_binary_core_source_posted; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_core_source_posted ON public.binary_core USING btree (source_posted_at, binary_id) WHERE (source_posted_at IS NOT NULL);


--
-- Name: idx_crosspost_popularity_refresh_queue_ready; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_crosspost_popularity_refresh_queue_ready ON public.crosspost_popularity_refresh_queue USING btree (ready_at, observed_group_name) WHERE (status = ANY (ARRAY['pending'::text, 'failed'::text]));


--
-- Name: idx_deferred_article_ranges_ready; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_deferred_article_ranges_ready ON public.deferred_article_ranges USING btree (state, priority_score DESC, posted_at_max DESC NULLS LAST, updated_at) WHERE (state = 'ready'::text);


--
-- Name: idx_indexer_daily_bucket_stats_day_tier; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_indexer_daily_bucket_stats_day_tier ON public.indexer_daily_bucket_stats USING btree (bucket_day DESC, tier, provider_id, newsgroup_id);


--
-- Name: idx_indexer_group_profiles_tier_score; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_indexer_group_profiles_tier_score ON public.indexer_group_profiles USING btree (COALESCE(tier_override, tier), score DESC, updated_at DESC);


--
-- Name: idx_indexer_group_profiles_yield; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_indexer_group_profiles_yield ON public.indexer_group_profiles USING btree (releases_created_1d DESC, recovery_queued_1d DESC, score DESC);


--
-- Name: idx_indexer_nntp_runtime_snapshots_module_updated_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_indexer_nntp_runtime_snapshots_module_updated_at ON public.indexer_nntp_runtime_snapshots USING btree (module_name, updated_at DESC);


--
-- Name: idx_indexer_provider_group_inventory_group_name; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_indexer_provider_group_inventory_group_name ON public.indexer_provider_group_inventory USING btree (lower(group_name));


--
-- Name: idx_indexer_provider_group_inventory_scanned_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_indexer_provider_group_inventory_scanned_at ON public.indexer_provider_group_inventory USING btree (scanned_at DESC);


--
-- Name: idx_indexer_scrape_day_boundaries_day; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_indexer_scrape_day_boundaries_day ON public.indexer_scrape_day_boundaries USING btree (bucket_day DESC, provider_id, newsgroup_id);


--
-- Name: idx_indexer_stage_runs_stage_started_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_indexer_stage_runs_stage_started_at ON public.indexer_stage_runs USING btree (stage_name, started_at DESC);


--
-- Name: idx_indexer_stage_runs_stage_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_indexer_stage_runs_stage_status ON public.indexer_stage_runs USING btree (stage_name, status);


--
-- Name: idx_indexer_stage_state_lease_expires_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_indexer_stage_state_lease_expires_at ON public.indexer_stage_state USING btree (lease_expires_at);


--
-- Name: idx_nzb_cache_generation_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_nzb_cache_generation_status ON public.nzb_cache USING btree (generation_status);


--
-- Name: idx_poster_materialization_queue_ready; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_poster_materialization_queue_ready ON ONLY public.poster_materialization_queue USING btree (status, ready_at, lease_expires_at, article_header_id);


--
-- Name: idx_poster_materialization_queue_ready_partition; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_poster_materialization_queue_ready_partition ON ONLY public.poster_materialization_queue USING btree (status, ready_at, source_posted_at, article_header_id) WHERE ((status = ANY (ARRAY['pending'::text, 'failed'::text])) AND (btrim(COALESCE(poster_key, ''::text)) <> ''::text));


--
-- Name: idx_predb_backfill_checkpoints_updated_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_predb_backfill_checkpoints_updated_at ON public.predb_backfill_checkpoints USING btree (updated_at DESC);


--
-- Name: idx_predb_entries_source_external_id; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_predb_entries_source_external_id ON public.predb_entries USING btree (source, external_id) WHERE (external_id > 0);


--
-- Name: idx_release_archive_detail_files_release_order; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_release_archive_detail_files_release_order ON public.release_archive_detail_files USING btree (release_id, file_index, file_name);


--
-- Name: idx_release_archive_detail_subtitle_release; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_release_archive_detail_subtitle_release ON public.release_archive_detail_subtitle_languages USING btree (release_id, ordinal);


--
-- Name: idx_release_archive_lineage_article_headers_article_header_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_release_archive_lineage_article_headers_article_header_id ON public.release_archive_lineage_article_headers USING btree (article_header_id);


--
-- Name: idx_release_archive_lineage_binaries_binary_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_release_archive_lineage_binaries_binary_id ON public.release_archive_lineage_binaries USING btree (binary_id);


--
-- Name: idx_release_archive_state_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_release_archive_state_status ON public.release_archive_state USING btree (archive_status, purge_eligible_at, archived_at);


--
-- Name: idx_release_catalog_files_release_order; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_release_catalog_files_release_order ON public.release_catalog_files USING btree (release_id, file_index, id);


--
-- Name: idx_release_family_readiness_acks_processed_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_release_family_readiness_acks_processed_at ON public.release_family_readiness_acks USING btree (processed_at);


--
-- Name: idx_release_family_readiness_bucket_lookup; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_release_family_readiness_bucket_lookup ON ONLY public.release_family_readiness_summaries USING btree (source_posted_at, readiness_bucket, provider_id, newsgroup_id, key_kind, family_key);


--
-- Name: idx_release_family_readiness_key_bucket_lookup; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_release_family_readiness_key_bucket_lookup ON ONLY public.release_family_readiness_summaries USING btree (provider_id, newsgroup_id, key_kind, family_key, readiness_bucket, source_posted_at);


--
-- Name: idx_release_family_readiness_source_posted; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_release_family_readiness_source_posted ON ONLY public.release_family_readiness_summaries USING btree (source_posted_at, provider_id, newsgroup_id, key_kind, family_key);


--
-- Name: idx_release_family_readiness_summaries_pending; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_release_family_readiness_summaries_pending ON ONLY public.release_family_readiness_summaries USING btree (source_posted_at, updated_at, provider_id, newsgroup_id) WHERE (updated_at > COALESCE(processed_at, updated_at));


--
-- Name: idx_release_family_readiness_summaries_release_queue; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_release_family_readiness_summaries_release_queue ON ONLY public.release_family_readiness_summaries USING btree (source_posted_at, updated_at, provider_id, newsgroup_id, key_kind, family_key) WHERE (recover_pending = false);


--
-- Name: idx_release_family_summary_refresh_queue_base_stem; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_release_family_summary_refresh_queue_base_stem ON public.release_family_summary_refresh_queue USING btree (queued_at, provider_id, newsgroup_id, family_key) WHERE (key_kind = 'base_stem'::text);


--
-- Name: idx_release_family_summary_refresh_queue_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_release_family_summary_refresh_queue_key ON public.release_family_summary_refresh_queue USING btree (provider_id, newsgroup_id, key_kind, family_key);


--
-- Name: idx_release_family_summary_refresh_queue_queued_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_release_family_summary_refresh_queue_queued_at ON public.release_family_summary_refresh_queue USING btree (queued_at, provider_id, newsgroup_id, key_kind, family_key);


--
-- Name: idx_release_family_yenc_recovery_candidates; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_release_family_yenc_recovery_candidates ON ONLY public.release_family_readiness_summaries USING btree (source_posted_at, provider_id, newsgroup_id, family_key) WHERE ((key_kind = 'release_family'::text) AND (readiness_bucket = ANY (ARRAY['overgrouped_contextual'::text, 'weak_single_binary'::text, 'weak_obfuscated_set'::text])));


--
-- Name: idx_release_files_binary_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_release_files_binary_unique ON public.release_files USING btree (binary_id) WHERE (binary_id IS NOT NULL);


--
-- Name: idx_release_files_release_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_release_files_release_id ON public.release_files USING btree (release_id);


--
-- Name: idx_release_password_candidates_release_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_release_password_candidates_release_status ON public.release_password_candidates USING btree (release_id, verification_status, updated_at DESC);


--
-- Name: idx_release_ready_candidate_acks_processed_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_release_ready_candidate_acks_processed_at ON public.release_ready_candidate_acks USING btree (processed_at);


--
-- Name: idx_release_ready_candidates_lookup; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_release_ready_candidates_lookup ON ONLY public.release_ready_candidates USING btree (provider_id, key_kind, family_key, source_posted_at, newsgroup_id);


--
-- Name: idx_release_ready_candidates_queue; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_release_ready_candidates_queue ON ONLY public.release_ready_candidates USING btree (source_posted_at, updated_at, provider_id, newsgroup_id, key_kind, family_key);


--
-- Name: idx_release_ready_candidates_source_posted; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_release_ready_candidates_source_posted ON ONLY public.release_ready_candidates USING btree (source_posted_at, provider_id, newsgroup_id, key_kind, family_key);


--
-- Name: idx_release_recovered_file_set_candidates_lookup; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_release_recovered_file_set_candidates_lookup ON ONLY public.release_recovered_file_set_candidates USING btree (provider_id, file_set_key, source_posted_at);


--
-- Name: idx_release_recovered_file_set_candidates_queue; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_release_recovered_file_set_candidates_queue ON ONLY public.release_recovered_file_set_candidates USING btree (source_posted_at, updated_at, provider_id, representative_newsgroup_id, file_set_key);


--
-- Name: idx_release_recovered_file_set_candidates_source_posted; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_release_recovered_file_set_candidates_source_posted ON ONLY public.release_recovered_file_set_candidates USING btree (source_posted_at, provider_id, file_set_key);


--
-- Name: idx_release_stage_dirty_families_source_posted; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_release_stage_dirty_families_source_posted ON ONLY public.release_stage_dirty_families USING btree (source_posted_at, provider_id, newsgroup_id, key_kind, family_key);


--
-- Name: idx_release_stage_dirty_families_updated_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_release_stage_dirty_families_updated_at ON ONLY public.release_stage_dirty_families USING btree (source_posted_at, updated_at, provider_id, newsgroup_id);


--
-- Name: idx_release_tmdb_matches_release_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_release_tmdb_matches_release_id ON public.release_tmdb_matches USING btree (release_id);


--
-- Name: idx_release_tvdb_matches_release_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_release_tvdb_matches_release_id ON public.release_tvdb_matches USING btree (release_id);


--
-- Name: idx_releases_category_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_releases_category_id ON public.releases USING btree (category_id);


--
-- Name: idx_releases_posted_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_releases_posted_at ON public.releases USING btree (posted_at DESC);


--
-- Name: idx_releases_provider_group_name; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_releases_provider_group_name ON public.releases USING btree (provider_id, group_name);


--
-- Name: idx_releases_provider_release_family_key; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_releases_provider_release_family_key ON public.releases USING btree (provider_id, release_family_key);


--
-- Name: idx_releases_provider_release_key; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_releases_provider_release_key ON public.releases USING btree (provider_id, release_key);


--
-- Name: idx_releases_search_title; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_releases_search_title ON public.releases USING btree (search_title);


--
-- Name: idx_scrape_checkpoints_provider_newsgroup; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_scrape_checkpoints_provider_newsgroup ON public.scrape_checkpoints USING btree (provider_id, newsgroup_id);


--
-- Name: idx_scrape_runs_provider_id_started_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_scrape_runs_provider_id_started_at ON public.scrape_runs USING btree (provider_id, started_at DESC);


--
-- Name: idx_yenc_recovery_ready_date_priority_claim; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_yenc_recovery_ready_date_priority_claim ON ONLY public.yenc_recovery_work_items USING btree (date_utc DESC NULLS LAST, priority_rank, updated_at DESC, binary_id) WHERE ((status = 'ready'::text) AND (btrim(COALESCE(message_id, ''::text)) <> ''::text));


--
-- Name: idx_yenc_recovery_ready_nonpriority_updated; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_yenc_recovery_ready_nonpriority_updated ON ONLY public.yenc_recovery_work_items USING btree (updated_at DESC, binary_id, source_posted_at) WHERE ((status = 'ready'::text) AND (priority_rank > 0));


--
-- Name: idx_yenc_recovery_work_items_admission_pressure; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_yenc_recovery_work_items_admission_pressure ON ONLY public.yenc_recovery_work_items USING btree (status, group_tier, priority_rank, source_posted_at DESC, updated_at DESC);


--
-- Name: idx_yenc_recovery_work_items_article_header_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_yenc_recovery_work_items_article_header_id ON ONLY public.yenc_recovery_work_items USING btree (article_header_id);


--
-- Name: idx_yenc_recovery_work_items_blank_message_retire; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_yenc_recovery_work_items_blank_message_retire ON ONLY public.yenc_recovery_work_items USING btree (updated_at, binary_id) WHERE ((status = ANY (ARRAY['ready'::text, 'running'::text])) AND (btrim(message_id) = ''::text));


--
-- Name: idx_yenc_recovery_work_items_expired_running; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_yenc_recovery_work_items_expired_running ON ONLY public.yenc_recovery_work_items USING btree (lease_expires_at, priority_rank, updated_at DESC, binary_id) WHERE (status = 'running'::text);


--
-- Name: idx_yenc_recovery_work_items_partition_day; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_yenc_recovery_work_items_partition_day ON ONLY public.yenc_recovery_work_items USING btree (partition_day, status, provider_id, newsgroup_id);


--
-- Name: idx_yenc_recovery_work_items_ready; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_yenc_recovery_work_items_ready ON ONLY public.yenc_recovery_work_items USING btree (status, ready_at, priority_rank, updated_at DESC, binary_id) WHERE (status = 'ready'::text);


--
-- Name: idx_yenc_recovery_work_items_ready_date_range; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_yenc_recovery_work_items_ready_date_range ON ONLY public.yenc_recovery_work_items USING btree (date_utc DESC NULLS LAST, priority_rank, newsgroup_id, article_number, binary_id) WHERE (status = 'ready'::text);


--
-- Name: idx_yenc_recovery_work_items_ready_order; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_yenc_recovery_work_items_ready_order ON ONLY public.yenc_recovery_work_items USING btree (priority_rank, updated_at DESC, binary_id) WHERE (status = 'ready'::text);


--
-- Name: idx_yenc_recovery_work_items_ready_posted_nulls_last; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_yenc_recovery_work_items_ready_posted_nulls_last ON ONLY public.yenc_recovery_work_items USING btree (priority_rank, date_utc DESC NULLS LAST, updated_at DESC, binary_id) WHERE (status = 'ready'::text);


--
-- Name: poster_materialization_queue__status_ready_at_lease_expires_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX poster_materialization_queue__status_ready_at_lease_expires_idx ON public.poster_materialization_queue_default USING btree (status, ready_at, lease_expires_at, article_header_id);


--
-- Name: poster_materialization_queue_status_ready_at_source_poste_idx31; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX poster_materialization_queue_status_ready_at_source_poste_idx31 ON public.poster_materialization_queue_default USING btree (status, ready_at, source_posted_at, article_header_id) WHERE ((status = ANY (ARRAY['pending'::text, 'failed'::text])) AND (btrim(COALESCE(poster_key, ''::text)) <> ''::text));


--
-- Name: release_family_readiness_sum_provider_id_newsgroup_id_key_idx31; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX release_family_readiness_sum_provider_id_newsgroup_id_key_idx31 ON public.release_family_readiness_summaries_default USING btree (provider_id, newsgroup_id, key_kind, family_key, readiness_bucket, source_posted_at);


--
-- Name: release_family_readiness_summ_source_posted_at_provider_id__idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX release_family_readiness_summ_source_posted_at_provider_id__idx ON public.release_family_readiness_summaries_default USING btree (source_posted_at, provider_id, newsgroup_id, key_kind, family_key);


--
-- Name: release_family_readiness_summ_source_posted_at_provider_id_idx1; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX release_family_readiness_summ_source_posted_at_provider_id_idx1 ON public.release_family_readiness_summaries_default USING btree (source_posted_at, provider_id, newsgroup_id, family_key) WHERE ((key_kind = 'release_family'::text) AND (readiness_bucket = ANY (ARRAY['overgrouped_contextual'::text, 'weak_single_binary'::text, 'weak_obfuscated_set'::text])));


--
-- Name: release_family_readiness_summ_source_posted_at_readiness_bu_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX release_family_readiness_summ_source_posted_at_readiness_bu_idx ON public.release_family_readiness_summaries_default USING btree (source_posted_at, readiness_bucket, provider_id, newsgroup_id, key_kind, family_key);


--
-- Name: release_family_readiness_summ_source_posted_at_updated_at__idx1; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX release_family_readiness_summ_source_posted_at_updated_at__idx1 ON public.release_family_readiness_summaries_default USING btree (source_posted_at, updated_at, provider_id, newsgroup_id, key_kind, family_key) WHERE (recover_pending = false);


--
-- Name: release_family_readiness_summ_source_posted_at_updated_at_p_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX release_family_readiness_summ_source_posted_at_updated_at_p_idx ON public.release_family_readiness_summaries_default USING btree (source_posted_at, updated_at, provider_id, newsgroup_id) WHERE (updated_at > COALESCE(processed_at, updated_at));


--
-- Name: release_ready_candidates_defa_provider_id_key_kind_family_k_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX release_ready_candidates_defa_provider_id_key_kind_family_k_idx ON public.release_ready_candidates_default USING btree (provider_id, key_kind, family_key, source_posted_at, newsgroup_id);


--
-- Name: release_ready_candidates_defa_source_posted_at_provider_id__idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX release_ready_candidates_defa_source_posted_at_provider_id__idx ON public.release_ready_candidates_default USING btree (source_posted_at, provider_id, newsgroup_id, key_kind, family_key);


--
-- Name: release_ready_candidates_defa_source_posted_at_updated_at_p_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX release_ready_candidates_defa_source_posted_at_updated_at_p_idx ON public.release_ready_candidates_default USING btree (source_posted_at, updated_at, provider_id, newsgroup_id, key_kind, family_key);


--
-- Name: release_recovered_file_set_c_provider_id_file_set_key_sou_idx31; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX release_recovered_file_set_c_provider_id_file_set_key_sou_idx31 ON public.release_recovered_file_set_candidates_default USING btree (provider_id, file_set_key, source_posted_at);


--
-- Name: release_recovered_file_set_ca_source_posted_at_provider_id__idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX release_recovered_file_set_ca_source_posted_at_provider_id__idx ON public.release_recovered_file_set_candidates_default USING btree (source_posted_at, provider_id, file_set_key);


--
-- Name: release_recovered_file_set_ca_source_posted_at_updated_at_p_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX release_recovered_file_set_ca_source_posted_at_updated_at_p_idx ON public.release_recovered_file_set_candidates_default USING btree (source_posted_at, updated_at, provider_id, representative_newsgroup_id, file_set_key);


--
-- Name: release_stage_dirty_families__source_posted_at_provider_id__idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX release_stage_dirty_families__source_posted_at_provider_id__idx ON public.release_stage_dirty_families_default USING btree (source_posted_at, provider_id, newsgroup_id, key_kind, family_key);


--
-- Name: release_stage_dirty_families__source_posted_at_updated_at_p_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX release_stage_dirty_families__source_posted_at_updated_at_p_idx ON public.release_stage_dirty_families_default USING btree (source_posted_at, updated_at, provider_id, newsgroup_id);


--
-- Name: yenc_recovery_work_items_defa_date_utc_priority_rank_newsgr_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX yenc_recovery_work_items_defa_date_utc_priority_rank_newsgr_idx ON public.yenc_recovery_work_items_default USING btree (date_utc DESC NULLS LAST, priority_rank, newsgroup_id, article_number, binary_id) WHERE (status = 'ready'::text);


--
-- Name: yenc_recovery_work_items_defa_date_utc_priority_rank_update_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX yenc_recovery_work_items_defa_date_utc_priority_rank_update_idx ON public.yenc_recovery_work_items_default USING btree (date_utc DESC NULLS LAST, priority_rank, updated_at DESC, binary_id) WHERE ((status = 'ready'::text) AND (btrim(COALESCE(message_id, ''::text)) <> ''::text));


--
-- Name: yenc_recovery_work_items_defa_lease_expires_at_priority_ran_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX yenc_recovery_work_items_defa_lease_expires_at_priority_ran_idx ON public.yenc_recovery_work_items_default USING btree (lease_expires_at, priority_rank, updated_at DESC, binary_id) WHERE (status = 'running'::text);


--
-- Name: yenc_recovery_work_items_defa_partition_day_status_provider_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX yenc_recovery_work_items_defa_partition_day_status_provider_idx ON public.yenc_recovery_work_items_default USING btree (partition_day, status, provider_id, newsgroup_id);


--
-- Name: yenc_recovery_work_items_defa_priority_rank_date_utc_update_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX yenc_recovery_work_items_defa_priority_rank_date_utc_update_idx ON public.yenc_recovery_work_items_default USING btree (priority_rank, date_utc DESC NULLS LAST, updated_at DESC, binary_id) WHERE (status = 'ready'::text);


--
-- Name: yenc_recovery_work_items_defa_priority_rank_updated_at_bina_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX yenc_recovery_work_items_defa_priority_rank_updated_at_bina_idx ON public.yenc_recovery_work_items_default USING btree (priority_rank, updated_at DESC, binary_id) WHERE (status = 'ready'::text);


--
-- Name: yenc_recovery_work_items_defa_status_group_tier_priority_ra_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX yenc_recovery_work_items_defa_status_group_tier_priority_ra_idx ON public.yenc_recovery_work_items_default USING btree (status, group_tier, priority_rank, source_posted_at DESC, updated_at DESC);


--
-- Name: yenc_recovery_work_items_defa_status_ready_at_priority_rank_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX yenc_recovery_work_items_defa_status_ready_at_priority_rank_idx ON public.yenc_recovery_work_items_default USING btree (status, ready_at, priority_rank, updated_at DESC, binary_id) WHERE (status = 'ready'::text);


--
-- Name: yenc_recovery_work_items_defa_updated_at_binary_id_source_p_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX yenc_recovery_work_items_defa_updated_at_binary_id_source_p_idx ON public.yenc_recovery_work_items_default USING btree (updated_at DESC, binary_id, source_posted_at) WHERE ((status = 'ready'::text) AND (priority_rank > 0));


--
-- Name: yenc_recovery_work_items_default_article_header_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX yenc_recovery_work_items_default_article_header_id_idx ON public.yenc_recovery_work_items_default USING btree (article_header_id);


--
-- Name: yenc_recovery_work_items_default_updated_at_binary_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX yenc_recovery_work_items_default_updated_at_binary_id_idx ON public.yenc_recovery_work_items_default USING btree (updated_at, binary_id) WHERE ((status = ANY (ARRAY['ready'::text, 'running'::text])) AND (btrim(message_id) = ''::text));


--
-- Name: article_cohort_assembly_queue_claim_until_priority_rank_sou_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_article_cohort_assembly_queue_claim_until ATTACH PARTITION public.article_cohort_assembly_queue_claim_until_priority_rank_sou_idx;


--
-- Name: article_cohort_assembly_queue_default_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.article_cohort_assembly_queue_pkey ATTACH PARTITION public.article_cohort_assembly_queue_default_pkey;


--
-- Name: article_cohort_assembly_queue_status_priority_rank_score_so_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_article_cohort_assembly_queue_claim ATTACH PARTITION public.article_cohort_assembly_queue_status_priority_rank_score_so_idx;


--
-- Name: article_cohort_candidates_def_provider_id_newsgroup_id_coho_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_article_cohort_candidates_lookup ATTACH PARTITION public.article_cohort_candidates_def_provider_id_newsgroup_id_coho_idx;


--
-- Name: article_cohort_candidates_def_status_priority_rank_score_so_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_article_cohort_candidates_ready ATTACH PARTITION public.article_cohort_candidates_def_status_priority_rank_score_so_idx;


--
-- Name: article_cohort_candidates_default_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.article_cohort_candidates_pkey ATTACH PARTITION public.article_cohort_candidates_default_pkey;


--
-- Name: article_cohort_yenc_queue_def_cohort_key_status_source_post_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_article_cohort_yenc_queue_cohort ATTACH PARTITION public.article_cohort_yenc_queue_def_cohort_key_status_source_post_idx;


--
-- Name: article_cohort_yenc_queue_def_source_posted_at_article_head_key; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.article_cohort_yenc_queue_article_key ATTACH PARTITION public.article_cohort_yenc_queue_def_source_posted_at_article_head_key;


--
-- Name: article_cohort_yenc_queue_def_status_priority_rank_score_so_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_article_cohort_yenc_queue_ready ATTACH PARTITION public.article_cohort_yenc_queue_def_status_priority_rank_score_so_idx;


--
-- Name: article_cohort_yenc_queue_default_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.article_cohort_yenc_queue_pkey ATTACH PARTITION public.article_cohort_yenc_queue_default_pkey;


--
-- Name: article_header_assembly_queu_article_header_id_source_po_idx127; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_article_assembly_queue_general_lane_claim ATTACH PARTITION public.article_header_assembly_queu_article_header_id_source_po_idx127;


--
-- Name: article_header_assembly_queu_article_header_id_source_pos_idx31; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_article_assembly_queue_recent_claimable ATTACH PARTITION public.article_header_assembly_queu_article_header_id_source_pos_idx31;


--
-- Name: article_header_assembly_queu_article_header_id_source_pos_idx63; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_article_assembly_queue_general_claimable ATTACH PARTITION public.article_header_assembly_queu_article_header_id_source_pos_idx63;


--
-- Name: article_header_assembly_queu_article_header_id_source_pos_idx95; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_article_assembly_queue_structured_lane_claim ATTACH PARTITION public.article_header_assembly_queu_article_header_id_source_pos_idx95;


--
-- Name: article_header_assembly_queu_provider_id_newsgroup_id_nor_idx31; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_article_assembly_queue_structured_lookup ATTACH PARTITION public.article_header_assembly_queu_provider_id_newsgroup_id_nor_idx31;


--
-- Name: article_header_assembly_queue_claim_until_queued_at_article_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_article_assembly_queue_claim ATTACH PARTITION public.article_header_assembly_queue_claim_until_queued_at_article_idx;


--
-- Name: article_header_assembly_queue_default_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.article_header_assembly_queue_pkey ATTACH PARTITION public.article_header_assembly_queue_default_pkey;


--
-- Name: article_header_assembly_queue_source_posted_at_claim_until__idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_article_header_assembly_queue_source_posted ATTACH PARTITION public.article_header_assembly_queue_source_posted_at_claim_until__idx;


--
-- Name: article_header_crosspost_gro_observed_group_name_article__idx31; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_article_header_crosspost_groups_refresh_lookup ATTACH PARTITION public.article_header_crosspost_gro_observed_group_name_article__idx31;


--
-- Name: article_header_crosspost_groups_default_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.article_header_crosspost_groups_pkey ATTACH PARTITION public.article_header_crosspost_groups_default_pkey;


--
-- Name: article_header_ingest_payloads_default_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.article_header_ingest_payloads_pkey ATTACH PARTITION public.article_header_ingest_payloads_default_pkey;


--
-- Name: article_header_poster_refs_default_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.article_header_poster_refs_pkey ATTACH PARTITION public.article_header_poster_refs_default_pkey;


--
-- Name: article_headers_default_id_source_posted_at_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_article_headers_id_source_posted ATTACH PARTITION public.article_headers_default_id_source_posted_at_idx;


--
-- Name: article_headers_default_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.article_headers_pkey ATTACH PARTITION public.article_headers_default_pkey;


--
-- Name: article_headers_default_provider_id_newsgroup_id_article_nu_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_article_headers_provider_group_article_desc ATTACH PARTITION public.article_headers_default_provider_id_newsgroup_id_article_nu_idx;


--
-- Name: article_headers_default_provider_id_newsgroup_id_date_utc_a_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_article_headers_provider_group_date_article ATTACH PARTITION public.article_headers_default_provider_id_newsgroup_id_date_utc_a_idx;


--
-- Name: article_headers_default_source_posted_at_newsgroup_id_artic_key; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.article_headers_newsgroup_id_article_number_key ATTACH PARTITION public.article_headers_default_source_posted_at_newsgroup_id_artic_key;


--
-- Name: article_headers_default_source_posted_at_newsgroup_id_messa_key; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.article_headers_newsgroup_id_message_id_key ATTACH PARTITION public.article_headers_default_source_posted_at_newsgroup_id_messa_key;


--
-- Name: article_headers_default_source_posted_at_provider_id_newsgr_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_article_headers_source_posted_group_article ATTACH PARTITION public.article_headers_default_source_posted_at_provider_id_newsgr_idx;


--
-- Name: binary_archive_entries_defaul_source_posted_at_binary_id_en_key; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.binary_archive_entries_binary_id_entry_name_key ATTACH PARTITION public.binary_archive_entries_defaul_source_posted_at_binary_id_en_key;


--
-- Name: binary_archive_entries_default_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.binary_archive_entries_pkey ATTACH PARTITION public.binary_archive_entries_default_pkey;


--
-- Name: binary_archive_entries_default_source_posted_at_binary_id_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_archive_entries_source_posted ATTACH PARTITION public.binary_archive_entries_default_source_posted_at_binary_id_idx;


--
-- Name: binary_completion_keys_defaul_provider_id_newsgroup_id_norm_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_completion_keys_filename_lookup ATTACH PARTITION public.binary_completion_keys_defaul_provider_id_newsgroup_id_norm_idx;


--
-- Name: binary_completion_keys_defaul_source_posted_at_is_main_payl_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_completion_keys_rank ATTACH PARTITION public.binary_completion_keys_defaul_source_posted_at_is_main_payl_idx;


--
-- Name: binary_completion_keys_defaul_source_posted_at_provider_id__idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_completion_keys_match ATTACH PARTITION public.binary_completion_keys_defaul_source_posted_at_provider_id__idx;


--
-- Name: binary_completion_keys_defaul_source_posted_at_provider_id_idx1; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_completion_keys_match_rank ATTACH PARTITION public.binary_completion_keys_defaul_source_posted_at_provider_id_idx1;


--
-- Name: binary_completion_keys_default_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.binary_completion_keys_pkey ATTACH PARTITION public.binary_completion_keys_default_pkey;


--
-- Name: binary_grouping_evidence_default_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.binary_grouping_evidence_pkey ATTACH PARTITION public.binary_grouping_evidence_default_pkey;


--
-- Name: binary_grouping_evidence_default_source_posted_at_binary_id_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_grouping_evidence_source_posted ATTACH PARTITION public.binary_grouping_evidence_default_source_posted_at_binary_id_idx;


--
-- Name: binary_identity_current_defau_lower_updated_at_binary_id_so_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_identity_strength_updated ATTACH PARTITION public.binary_identity_current_defau_lower_updated_at_binary_id_so_idx;


--
-- Name: binary_identity_current_defau_provider_id_file_set_key_sour_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_identity_file_set_lookup ATTACH PARTITION public.binary_identity_current_defau_provider_id_file_set_key_sour_idx;


--
-- Name: binary_identity_current_defau_provider_id_newsgroup_id_iden_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_identity_opaque_subject_cohort ATTACH PARTITION public.binary_identity_current_defau_provider_id_newsgroup_id_iden_idx;


--
-- Name: binary_identity_current_defau_provider_id_newsgroup_id_low_idx1; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_identity_subject_regroup_lookup ATTACH PARTITION public.binary_identity_current_defau_provider_id_newsgroup_id_low_idx1;


--
-- Name: binary_identity_current_defau_provider_id_newsgroup_id_lowe_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_identity_base_stem_lookup ATTACH PARTITION public.binary_identity_current_defau_provider_id_newsgroup_id_lowe_idx;


--
-- Name: binary_identity_current_defau_provider_id_newsgroup_id_rele_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_identity_release_family_lookup ATTACH PARTITION public.binary_identity_current_defau_provider_id_newsgroup_id_rele_idx;


--
-- Name: binary_identity_current_defau_source_posted_at_binary_id_pr_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_identity_subject_regroup_candidates ATTACH PARTITION public.binary_identity_current_defau_source_posted_at_binary_id_pr_idx;


--
-- Name: binary_identity_current_defau_source_posted_at_provider_id__idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_identity_current_source_posted ATTACH PARTITION public.binary_identity_current_defau_source_posted_at_provider_id__idx;


--
-- Name: binary_identity_current_defau_source_posted_at_provider_id_idx1; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_identity_release_family ATTACH PARTITION public.binary_identity_current_defau_source_posted_at_provider_id_idx1;


--
-- Name: binary_identity_current_defau_source_posted_at_provider_id_idx2; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_identity_file_set ATTACH PARTITION public.binary_identity_current_defau_source_posted_at_provider_id_idx2;


--
-- Name: binary_identity_current_defau_source_posted_at_updated_at__idx1; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_identity_inspect_par2_backlog ATTACH PARTITION public.binary_identity_current_defau_source_posted_at_updated_at__idx1;


--
-- Name: binary_identity_current_defau_source_posted_at_updated_at_b_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_identity_inspect_discovery_backlog ATTACH PARTITION public.binary_identity_current_defau_source_posted_at_updated_at_b_idx;


--
-- Name: binary_identity_current_default_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.binary_identity_current_pkey ATTACH PARTITION public.binary_identity_current_default_pkey;


--
-- Name: binary_identity_current_default_source_posted_at_binary_id_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_identity_subject_multipart_stale ATTACH PARTITION public.binary_identity_current_default_source_posted_at_binary_id_idx;


--
-- Name: binary_inspection_artifacts_d_source_posted_at_binary_id_st_key; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.binary_inspection_artifacts_binary_id_stage_name_artifact_r_key ATTACH PARTITION public.binary_inspection_artifacts_d_source_posted_at_binary_id_st_key;


--
-- Name: binary_inspection_artifacts_d_source_posted_at_stage_name_b_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_inspection_artifacts_source_posted ATTACH PARTITION public.binary_inspection_artifacts_d_source_posted_at_stage_name_b_idx;


--
-- Name: binary_inspection_artifacts_default_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.binary_inspection_artifacts_pkey ATTACH PARTITION public.binary_inspection_artifacts_default_pkey;


--
-- Name: binary_inspection_ready_queue_default_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.binary_inspection_ready_queue_pkey ATTACH PARTITION public.binary_inspection_ready_queue_default_pkey;


--
-- Name: binary_inspection_ready_queue_source_posted_at_stage_name_s_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_inspection_ready_queue_source_posted ATTACH PARTITION public.binary_inspection_ready_queue_source_posted_at_stage_name_s_idx;


--
-- Name: binary_inspections_default_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.binary_inspections_pkey ATTACH PARTITION public.binary_inspections_default_pkey;


--
-- Name: binary_inspections_default_release_id_status_updated_at_sou_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_inspections_release_status_lookup ATTACH PARTITION public.binary_inspections_default_release_id_status_updated_at_sou_idx;


--
-- Name: binary_inspections_default_source_posted_at_stage_name_bina_key; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.binary_inspections_stage_name_binary_id_key ATTACH PARTITION public.binary_inspections_default_source_posted_at_stage_name_bina_key;


--
-- Name: binary_inspections_default_source_posted_at_stage_name_sta_idx1; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_inspections_status ATTACH PARTITION public.binary_inspections_default_source_posted_at_stage_name_sta_idx1;


--
-- Name: binary_inspections_default_source_posted_at_stage_name_stat_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_inspections_source_posted ATTACH PARTITION public.binary_inspections_default_source_posted_at_stage_name_stat_idx;


--
-- Name: binary_lifecycle_default_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.binary_lifecycle_pkey ATTACH PARTITION public.binary_lifecycle_default_pkey;


--
-- Name: binary_lifecycle_default_source_posted_at_binary_id_lifecyc_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_lifecycle_status_lookup ATTACH PARTITION public.binary_lifecycle_default_source_posted_at_binary_id_lifecyc_idx;


--
-- Name: binary_lifecycle_default_source_posted_at_provider_id_newsg_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_lifecycle_source_posted ATTACH PARTITION public.binary_lifecycle_default_source_posted_at_provider_id_newsg_idx;


--
-- Name: binary_lifecycle_default_source_posted_at_release_id_lifecy_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_lifecycle_release ATTACH PARTITION public.binary_lifecycle_default_source_posted_at_release_id_lifecy_idx;


--
-- Name: binary_media_streams_default_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.binary_media_streams_pkey ATTACH PARTITION public.binary_media_streams_default_pkey;


--
-- Name: binary_media_streams_default_source_posted_at_binary_id_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_media_streams_source_posted ATTACH PARTITION public.binary_media_streams_default_source_posted_at_binary_id_idx;


--
-- Name: binary_media_streams_default_source_posted_at_binary_id_str_key; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.binary_media_streams_binary_id_stream_index_stream_type_key ATTACH PARTITION public.binary_media_streams_default_source_posted_at_binary_id_str_key;


--
-- Name: binary_observation_stats_defa_posted_at_source_posted_at_pr_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_observation_stats_opaque_posted_admission ATTACH PARTITION public.binary_observation_stats_defa_posted_at_source_posted_at_pr_idx;


--
-- Name: binary_observation_stats_defa_provider_id_newsgroup_id_post_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_observation_stats_posted_cohort ATTACH PARTITION public.binary_observation_stats_defa_provider_id_newsgroup_id_post_idx;


--
-- Name: binary_observation_stats_defa_source_posted_at_observed_par_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_observation_incomplete_rank ATTACH PARTITION public.binary_observation_stats_defa_source_posted_at_observed_par_idx;


--
-- Name: binary_observation_stats_defa_source_posted_at_provider_id__idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_observation_stats_source_posted ATTACH PARTITION public.binary_observation_stats_defa_source_posted_at_provider_id__idx;


--
-- Name: binary_observation_stats_defa_source_posted_at_provider_id_idx1; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_observation_completeness ATTACH PARTITION public.binary_observation_stats_defa_source_posted_at_provider_id_idx1;


--
-- Name: binary_observation_stats_defa_updated_at_binary_id_source_p_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_observation_stats_singleton_updated ATTACH PARTITION public.binary_observation_stats_defa_updated_at_binary_id_source_p_idx;


--
-- Name: binary_observation_stats_default_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.binary_observation_stats_pkey ATTACH PARTITION public.binary_observation_stats_default_pkey;


--
-- Name: binary_observation_stats_default_updated_at_binary_id_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_observation_stats_incomplete_updated ATTACH PARTITION public.binary_observation_stats_default_updated_at_binary_id_idx;


--
-- Name: binary_par2_sets_default_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.binary_par2_sets_pkey ATTACH PARTITION public.binary_par2_sets_default_pkey;


--
-- Name: binary_par2_sets_default_source_posted_at_binary_id_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_par2_sets_source_posted ATTACH PARTITION public.binary_par2_sets_default_source_posted_at_binary_id_idx;


--
-- Name: binary_par2_sets_default_source_posted_at_binary_id_set_nam_key; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.binary_par2_sets_binary_id_set_name_key ATTACH PARTITION public.binary_par2_sets_default_source_posted_at_binary_id_set_nam_key;


--
-- Name: binary_par2_targets_default_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.binary_par2_targets_pkey ATTACH PARTITION public.binary_par2_targets_default_pkey;


--
-- Name: binary_par2_targets_default_source_posted_at_binary_id_file_key; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.binary_par2_targets_binary_id_file_name_key ATTACH PARTITION public.binary_par2_targets_default_source_posted_at_binary_id_file_key;


--
-- Name: binary_par2_targets_default_source_posted_at_binary_id_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_par2_targets_source_posted ATTACH PARTITION public.binary_par2_targets_default_source_posted_at_binary_id_idx;


--
-- Name: binary_parts_default_article_header_id_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_parts_article_header_id ATTACH PARTITION public.binary_parts_default_article_header_id_idx;


--
-- Name: binary_parts_default_binary_id_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_parts_binary_id ATTACH PARTITION public.binary_parts_default_binary_id_idx;


--
-- Name: binary_parts_default_binary_id_source_posted_at_part_number_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_parts_binary_source_part ATTACH PARTITION public.binary_parts_default_binary_id_source_posted_at_part_number_idx;


--
-- Name: binary_parts_default_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.binary_parts_pkey ATTACH PARTITION public.binary_parts_default_pkey;


--
-- Name: binary_parts_default_source_posted_at_article_header_id_key; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.binary_parts_article_header_id_key ATTACH PARTITION public.binary_parts_default_source_posted_at_article_header_id_key;


--
-- Name: binary_parts_default_source_posted_at_binary_id_article_hea_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_parts_source_posted ATTACH PARTITION public.binary_parts_default_source_posted_at_binary_id_article_hea_idx;


--
-- Name: binary_parts_default_source_posted_at_binary_id_part_number_key; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.binary_parts_binary_id_part_number_key ATTACH PARTITION public.binary_parts_default_source_posted_at_binary_id_part_number_key;


--
-- Name: binary_projection_events_defa_source_posted_at_event_stage__idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_projection_events_source_posted ATTACH PARTITION public.binary_projection_events_defa_source_posted_at_event_stage__idx;


--
-- Name: binary_projection_events_defa_source_posted_at_event_stage_idx1; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_projection_events_stage ATTACH PARTITION public.binary_projection_events_defa_source_posted_at_event_stage_idx1;


--
-- Name: binary_projection_events_default_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.binary_projection_events_pkey ATTACH PARTITION public.binary_projection_events_default_pkey;


--
-- Name: binary_recovery_current_defau_source_posted_at_provider_id__idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_recovery_current_source_posted ATTACH PARTITION public.binary_recovery_current_defau_source_posted_at_provider_id__idx;


--
-- Name: binary_recovery_current_defau_source_posted_at_provider_id_idx1; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_recovery_backlog ATTACH PARTITION public.binary_recovery_current_defau_source_posted_at_provider_id_idx1;


--
-- Name: binary_recovery_current_default_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.binary_recovery_current_pkey ATTACH PARTITION public.binary_recovery_current_default_pkey;


--
-- Name: binary_superseded_sources_def_source_posted_at_provider_id__idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_superseded_sources_source_posted ATTACH PARTITION public.binary_superseded_sources_def_source_posted_at_provider_id__idx;


--
-- Name: binary_superseded_sources_def_source_posted_at_provider_id_idx1; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_superseded_sources_release_family ATTACH PARTITION public.binary_superseded_sources_def_source_posted_at_provider_id_idx1;


--
-- Name: binary_superseded_sources_def_source_posted_at_target_binar_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_superseded_sources_target ATTACH PARTITION public.binary_superseded_sources_def_source_posted_at_target_binar_idx;


--
-- Name: binary_superseded_sources_default_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.binary_superseded_sources_pkey ATTACH PARTITION public.binary_superseded_sources_default_pkey;


--
-- Name: binary_text_evidence_default_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.binary_text_evidence_pkey ATTACH PARTITION public.binary_text_evidence_default_pkey;


--
-- Name: binary_text_evidence_default_source_posted_at_binary_id_sta_key; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.binary_text_evidence_binary_id_stage_name_evidence_kind_key ATTACH PARTITION public.binary_text_evidence_default_source_posted_at_binary_id_sta_key;


--
-- Name: binary_text_evidence_default_source_posted_at_stage_name_bi_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_binary_text_evidence_source_posted ATTACH PARTITION public.binary_text_evidence_default_source_posted_at_stage_name_bi_idx;


--
-- Name: poster_materialization_queue__status_ready_at_lease_expires_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_poster_materialization_queue_ready ATTACH PARTITION public.poster_materialization_queue__status_ready_at_lease_expires_idx;


--
-- Name: poster_materialization_queue_default_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.poster_materialization_queue_pkey ATTACH PARTITION public.poster_materialization_queue_default_pkey;


--
-- Name: poster_materialization_queue_status_ready_at_source_poste_idx31; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_poster_materialization_queue_ready_partition ATTACH PARTITION public.poster_materialization_queue_status_ready_at_source_poste_idx31;


--
-- Name: release_family_readiness_sum_provider_id_newsgroup_id_key_idx31; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_release_family_readiness_key_bucket_lookup ATTACH PARTITION public.release_family_readiness_sum_provider_id_newsgroup_id_key_idx31;


--
-- Name: release_family_readiness_summ_source_posted_at_provider_id__idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_release_family_readiness_source_posted ATTACH PARTITION public.release_family_readiness_summ_source_posted_at_provider_id__idx;


--
-- Name: release_family_readiness_summ_source_posted_at_provider_id_idx1; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_release_family_yenc_recovery_candidates ATTACH PARTITION public.release_family_readiness_summ_source_posted_at_provider_id_idx1;


--
-- Name: release_family_readiness_summ_source_posted_at_readiness_bu_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_release_family_readiness_bucket_lookup ATTACH PARTITION public.release_family_readiness_summ_source_posted_at_readiness_bu_idx;


--
-- Name: release_family_readiness_summ_source_posted_at_updated_at__idx1; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_release_family_readiness_summaries_release_queue ATTACH PARTITION public.release_family_readiness_summ_source_posted_at_updated_at__idx1;


--
-- Name: release_family_readiness_summ_source_posted_at_updated_at_p_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_release_family_readiness_summaries_pending ATTACH PARTITION public.release_family_readiness_summ_source_posted_at_updated_at_p_idx;


--
-- Name: release_family_readiness_summaries_default_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.release_family_readiness_summaries_pkey ATTACH PARTITION public.release_family_readiness_summaries_default_pkey;


--
-- Name: release_ready_candidates_defa_provider_id_key_kind_family_k_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_release_ready_candidates_lookup ATTACH PARTITION public.release_ready_candidates_defa_provider_id_key_kind_family_k_idx;


--
-- Name: release_ready_candidates_defa_source_posted_at_provider_id__idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_release_ready_candidates_source_posted ATTACH PARTITION public.release_ready_candidates_defa_source_posted_at_provider_id__idx;


--
-- Name: release_ready_candidates_defa_source_posted_at_updated_at_p_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_release_ready_candidates_queue ATTACH PARTITION public.release_ready_candidates_defa_source_posted_at_updated_at_p_idx;


--
-- Name: release_ready_candidates_default_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.release_ready_candidates_pkey ATTACH PARTITION public.release_ready_candidates_default_pkey;


--
-- Name: release_recovered_file_set_c_provider_id_file_set_key_sou_idx31; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_release_recovered_file_set_candidates_lookup ATTACH PARTITION public.release_recovered_file_set_c_provider_id_file_set_key_sou_idx31;


--
-- Name: release_recovered_file_set_ca_source_posted_at_provider_id__idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_release_recovered_file_set_candidates_source_posted ATTACH PARTITION public.release_recovered_file_set_ca_source_posted_at_provider_id__idx;


--
-- Name: release_recovered_file_set_ca_source_posted_at_updated_at_p_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_release_recovered_file_set_candidates_queue ATTACH PARTITION public.release_recovered_file_set_ca_source_posted_at_updated_at_p_idx;


--
-- Name: release_recovered_file_set_candidates_default_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.release_recovered_file_set_candidates_pkey ATTACH PARTITION public.release_recovered_file_set_candidates_default_pkey;


--
-- Name: release_stage_dirty_families__source_posted_at_provider_id__idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_release_stage_dirty_families_source_posted ATTACH PARTITION public.release_stage_dirty_families__source_posted_at_provider_id__idx;


--
-- Name: release_stage_dirty_families__source_posted_at_updated_at_p_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_release_stage_dirty_families_updated_at ATTACH PARTITION public.release_stage_dirty_families__source_posted_at_updated_at_p_idx;


--
-- Name: release_stage_dirty_families_default_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.release_stage_dirty_families_pkey ATTACH PARTITION public.release_stage_dirty_families_default_pkey;


--
-- Name: yenc_recovery_work_items_defa_date_utc_priority_rank_newsgr_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_yenc_recovery_work_items_ready_date_range ATTACH PARTITION public.yenc_recovery_work_items_defa_date_utc_priority_rank_newsgr_idx;


--
-- Name: yenc_recovery_work_items_defa_date_utc_priority_rank_update_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_yenc_recovery_ready_date_priority_claim ATTACH PARTITION public.yenc_recovery_work_items_defa_date_utc_priority_rank_update_idx;


--
-- Name: yenc_recovery_work_items_defa_lease_expires_at_priority_ran_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_yenc_recovery_work_items_expired_running ATTACH PARTITION public.yenc_recovery_work_items_defa_lease_expires_at_priority_ran_idx;


--
-- Name: yenc_recovery_work_items_defa_partition_day_status_provider_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_yenc_recovery_work_items_partition_day ATTACH PARTITION public.yenc_recovery_work_items_defa_partition_day_status_provider_idx;


--
-- Name: yenc_recovery_work_items_defa_priority_rank_date_utc_update_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_yenc_recovery_work_items_ready_posted_nulls_last ATTACH PARTITION public.yenc_recovery_work_items_defa_priority_rank_date_utc_update_idx;


--
-- Name: yenc_recovery_work_items_defa_priority_rank_updated_at_bina_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_yenc_recovery_work_items_ready_order ATTACH PARTITION public.yenc_recovery_work_items_defa_priority_rank_updated_at_bina_idx;


--
-- Name: yenc_recovery_work_items_defa_source_posted_at_article_head_key; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.yenc_recovery_work_items_article_header_id_key ATTACH PARTITION public.yenc_recovery_work_items_defa_source_posted_at_article_head_key;


--
-- Name: yenc_recovery_work_items_defa_status_group_tier_priority_ra_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_yenc_recovery_work_items_admission_pressure ATTACH PARTITION public.yenc_recovery_work_items_defa_status_group_tier_priority_ra_idx;


--
-- Name: yenc_recovery_work_items_defa_status_ready_at_priority_rank_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_yenc_recovery_work_items_ready ATTACH PARTITION public.yenc_recovery_work_items_defa_status_ready_at_priority_rank_idx;


--
-- Name: yenc_recovery_work_items_defa_updated_at_binary_id_source_p_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_yenc_recovery_ready_nonpriority_updated ATTACH PARTITION public.yenc_recovery_work_items_defa_updated_at_binary_id_source_p_idx;


--
-- Name: yenc_recovery_work_items_default_article_header_id_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_yenc_recovery_work_items_article_header_id ATTACH PARTITION public.yenc_recovery_work_items_default_article_header_id_idx;


--
-- Name: yenc_recovery_work_items_default_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.yenc_recovery_work_items_pkey ATTACH PARTITION public.yenc_recovery_work_items_default_pkey;


--
-- Name: yenc_recovery_work_items_default_updated_at_binary_id_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_yenc_recovery_work_items_blank_message_retire ATTACH PARTITION public.yenc_recovery_work_items_default_updated_at_binary_id_idx;


--
-- Name: article_cohort_assembly_queue article_cohort_assembly_queue_header_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.article_cohort_assembly_queue
    ADD CONSTRAINT article_cohort_assembly_queue_header_fkey FOREIGN KEY (source_posted_at, article_header_id) REFERENCES public.article_headers(source_posted_at, id) ON DELETE CASCADE;


--
-- Name: article_cohort_yenc_queue article_cohort_yenc_queue_binary_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.article_cohort_yenc_queue
    ADD CONSTRAINT article_cohort_yenc_queue_binary_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;


--
-- Name: article_cohort_yenc_queue article_cohort_yenc_queue_header_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.article_cohort_yenc_queue
    ADD CONSTRAINT article_cohort_yenc_queue_header_fkey FOREIGN KEY (source_posted_at, article_header_id) REFERENCES public.article_headers(source_posted_at, id) ON DELETE CASCADE;


--
-- Name: article_header_assembly_queue article_header_assembly_queue_article_header_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.article_header_assembly_queue
    ADD CONSTRAINT article_header_assembly_queue_article_header_id_fkey FOREIGN KEY (source_posted_at, article_header_id) REFERENCES public.article_headers(source_posted_at, id) ON DELETE CASCADE;


--
-- Name: article_header_crosspost_groups article_header_crosspost_groups_article_header_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.article_header_crosspost_groups
    ADD CONSTRAINT article_header_crosspost_groups_article_header_id_fkey FOREIGN KEY (source_posted_at, article_header_id) REFERENCES public.article_headers(source_posted_at, id) ON DELETE CASCADE;


--
-- Name: article_header_crosspost_groups article_header_crosspost_groups_source_newsgroup_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.article_header_crosspost_groups
    ADD CONSTRAINT article_header_crosspost_groups_source_newsgroup_id_fkey FOREIGN KEY (source_newsgroup_id) REFERENCES public.newsgroups(id) ON DELETE RESTRICT;


--
-- Name: article_header_ingest_payloads article_header_ingest_payloads_article_header_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.article_header_ingest_payloads
    ADD CONSTRAINT article_header_ingest_payloads_article_header_id_fkey FOREIGN KEY (source_posted_at, article_header_id) REFERENCES public.article_headers(source_posted_at, id) ON DELETE CASCADE;


--
-- Name: article_header_ingest_payloads article_header_ingest_payloads_poster_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.article_header_ingest_payloads
    ADD CONSTRAINT article_header_ingest_payloads_poster_id_fkey FOREIGN KEY (poster_id) REFERENCES public.posters(id) ON DELETE SET NULL;


--
-- Name: article_header_poster_refs article_header_poster_refs_article_header_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.article_header_poster_refs
    ADD CONSTRAINT article_header_poster_refs_article_header_id_fkey FOREIGN KEY (source_posted_at, article_header_id) REFERENCES public.article_headers(source_posted_at, id) ON DELETE CASCADE;


--
-- Name: article_header_poster_refs article_header_poster_refs_poster_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.article_header_poster_refs
    ADD CONSTRAINT article_header_poster_refs_poster_id_fkey FOREIGN KEY (poster_id) REFERENCES public.posters(id) ON DELETE CASCADE;


--
-- Name: article_headers article_headers_newsgroup_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.article_headers
    ADD CONSTRAINT article_headers_newsgroup_id_fkey FOREIGN KEY (newsgroup_id) REFERENCES public.newsgroups(id) ON DELETE RESTRICT;


--
-- Name: article_headers article_headers_provider_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.article_headers
    ADD CONSTRAINT article_headers_provider_id_fkey FOREIGN KEY (provider_id) REFERENCES public.usenet_providers(id) ON DELETE RESTRICT;


--
-- Name: binary_archive_entries binary_archive_entries_binary_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.binary_archive_entries
    ADD CONSTRAINT binary_archive_entries_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;


--
-- Name: binary_completion_keys binary_completion_keys_binary_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.binary_completion_keys
    ADD CONSTRAINT binary_completion_keys_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;


--
-- Name: binary_grouping_evidence binary_grouping_evidence_binary_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.binary_grouping_evidence
    ADD CONSTRAINT binary_grouping_evidence_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;


--
-- Name: binary_identity_current binary_identity_current_binary_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.binary_identity_current
    ADD CONSTRAINT binary_identity_current_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;


--
-- Name: binary_inspection_artifacts binary_inspection_artifacts_binary_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.binary_inspection_artifacts
    ADD CONSTRAINT binary_inspection_artifacts_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;


--
-- Name: binary_inspection_ready_queue binary_inspection_ready_queue_binary_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.binary_inspection_ready_queue
    ADD CONSTRAINT binary_inspection_ready_queue_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;


--
-- Name: binary_inspections binary_inspections_binary_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.binary_inspections
    ADD CONSTRAINT binary_inspections_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;


--
-- Name: binary_lifecycle binary_lifecycle_binary_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.binary_lifecycle
    ADD CONSTRAINT binary_lifecycle_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;


--
-- Name: binary_media_streams binary_media_streams_binary_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.binary_media_streams
    ADD CONSTRAINT binary_media_streams_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;


--
-- Name: binary_observation_stats binary_observation_stats_binary_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.binary_observation_stats
    ADD CONSTRAINT binary_observation_stats_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;


--
-- Name: binary_par2_sets binary_par2_sets_binary_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.binary_par2_sets
    ADD CONSTRAINT binary_par2_sets_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;


--
-- Name: binary_par2_targets binary_par2_targets_binary_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.binary_par2_targets
    ADD CONSTRAINT binary_par2_targets_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;


--
-- Name: binary_parts binary_parts_article_header_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.binary_parts
    ADD CONSTRAINT binary_parts_article_header_id_fkey FOREIGN KEY (source_posted_at, article_header_id) REFERENCES public.article_headers(source_posted_at, id) ON DELETE CASCADE;


--
-- Name: binary_parts binary_parts_binary_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.binary_parts
    ADD CONSTRAINT binary_parts_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;


--
-- Name: binary_projection_events binary_projection_events_binary_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.binary_projection_events
    ADD CONSTRAINT binary_projection_events_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;


--
-- Name: binary_recovery_current binary_recovery_current_binary_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.binary_recovery_current
    ADD CONSTRAINT binary_recovery_current_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;


--
-- Name: binary_superseded_sources binary_superseded_sources_source_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.binary_superseded_sources
    ADD CONSTRAINT binary_superseded_sources_source_fkey FOREIGN KEY (source_binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;


--
-- Name: binary_superseded_sources binary_superseded_sources_target_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.binary_superseded_sources
    ADD CONSTRAINT binary_superseded_sources_target_fkey FOREIGN KEY (target_binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;


--
-- Name: binary_text_evidence binary_text_evidence_binary_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.binary_text_evidence
    ADD CONSTRAINT binary_text_evidence_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;


--
-- Name: deferred_article_ranges deferred_article_ranges_newsgroup_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.deferred_article_ranges
    ADD CONSTRAINT deferred_article_ranges_newsgroup_id_fkey FOREIGN KEY (newsgroup_id) REFERENCES public.newsgroups(id) ON DELETE CASCADE;


--
-- Name: deferred_article_ranges deferred_article_ranges_provider_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.deferred_article_ranges
    ADD CONSTRAINT deferred_article_ranges_provider_id_fkey FOREIGN KEY (provider_id) REFERENCES public.usenet_providers(id) ON DELETE CASCADE;


--
-- Name: indexer_daily_bucket_stats indexer_daily_bucket_stats_newsgroup_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.indexer_daily_bucket_stats
    ADD CONSTRAINT indexer_daily_bucket_stats_newsgroup_id_fkey FOREIGN KEY (newsgroup_id) REFERENCES public.newsgroups(id) ON DELETE CASCADE;


--
-- Name: indexer_daily_bucket_stats indexer_daily_bucket_stats_provider_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.indexer_daily_bucket_stats
    ADD CONSTRAINT indexer_daily_bucket_stats_provider_id_fkey FOREIGN KEY (provider_id) REFERENCES public.usenet_providers(id) ON DELETE CASCADE;


--
-- Name: indexer_group_profiles indexer_group_profiles_newsgroup_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.indexer_group_profiles
    ADD CONSTRAINT indexer_group_profiles_newsgroup_id_fkey FOREIGN KEY (newsgroup_id) REFERENCES public.newsgroups(id) ON DELETE CASCADE;


--
-- Name: indexer_group_profiles indexer_group_profiles_provider_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.indexer_group_profiles
    ADD CONSTRAINT indexer_group_profiles_provider_id_fkey FOREIGN KEY (provider_id) REFERENCES public.usenet_providers(id) ON DELETE CASCADE;


--
-- Name: indexer_scrape_day_boundaries indexer_scrape_day_boundaries_newsgroup_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.indexer_scrape_day_boundaries
    ADD CONSTRAINT indexer_scrape_day_boundaries_newsgroup_id_fkey FOREIGN KEY (newsgroup_id) REFERENCES public.newsgroups(id) ON DELETE CASCADE;


--
-- Name: indexer_scrape_day_boundaries indexer_scrape_day_boundaries_provider_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.indexer_scrape_day_boundaries
    ADD CONSTRAINT indexer_scrape_day_boundaries_provider_id_fkey FOREIGN KEY (provider_id) REFERENCES public.usenet_providers(id) ON DELETE CASCADE;


--
-- Name: indexer_stage_state indexer_stage_state_last_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.indexer_stage_state
    ADD CONSTRAINT indexer_stage_state_last_run_id_fkey FOREIGN KEY (last_run_id) REFERENCES public.indexer_stage_runs(id) ON DELETE SET NULL;


--
-- Name: nzb_cache nzb_cache_release_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.nzb_cache
    ADD CONSTRAINT nzb_cache_release_id_fkey FOREIGN KEY (release_id) REFERENCES public.releases(release_id) ON DELETE CASCADE;


--
-- Name: poster_materialization_queue poster_materialization_queue_article_header_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.poster_materialization_queue
    ADD CONSTRAINT poster_materialization_queue_article_header_id_fkey FOREIGN KEY (source_posted_at, article_header_id) REFERENCES public.article_headers(source_posted_at, id) ON DELETE CASCADE;


--
-- Name: release_archive_detail_files release_archive_detail_files_release_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_archive_detail_files
    ADD CONSTRAINT release_archive_detail_files_release_id_fkey FOREIGN KEY (release_id) REFERENCES public.release_archive_detail_snapshots(release_id) ON DELETE CASCADE;


--
-- Name: release_archive_detail_snapshots release_archive_detail_snapshots_release_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_archive_detail_snapshots
    ADD CONSTRAINT release_archive_detail_snapshots_release_id_fkey FOREIGN KEY (release_id) REFERENCES public.release_archive_state(release_id) ON DELETE CASCADE;


--
-- Name: release_archive_detail_subtitle_languages release_archive_detail_subtitle_languages_release_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_archive_detail_subtitle_languages
    ADD CONSTRAINT release_archive_detail_subtitle_languages_release_id_fkey FOREIGN KEY (release_id) REFERENCES public.release_archive_detail_snapshots(release_id) ON DELETE CASCADE;


--
-- Name: release_archive_lineage_article_headers release_archive_lineage_article_headers_release_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_archive_lineage_article_headers
    ADD CONSTRAINT release_archive_lineage_article_headers_release_id_fkey FOREIGN KEY (release_id) REFERENCES public.release_archive_state(release_id) ON DELETE CASCADE;


--
-- Name: release_archive_lineage_binaries release_archive_lineage_binaries_release_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_archive_lineage_binaries
    ADD CONSTRAINT release_archive_lineage_binaries_release_id_fkey FOREIGN KEY (release_id) REFERENCES public.release_archive_state(release_id) ON DELETE CASCADE;


--
-- Name: release_archive_state release_archive_state_release_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_archive_state
    ADD CONSTRAINT release_archive_state_release_id_fkey FOREIGN KEY (release_id) REFERENCES public.releases(release_id) ON DELETE CASCADE;


--
-- Name: release_catalog_files release_catalog_files_release_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_catalog_files
    ADD CONSTRAINT release_catalog_files_release_id_fkey FOREIGN KEY (release_id) REFERENCES public.releases(release_id) ON DELETE CASCADE;


--
-- Name: release_family_readiness_summaries release_family_readiness_summaries_newsgroup_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.release_family_readiness_summaries
    ADD CONSTRAINT release_family_readiness_summaries_newsgroup_id_fkey FOREIGN KEY (newsgroup_id) REFERENCES public.newsgroups(id) ON DELETE CASCADE;


--
-- Name: release_family_readiness_summaries release_family_readiness_summaries_provider_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.release_family_readiness_summaries
    ADD CONSTRAINT release_family_readiness_summaries_provider_id_fkey FOREIGN KEY (provider_id) REFERENCES public.usenet_providers(id) ON DELETE CASCADE;


--
-- Name: release_files release_files_release_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_files
    ADD CONSTRAINT release_files_release_id_fkey FOREIGN KEY (release_id) REFERENCES public.releases(release_id) ON DELETE CASCADE;


--
-- Name: release_newsgroups release_newsgroups_newsgroup_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_newsgroups
    ADD CONSTRAINT release_newsgroups_newsgroup_id_fkey FOREIGN KEY (newsgroup_id) REFERENCES public.newsgroups(id) ON DELETE RESTRICT;


--
-- Name: release_newsgroups release_newsgroups_release_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_newsgroups
    ADD CONSTRAINT release_newsgroups_release_id_fkey FOREIGN KEY (release_id) REFERENCES public.releases(release_id) ON DELETE CASCADE;


--
-- Name: release_overrides release_overrides_release_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_overrides
    ADD CONSTRAINT release_overrides_release_id_fkey FOREIGN KEY (release_id) REFERENCES public.releases(release_id) ON DELETE CASCADE;


--
-- Name: release_password_candidates release_password_candidates_binary_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_password_candidates
    ADD CONSTRAINT release_password_candidates_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE SET NULL;


--
-- Name: release_password_candidates release_password_candidates_release_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_password_candidates
    ADD CONSTRAINT release_password_candidates_release_id_fkey FOREIGN KEY (release_id) REFERENCES public.releases(release_id) ON DELETE CASCADE;


--
-- Name: release_predb_matches release_predb_matches_predb_entry_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_predb_matches
    ADD CONSTRAINT release_predb_matches_predb_entry_id_fkey FOREIGN KEY (predb_entry_id) REFERENCES public.predb_entries(id) ON DELETE RESTRICT;


--
-- Name: release_predb_matches release_predb_matches_release_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_predb_matches
    ADD CONSTRAINT release_predb_matches_release_id_fkey FOREIGN KEY (release_id) REFERENCES public.releases(release_id) ON DELETE CASCADE;


--
-- Name: release_ready_candidate_acks release_ready_candidate_acks_provider_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_ready_candidate_acks
    ADD CONSTRAINT release_ready_candidate_acks_provider_id_fkey FOREIGN KEY (provider_id) REFERENCES public.usenet_providers(id) ON DELETE CASCADE;


--
-- Name: release_ready_candidates release_ready_candidates_provider_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.release_ready_candidates
    ADD CONSTRAINT release_ready_candidates_provider_id_fkey FOREIGN KEY (provider_id) REFERENCES public.usenet_providers(id) ON DELETE CASCADE;



--
-- Name: release_recovered_file_set_candidates release_recovered_file_set_candidates_provider_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.release_recovered_file_set_candidates
    ADD CONSTRAINT release_recovered_file_set_candidates_provider_id_fkey FOREIGN KEY (provider_id) REFERENCES public.usenet_providers(id) ON DELETE CASCADE;


--
-- Name: release_stage_dirty_families release_stage_dirty_families_newsgroup_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.release_stage_dirty_families
    ADD CONSTRAINT release_stage_dirty_families_newsgroup_id_fkey FOREIGN KEY (newsgroup_id) REFERENCES public.newsgroups(id) ON DELETE CASCADE;


--
-- Name: release_stage_dirty_families release_stage_dirty_families_provider_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.release_stage_dirty_families
    ADD CONSTRAINT release_stage_dirty_families_provider_id_fkey FOREIGN KEY (provider_id) REFERENCES public.usenet_providers(id) ON DELETE CASCADE;


--
-- Name: release_tmdb_matches release_tmdb_matches_release_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_tmdb_matches
    ADD CONSTRAINT release_tmdb_matches_release_id_fkey FOREIGN KEY (release_id) REFERENCES public.releases(release_id) ON DELETE CASCADE;


--
-- Name: release_tvdb_matches release_tvdb_matches_release_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_tvdb_matches
    ADD CONSTRAINT release_tvdb_matches_release_id_fkey FOREIGN KEY (release_id) REFERENCES public.releases(release_id) ON DELETE CASCADE;


--
-- Name: releases releases_provider_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.releases
    ADD CONSTRAINT releases_provider_id_fkey FOREIGN KEY (provider_id) REFERENCES public.usenet_providers(id) ON DELETE RESTRICT;


--
-- Name: scrape_checkpoints scrape_checkpoints_newsgroup_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.scrape_checkpoints
    ADD CONSTRAINT scrape_checkpoints_newsgroup_id_fkey FOREIGN KEY (newsgroup_id) REFERENCES public.newsgroups(id) ON DELETE CASCADE;


--
-- Name: scrape_checkpoints scrape_checkpoints_provider_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.scrape_checkpoints
    ADD CONSTRAINT scrape_checkpoints_provider_id_fkey FOREIGN KEY (provider_id) REFERENCES public.usenet_providers(id) ON DELETE CASCADE;


--
-- Name: scrape_runs scrape_runs_provider_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.scrape_runs
    ADD CONSTRAINT scrape_runs_provider_id_fkey FOREIGN KEY (provider_id) REFERENCES public.usenet_providers(id) ON DELETE RESTRICT;


--
-- Name: yenc_recovery_work_items yenc_recovery_work_items_article_header_source_posted_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.yenc_recovery_work_items
    ADD CONSTRAINT yenc_recovery_work_items_article_header_source_posted_fkey FOREIGN KEY (source_posted_at, article_header_id) REFERENCES public.article_headers(source_posted_at, id) ON DELETE CASCADE;


--
-- Name: yenc_recovery_work_items yenc_recovery_work_items_binary_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.yenc_recovery_work_items
    ADD CONSTRAINT yenc_recovery_work_items_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;


--
-- Name: yenc_recovery_work_items yenc_recovery_work_items_deferred_range_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.yenc_recovery_work_items
    ADD CONSTRAINT yenc_recovery_work_items_deferred_range_id_fkey FOREIGN KEY (deferred_range_id) REFERENCES public.deferred_article_ranges(id) ON DELETE SET NULL;


--
-- PostgreSQL database dump complete
--
