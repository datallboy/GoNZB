-- v0.8.0 PostgreSQL baseline generated from migrations 001-062.
-- Retired public.binaries compatibility table is intentionally omitted.

--
-- PostgreSQL database dump
--


-- Dumped from database version 17.10 (Debian 17.10-1.pgdg13+1)
-- Dumped by pg_dump version 17.10 (Debian 17.10-1.pgdg13+1)

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: article_header_assembly_queue; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.article_header_assembly_queue (
    article_header_id bigint NOT NULL,
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
WITH (fillfactor='90');


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
    poster_id bigint NOT NULL,
    poster_name text DEFAULT ''::text NOT NULL,
    poster_key text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
)
WITH (fillfactor='90');


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
    bytes bigint DEFAULT 0 NOT NULL,
    lines integer DEFAULT 0 NOT NULL,
    scraped_at timestamp with time zone DEFAULT now() NOT NULL,
    assembled_at timestamp with time zone,
    assembly_claimed_by text DEFAULT ''::text NOT NULL,
    assembly_claimed_until timestamp with time zone
);


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
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


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
    CONSTRAINT binary_completion_keys_normalized_file_name_check CHECK ((btrim(normalized_file_name) <> ''::text))
)
WITH (fillfactor='80');


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
    updated_at timestamp with time zone DEFAULT now() NOT NULL
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
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);
ALTER TABLE ONLY public.binary_grouping_evidence ALTER COLUMN payload_json SET STORAGE EXTERNAL;


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
    updated_at timestamp with time zone DEFAULT now() NOT NULL
)
WITH (fillfactor='80', autovacuum_vacuum_scale_factor='0.01', autovacuum_analyze_scale_factor='0.02', autovacuum_vacuum_threshold='5000', autovacuum_analyze_threshold='5000');


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
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


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
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


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
    updated_at timestamp with time zone DEFAULT now() NOT NULL
)
WITH (fillfactor='80', autovacuum_vacuum_scale_factor='0.01', autovacuum_analyze_scale_factor='0.02', autovacuum_vacuum_threshold='5000', autovacuum_analyze_threshold='5000');


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
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


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
    updated_at timestamp with time zone DEFAULT now() NOT NULL
)
WITH (fillfactor='80', autovacuum_vacuum_scale_factor='0.01', autovacuum_analyze_scale_factor='0.02', autovacuum_vacuum_threshold='5000', autovacuum_analyze_threshold='5000');


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
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


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
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: binary_par2_targets_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

ALTER TABLE public.binary_par2_targets ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME public.binary_par2_targets_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);


--
-- Name: binary_parts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.binary_parts (
    id bigint NOT NULL,
    binary_id bigint NOT NULL,
    article_header_id bigint NOT NULL,
    message_id text DEFAULT ''::text NOT NULL,
    part_number integer NOT NULL,
    total_parts integer DEFAULT 0 NOT NULL,
    segment_bytes bigint DEFAULT 0 NOT NULL,
    file_name text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


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
    created_at timestamp with time zone DEFAULT now() NOT NULL
)
WITH (fillfactor='100');


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
    updated_at timestamp with time zone DEFAULT now() NOT NULL
)
WITH (fillfactor='80', autovacuum_vacuum_scale_factor='0.01', autovacuum_analyze_scale_factor='0.02', autovacuum_vacuum_threshold='5000', autovacuum_analyze_threshold='5000');


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
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


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
-- Name: indexer_table_write_ownership; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.indexer_table_write_ownership (
    table_name text NOT NULL,
    owner_stage text NOT NULL,
    allowed_writer_stages text[] DEFAULT ARRAY[]::text[] NOT NULL,
    notes text DEFAULT ''::text NOT NULL,
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
WITH (fillfactor='80', autovacuum_vacuum_scale_factor='0.01', autovacuum_analyze_scale_factor='0.02', autovacuum_vacuum_threshold='5000', autovacuum_analyze_threshold='5000');


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
    recover_pending boolean DEFAULT false NOT NULL
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
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: release_recovered_file_set_candidate_acks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.release_recovered_file_set_candidate_acks (
    provider_id bigint NOT NULL,
    file_set_key text NOT NULL,
    processed_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
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
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: release_stage_dirty_families; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.release_stage_dirty_families (
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    key_kind text NOT NULL,
    family_key text NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
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
    password_state text DEFAULT 'not_passworded'::text NOT NULL,
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
    lease_expires_at timestamp with time zone
);


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
-- Name: article_header_assembly_queue article_header_assembly_queue_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_header_assembly_queue
    ADD CONSTRAINT article_header_assembly_queue_pkey PRIMARY KEY (article_header_id);


--
-- Name: article_header_crosspost_group_summary article_header_crosspost_group_summary_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_header_crosspost_group_summary
    ADD CONSTRAINT article_header_crosspost_group_summary_pkey PRIMARY KEY (observed_group_name);


--
-- Name: article_header_crosspost_groups article_header_crosspost_groups_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_header_crosspost_groups
    ADD CONSTRAINT article_header_crosspost_groups_pkey PRIMARY KEY (article_header_id, observed_group_name);


--
-- Name: article_header_ingest_payloads article_header_ingest_payloads_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_header_ingest_payloads
    ADD CONSTRAINT article_header_ingest_payloads_pkey PRIMARY KEY (article_header_id);


--
-- Name: article_header_poster_refs article_header_poster_refs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_header_poster_refs
    ADD CONSTRAINT article_header_poster_refs_pkey PRIMARY KEY (article_header_id);


--
-- Name: article_headers article_headers_newsgroup_id_article_number_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_headers
    ADD CONSTRAINT article_headers_newsgroup_id_article_number_key UNIQUE (newsgroup_id, article_number);


--
-- Name: article_headers article_headers_newsgroup_id_message_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_headers
    ADD CONSTRAINT article_headers_newsgroup_id_message_id_key UNIQUE (newsgroup_id, message_id);


--
-- Name: article_headers article_headers_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_headers
    ADD CONSTRAINT article_headers_pkey PRIMARY KEY (id);


--
-- Name: binary_archive_entries binary_archive_entries_binary_id_entry_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_archive_entries
    ADD CONSTRAINT binary_archive_entries_binary_id_entry_name_key UNIQUE (binary_id, entry_name);


--
-- Name: binary_archive_entries binary_archive_entries_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_archive_entries
    ADD CONSTRAINT binary_archive_entries_pkey PRIMARY KEY (id);


--
-- Name: binary_completion_keys binary_completion_keys_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_completion_keys
    ADD CONSTRAINT binary_completion_keys_pkey PRIMARY KEY (binary_id);


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
    ADD CONSTRAINT binary_grouping_evidence_pkey PRIMARY KEY (binary_id);


--
-- Name: binary_identity_current binary_identity_current_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_identity_current
    ADD CONSTRAINT binary_identity_current_pkey PRIMARY KEY (binary_id);


--
-- Name: binary_inspection_artifacts binary_inspection_artifacts_binary_id_stage_name_artifact_r_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_inspection_artifacts
    ADD CONSTRAINT binary_inspection_artifacts_binary_id_stage_name_artifact_r_key UNIQUE (binary_id, stage_name, artifact_role, artifact_name);


--
-- Name: binary_inspection_artifacts binary_inspection_artifacts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_inspection_artifacts
    ADD CONSTRAINT binary_inspection_artifacts_pkey PRIMARY KEY (id);


--
-- Name: binary_inspections binary_inspections_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_inspections
    ADD CONSTRAINT binary_inspections_pkey PRIMARY KEY (id);


--
-- Name: binary_inspections binary_inspections_stage_name_binary_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_inspections
    ADD CONSTRAINT binary_inspections_stage_name_binary_id_key UNIQUE (stage_name, binary_id);


--
-- Name: binary_lifecycle binary_lifecycle_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_lifecycle
    ADD CONSTRAINT binary_lifecycle_pkey PRIMARY KEY (binary_id);


--
-- Name: binary_media_streams binary_media_streams_binary_id_stream_index_stream_type_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_media_streams
    ADD CONSTRAINT binary_media_streams_binary_id_stream_index_stream_type_key UNIQUE (binary_id, stream_index, stream_type);


--
-- Name: binary_media_streams binary_media_streams_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_media_streams
    ADD CONSTRAINT binary_media_streams_pkey PRIMARY KEY (id);


--
-- Name: binary_observation_stats binary_observation_stats_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_observation_stats
    ADD CONSTRAINT binary_observation_stats_pkey PRIMARY KEY (binary_id);


--
-- Name: binary_par2_sets binary_par2_sets_binary_id_set_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_par2_sets
    ADD CONSTRAINT binary_par2_sets_binary_id_set_name_key UNIQUE (binary_id, set_name);


--
-- Name: binary_par2_sets binary_par2_sets_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_par2_sets
    ADD CONSTRAINT binary_par2_sets_pkey PRIMARY KEY (id);


--
-- Name: binary_par2_targets binary_par2_targets_binary_id_file_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_par2_targets
    ADD CONSTRAINT binary_par2_targets_binary_id_file_name_key UNIQUE (binary_id, file_name);


--
-- Name: binary_par2_targets binary_par2_targets_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_par2_targets
    ADD CONSTRAINT binary_par2_targets_pkey PRIMARY KEY (id);


--
-- Name: binary_parts binary_parts_article_header_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_parts
    ADD CONSTRAINT binary_parts_article_header_id_key UNIQUE (article_header_id);


--
-- Name: binary_parts binary_parts_binary_id_part_number_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_parts
    ADD CONSTRAINT binary_parts_binary_id_part_number_key UNIQUE (binary_id, part_number);


--
-- Name: binary_parts binary_parts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_parts
    ADD CONSTRAINT binary_parts_pkey PRIMARY KEY (id);


--
-- Name: binary_projection_events binary_projection_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_projection_events
    ADD CONSTRAINT binary_projection_events_pkey PRIMARY KEY (id);


--
-- Name: binary_recovery_current binary_recovery_current_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_recovery_current
    ADD CONSTRAINT binary_recovery_current_pkey PRIMARY KEY (binary_id);


--
-- Name: binary_text_evidence binary_text_evidence_binary_id_stage_name_evidence_kind_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_text_evidence
    ADD CONSTRAINT binary_text_evidence_binary_id_stage_name_evidence_kind_key UNIQUE (binary_id, stage_name, evidence_kind);


--
-- Name: binary_text_evidence binary_text_evidence_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_text_evidence
    ADD CONSTRAINT binary_text_evidence_pkey PRIMARY KEY (id);


--
-- Name: crosspost_popularity_refresh_queue crosspost_popularity_refresh_queue_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.crosspost_popularity_refresh_queue
    ADD CONSTRAINT crosspost_popularity_refresh_queue_pkey PRIMARY KEY (observed_group_name);


--
-- Name: indexer_dashboard_stats indexer_dashboard_stats_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.indexer_dashboard_stats
    ADD CONSTRAINT indexer_dashboard_stats_pkey PRIMARY KEY (stat_key);


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
-- Name: indexer_table_write_ownership indexer_table_write_ownership_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.indexer_table_write_ownership
    ADD CONSTRAINT indexer_table_write_ownership_pkey PRIMARY KEY (table_name);


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
    ADD CONSTRAINT poster_materialization_queue_pkey PRIMARY KEY (article_header_id);


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
    ADD CONSTRAINT release_family_readiness_summaries_pkey PRIMARY KEY (provider_id, newsgroup_id, key_kind, family_key);


--
-- Name: release_family_summary_refresh_queue release_family_summary_refresh_queue_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_family_summary_refresh_queue
    ADD CONSTRAINT release_family_summary_refresh_queue_pkey PRIMARY KEY (provider_id, newsgroup_id, key_kind, family_key);


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
    ADD CONSTRAINT release_ready_candidates_pkey PRIMARY KEY (provider_id, newsgroup_id, key_kind, family_key);


--
-- Name: release_recovered_file_set_candidate_acks release_recovered_file_set_candidate_acks_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_recovered_file_set_candidate_acks
    ADD CONSTRAINT release_recovered_file_set_candidate_acks_pkey PRIMARY KEY (provider_id, file_set_key);


--
-- Name: release_recovered_file_set_candidates release_recovered_file_set_candidates_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_recovered_file_set_candidates
    ADD CONSTRAINT release_recovered_file_set_candidates_pkey PRIMARY KEY (provider_id, file_set_key);


--
-- Name: release_stage_dirty_families release_stage_dirty_families_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_stage_dirty_families
    ADD CONSTRAINT release_stage_dirty_families_pkey PRIMARY KEY (provider_id, newsgroup_id, key_kind, family_key);


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
-- Name: yenc_recovery_work_items yenc_recovery_work_items_article_header_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.yenc_recovery_work_items
    ADD CONSTRAINT yenc_recovery_work_items_article_header_id_key UNIQUE (article_header_id);


--
-- Name: yenc_recovery_work_items yenc_recovery_work_items_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.yenc_recovery_work_items
    ADD CONSTRAINT yenc_recovery_work_items_pkey PRIMARY KEY (binary_id);


--
-- Name: idx_article_assembly_queue_claim; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_article_assembly_queue_claim ON public.article_header_assembly_queue USING btree (claim_until, article_header_id DESC);


--
-- Name: idx_article_assembly_queue_structured_match; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_article_assembly_queue_structured_match ON public.article_header_assembly_queue USING btree (provider_id, newsgroup_id, normalized_file_name, claim_until, article_header_id DESC) WHERE (normalized_file_name <> ''::text);


--
-- Name: idx_article_header_crosspost_group_summary_rank; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_article_header_crosspost_group_summary_rank ON public.article_header_crosspost_group_summary USING btree (observed_article_count DESC, distinct_message_count DESC, last_seen_at DESC, observed_group_name);


--
-- Name: idx_article_header_crosspost_groups_group_article; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_article_header_crosspost_groups_group_article ON public.article_header_crosspost_groups USING btree (observed_group_name, article_header_id) INCLUDE (message_id, source_newsgroup_id, observed_at) WHERE (btrim(observed_group_name) <> ''::text);


--
-- Name: idx_article_header_crosspost_groups_group_name_observed_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_article_header_crosspost_groups_group_name_observed_at ON public.article_header_crosspost_groups USING btree (observed_group_name, observed_at DESC);


--
-- Name: idx_article_header_crosspost_groups_observed_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_article_header_crosspost_groups_observed_at ON public.article_header_crosspost_groups USING btree (observed_at DESC);


--
-- Name: idx_article_header_crosspost_groups_provider_group; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_article_header_crosspost_groups_provider_group ON public.article_header_crosspost_groups USING btree (provider_id, observed_group_name);


--
-- Name: idx_article_header_ingest_payloads_structured_name; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_article_header_ingest_payloads_structured_name ON public.article_header_ingest_payloads USING btree (lower(btrim(subject_file_name)), article_header_id) WHERE (btrim(subject_file_name) <> ''::text);


--
-- Name: idx_article_header_ingest_payloads_yenc_recovery_ready; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_article_header_ingest_payloads_yenc_recovery_ready ON public.article_header_ingest_payloads USING btree (article_header_id, yenc_recovery_retry_after) WHERE (subject_file_name = ''::text);


--
-- Name: idx_article_header_poster_refs_poster_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_article_header_poster_refs_poster_id ON public.article_header_poster_refs USING btree (poster_id);


--
-- Name: idx_article_header_poster_refs_poster_key; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_article_header_poster_refs_poster_key ON public.article_header_poster_refs USING btree (poster_key);


--
-- Name: idx_article_headers_newsgroup_id_date_utc; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_article_headers_newsgroup_id_date_utc ON public.article_headers USING btree (newsgroup_id, date_utc DESC);


--
-- Name: idx_article_headers_pending_assembly; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_article_headers_pending_assembly ON public.article_headers USING btree (id DESC) WHERE (assembled_at IS NULL);


--
-- Name: idx_article_headers_pending_assembly_claims; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_article_headers_pending_assembly_claims ON public.article_headers USING btree (assembly_claimed_until, id DESC) WHERE (assembled_at IS NULL);


--
-- Name: idx_binary_archive_entries_binary; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_archive_entries_binary ON public.binary_archive_entries USING btree (binary_id, updated_at DESC);


--
-- Name: idx_binary_completion_keys_match; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_completion_keys_match ON public.binary_completion_keys USING btree (provider_id, newsgroup_id, normalized_file_name, is_main_payload DESC, observed_parts DESC, binary_id DESC);


--
-- Name: idx_binary_completion_keys_rank; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_completion_keys_rank ON public.binary_completion_keys USING btree (is_main_payload DESC, completion_ratio DESC, observed_parts DESC, binary_id DESC);


--
-- Name: idx_binary_core_provider_group_key; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_core_provider_group_key ON public.binary_core USING btree (provider_id, newsgroup_id, binary_key);


--
-- Name: idx_binary_identity_base_stem; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_identity_base_stem ON public.binary_identity_current USING btree (provider_id, newsgroup_id, expected_file_count, lower(btrim(base_stem))) WHERE (btrim(base_stem) <> ''::text);


--
-- Name: idx_binary_identity_base_stem_file_set_refresh; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_identity_base_stem_file_set_refresh ON public.binary_identity_current USING btree (provider_id, newsgroup_id, lower(btrim(base_stem))) INCLUDE (binary_id, file_set_key, expected_file_count, expected_archive_file_count) WHERE ((btrim(base_stem) <> ''::text) AND (btrim(file_set_key) <> ''::text) AND (GREATEST(expected_file_count, expected_archive_file_count) > 1));


--
-- Name: idx_binary_identity_current_normalized_name; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_identity_current_normalized_name ON public.binary_identity_current USING btree (provider_id, newsgroup_id, lower(btrim(COALESCE(NULLIF(file_name, ''::text), NULLIF(binary_name, ''::text)))), is_main_payload, binary_id DESC) WHERE (btrim(COALESCE(NULLIF(file_name, ''::text), NULLIF(binary_name, ''::text))) <> ''::text);


--
-- Name: idx_binary_identity_file_set; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_identity_file_set ON public.binary_identity_current USING btree (provider_id, file_set_key, newsgroup_id) WHERE (btrim(file_set_key) <> ''::text);


--
-- Name: idx_binary_identity_inspect_discovery_backlog; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_identity_inspect_discovery_backlog ON public.binary_identity_current USING btree (updated_at DESC, binary_id DESC) INCLUDE (release_family_key, base_stem, release_name, binary_name, file_name, file_index, expected_file_count, expected_archive_file_count, is_auxiliary, is_main_payload, match_confidence, match_status) WHERE (((is_main_payload = true) OR (is_auxiliary = false)) AND ((lower(COALESCE(NULLIF(file_name, ''::text), NULLIF(binary_name, ''::text), ''::text)) ~~ '%.bin'::text) OR (COALESCE(NULLIF(file_name, ''::text), NULLIF(binary_name, ''::text), ''::text) !~ '\.[A-Za-z0-9]{1,8}$'::text)));


--
-- Name: idx_binary_identity_inspect_par2_backlog; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_identity_inspect_par2_backlog ON public.binary_identity_current USING btree (updated_at DESC, binary_id DESC) INCLUDE (release_family_key, release_name, binary_name, file_name, match_confidence) WHERE (lower(COALESCE(NULLIF(file_name, ''::text), NULLIF(binary_name, ''::text), ''::text)) ~~ '%.par2'::text);


--
-- Name: idx_binary_identity_release_family; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_identity_release_family ON public.binary_identity_current USING btree (provider_id, newsgroup_id, release_family_key);


--
-- Name: idx_binary_identity_release_family_provider; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_identity_release_family_provider ON public.binary_identity_current USING btree (provider_id, release_family_key) WHERE (btrim(release_family_key) <> ''::text);


--
-- Name: idx_binary_inspection_artifacts_binary_stage; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_inspection_artifacts_binary_stage ON public.binary_inspection_artifacts USING btree (binary_id, stage_name, updated_at DESC);


--
-- Name: idx_binary_inspections_claims; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_inspections_claims ON public.binary_inspections USING btree (stage_name, inspection_claimed_until, binary_id) WHERE (inspection_claimed_by <> ''::text);


--
-- Name: idx_binary_inspections_release_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_inspections_release_id ON public.binary_inspections USING btree (release_id);


--
-- Name: idx_binary_inspections_stage_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_inspections_stage_status ON public.binary_inspections USING btree (stage_name, status, updated_at DESC);


--
-- Name: idx_binary_lifecycle_release; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_lifecycle_release ON public.binary_lifecycle USING btree (release_id, lifecycle_status);


--
-- Name: idx_binary_media_streams_binary; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_media_streams_binary ON public.binary_media_streams USING btree (binary_id, updated_at DESC);


--
-- Name: idx_binary_observation_completeness; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_observation_completeness ON public.binary_observation_stats USING btree (provider_id, newsgroup_id, observed_parts, total_parts);


--
-- Name: idx_binary_observation_incomplete; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_observation_incomplete ON public.binary_observation_stats USING btree (binary_id, observed_parts DESC, total_parts) WHERE ((total_parts > 0) AND (observed_parts < total_parts));


--
-- Name: idx_binary_observation_incomplete_rank; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_observation_incomplete_rank ON public.binary_observation_stats USING btree (observed_parts DESC, binary_id DESC) INCLUDE (provider_id, newsgroup_id, total_parts) WHERE ((total_parts > 0) AND (observed_parts < total_parts));


--
-- Name: idx_binary_par2_sets_binary; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_par2_sets_binary ON public.binary_par2_sets USING btree (binary_id, updated_at DESC);


--
-- Name: idx_binary_par2_targets_file_name; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_par2_targets_file_name ON public.binary_par2_targets USING btree (lower(btrim(file_name))) WHERE (btrim(file_name) <> ''::text);


--
-- Name: idx_binary_par2_targets_release_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_par2_targets_release_id ON public.binary_par2_targets USING btree (release_id) WHERE (btrim(release_id) <> ''::text);


--
-- Name: idx_binary_parts_article_header_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_parts_article_header_id ON public.binary_parts USING btree (article_header_id);


--
-- Name: idx_binary_parts_binary_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_parts_binary_id ON public.binary_parts USING btree (binary_id);


--
-- Name: idx_binary_parts_binary_part_article; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_parts_binary_part_article ON public.binary_parts USING btree (binary_id, part_number, id) INCLUDE (article_header_id);


--
-- Name: idx_binary_projection_events_stage; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_projection_events_stage ON public.binary_projection_events USING btree (event_stage, event_kind, created_at DESC);


--
-- Name: idx_binary_recovery_backlog; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_recovery_backlog ON public.binary_recovery_current USING btree (provider_id, newsgroup_id, recovered_source, recovered_confidence);


--
-- Name: idx_binary_recovery_inspect_par2_backlog; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_recovery_inspect_par2_backlog ON public.binary_recovery_current USING btree (updated_at DESC, binary_id DESC) WHERE ((recovered_kind = 'par2'::text) OR (recovered_extension = '.par2'::text));


--
-- Name: idx_binary_text_evidence_binary_stage; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_text_evidence_binary_stage ON public.binary_text_evidence USING btree (binary_id, stage_name, updated_at DESC);


--
-- Name: idx_crosspost_popularity_refresh_queue_ready; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_crosspost_popularity_refresh_queue_ready ON public.crosspost_popularity_refresh_queue USING btree (ready_at, observed_group_name) WHERE (status = ANY (ARRAY['pending'::text, 'failed'::text]));


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
-- Name: idx_poster_materialization_queue_poster_key; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_poster_materialization_queue_poster_key ON public.poster_materialization_queue USING btree (poster_key) WHERE (status = ANY (ARRAY['pending'::text, 'failed'::text]));


--
-- Name: idx_poster_materialization_queue_ready; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_poster_materialization_queue_ready ON public.poster_materialization_queue USING btree (ready_at, article_header_id) WHERE (status = ANY (ARRAY['pending'::text, 'failed'::text]));


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

CREATE INDEX idx_release_family_readiness_bucket_lookup ON public.release_family_readiness_summaries USING btree (readiness_bucket, provider_id, newsgroup_id, key_kind, family_key);


--
-- Name: idx_release_family_readiness_summaries_pending; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_release_family_readiness_summaries_pending ON public.release_family_readiness_summaries USING btree (updated_at, provider_id, newsgroup_id) WHERE (updated_at > COALESCE(processed_at, updated_at));


--
-- Name: idx_release_family_readiness_summaries_release_queue; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_release_family_readiness_summaries_release_queue ON public.release_family_readiness_summaries USING btree (updated_at, provider_id, newsgroup_id, key_kind, family_key) WHERE (recover_pending = false);


--
-- Name: idx_release_family_summary_refresh_queue_base_stem; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_release_family_summary_refresh_queue_base_stem ON public.release_family_summary_refresh_queue USING btree (queued_at, provider_id, newsgroup_id, family_key) WHERE (key_kind = 'base_stem'::text);


--
-- Name: idx_release_family_summary_refresh_queue_queued_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_release_family_summary_refresh_queue_queued_at ON public.release_family_summary_refresh_queue USING btree (queued_at, provider_id, newsgroup_id, key_kind, family_key);


--
-- Name: idx_release_family_yenc_recovery_candidates; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_release_family_yenc_recovery_candidates ON public.release_family_readiness_summaries USING btree (provider_id, newsgroup_id, family_key) WHERE ((key_kind = 'release_family'::text) AND (readiness_bucket = ANY (ARRAY['overgrouped_contextual'::text, 'weak_single_binary'::text, 'weak_obfuscated_set'::text])));


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
-- Name: idx_release_ready_candidates_queue; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_release_ready_candidates_queue ON public.release_ready_candidates USING btree (updated_at, provider_id, newsgroup_id, key_kind, family_key);


--
-- Name: idx_release_recovered_file_set_candidates_queue; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_release_recovered_file_set_candidates_queue ON public.release_recovered_file_set_candidates USING btree (updated_at, provider_id, representative_newsgroup_id, file_set_key);


--
-- Name: idx_release_stage_dirty_families_updated_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_release_stage_dirty_families_updated_at ON public.release_stage_dirty_families USING btree (updated_at, provider_id, newsgroup_id);


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
-- Name: idx_yenc_recovery_work_items_article_header_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_yenc_recovery_work_items_article_header_id ON public.yenc_recovery_work_items USING btree (article_header_id);


--
-- Name: idx_yenc_recovery_work_items_expired_running; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_yenc_recovery_work_items_expired_running ON public.yenc_recovery_work_items USING btree (lease_expires_at, priority_rank, updated_at DESC, binary_id) WHERE (status = 'running'::text);


--
-- Name: idx_yenc_recovery_work_items_ready; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_yenc_recovery_work_items_ready ON public.yenc_recovery_work_items USING btree (status, ready_at, priority_rank, updated_at DESC, binary_id) WHERE (status = 'ready'::text);


--
-- Name: idx_yenc_recovery_work_items_ready_order; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_yenc_recovery_work_items_ready_order ON public.yenc_recovery_work_items USING btree (priority_rank, updated_at DESC, binary_id) WHERE (status = 'ready'::text);


--
-- Name: article_header_assembly_queue article_header_assembly_queue_article_header_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_header_assembly_queue
    ADD CONSTRAINT article_header_assembly_queue_article_header_id_fkey FOREIGN KEY (article_header_id) REFERENCES public.article_headers(id) ON DELETE CASCADE;


--
-- Name: article_header_crosspost_groups article_header_crosspost_groups_article_header_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_header_crosspost_groups
    ADD CONSTRAINT article_header_crosspost_groups_article_header_id_fkey FOREIGN KEY (article_header_id) REFERENCES public.article_headers(id) ON DELETE CASCADE;


--
-- Name: article_header_crosspost_groups article_header_crosspost_groups_source_newsgroup_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_header_crosspost_groups
    ADD CONSTRAINT article_header_crosspost_groups_source_newsgroup_id_fkey FOREIGN KEY (source_newsgroup_id) REFERENCES public.newsgroups(id) ON DELETE RESTRICT;


--
-- Name: article_header_ingest_payloads article_header_ingest_payloads_article_header_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_header_ingest_payloads
    ADD CONSTRAINT article_header_ingest_payloads_article_header_id_fkey FOREIGN KEY (article_header_id) REFERENCES public.article_headers(id) ON DELETE CASCADE;


--
-- Name: article_header_ingest_payloads article_header_ingest_payloads_poster_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_header_ingest_payloads
    ADD CONSTRAINT article_header_ingest_payloads_poster_id_fkey FOREIGN KEY (poster_id) REFERENCES public.posters(id) ON DELETE SET NULL;


--
-- Name: article_header_poster_refs article_header_poster_refs_article_header_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_header_poster_refs
    ADD CONSTRAINT article_header_poster_refs_article_header_id_fkey FOREIGN KEY (article_header_id) REFERENCES public.article_headers(id) ON DELETE CASCADE;


--
-- Name: article_header_poster_refs article_header_poster_refs_poster_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_header_poster_refs
    ADD CONSTRAINT article_header_poster_refs_poster_id_fkey FOREIGN KEY (poster_id) REFERENCES public.posters(id) ON DELETE CASCADE;


--
-- Name: article_headers article_headers_newsgroup_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_headers
    ADD CONSTRAINT article_headers_newsgroup_id_fkey FOREIGN KEY (newsgroup_id) REFERENCES public.newsgroups(id) ON DELETE RESTRICT;


--
-- Name: article_headers article_headers_provider_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_headers
    ADD CONSTRAINT article_headers_provider_id_fkey FOREIGN KEY (provider_id) REFERENCES public.usenet_providers(id) ON DELETE RESTRICT;


--
-- Name: binary_archive_entries binary_archive_entries_binary_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_archive_entries
    ADD CONSTRAINT binary_archive_entries_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;


--
-- Name: binary_completion_keys binary_completion_keys_binary_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_completion_keys
    ADD CONSTRAINT binary_completion_keys_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;


--
-- Name: binary_grouping_evidence binary_grouping_evidence_binary_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_grouping_evidence
    ADD CONSTRAINT binary_grouping_evidence_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;


--
-- Name: binary_identity_current binary_identity_current_binary_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_identity_current
    ADD CONSTRAINT binary_identity_current_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;


--
-- Name: binary_inspection_artifacts binary_inspection_artifacts_binary_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_inspection_artifacts
    ADD CONSTRAINT binary_inspection_artifacts_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;


--
-- Name: binary_inspections binary_inspections_binary_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_inspections
    ADD CONSTRAINT binary_inspections_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;


--
-- Name: binary_lifecycle binary_lifecycle_binary_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_lifecycle
    ADD CONSTRAINT binary_lifecycle_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;


--
-- Name: binary_media_streams binary_media_streams_binary_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_media_streams
    ADD CONSTRAINT binary_media_streams_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;


--
-- Name: binary_observation_stats binary_observation_stats_binary_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_observation_stats
    ADD CONSTRAINT binary_observation_stats_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;


--
-- Name: binary_par2_sets binary_par2_sets_binary_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_par2_sets
    ADD CONSTRAINT binary_par2_sets_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;


--
-- Name: binary_par2_targets binary_par2_targets_binary_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_par2_targets
    ADD CONSTRAINT binary_par2_targets_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;


--
-- Name: binary_parts binary_parts_article_header_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_parts
    ADD CONSTRAINT binary_parts_article_header_id_fkey FOREIGN KEY (article_header_id) REFERENCES public.article_headers(id) ON DELETE CASCADE;


--
-- Name: binary_parts binary_parts_binary_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_parts
    ADD CONSTRAINT binary_parts_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;


--
-- Name: binary_projection_events binary_projection_events_binary_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_projection_events
    ADD CONSTRAINT binary_projection_events_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;


--
-- Name: binary_recovery_current binary_recovery_current_binary_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_recovery_current
    ADD CONSTRAINT binary_recovery_current_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;


--
-- Name: binary_text_evidence binary_text_evidence_binary_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_text_evidence
    ADD CONSTRAINT binary_text_evidence_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;


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

ALTER TABLE ONLY public.poster_materialization_queue
    ADD CONSTRAINT poster_materialization_queue_article_header_id_fkey FOREIGN KEY (article_header_id) REFERENCES public.article_headers(id) ON DELETE CASCADE;


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

ALTER TABLE ONLY public.release_family_readiness_summaries
    ADD CONSTRAINT release_family_readiness_summaries_newsgroup_id_fkey FOREIGN KEY (newsgroup_id) REFERENCES public.newsgroups(id) ON DELETE CASCADE;


--
-- Name: release_family_readiness_summaries release_family_readiness_summaries_provider_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_family_readiness_summaries
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

ALTER TABLE ONLY public.release_ready_candidates
    ADD CONSTRAINT release_ready_candidates_provider_id_fkey FOREIGN KEY (provider_id) REFERENCES public.usenet_providers(id) ON DELETE CASCADE;


--
-- Name: release_recovered_file_set_candidate_acks release_recovered_file_set_candidate_acks_provider_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_recovered_file_set_candidate_acks
    ADD CONSTRAINT release_recovered_file_set_candidate_acks_provider_id_fkey FOREIGN KEY (provider_id) REFERENCES public.usenet_providers(id) ON DELETE CASCADE;


--
-- Name: release_recovered_file_set_candidates release_recovered_file_set_candidates_provider_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_recovered_file_set_candidates
    ADD CONSTRAINT release_recovered_file_set_candidates_provider_id_fkey FOREIGN KEY (provider_id) REFERENCES public.usenet_providers(id) ON DELETE CASCADE;


--
-- Name: release_stage_dirty_families release_stage_dirty_families_newsgroup_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_stage_dirty_families
    ADD CONSTRAINT release_stage_dirty_families_newsgroup_id_fkey FOREIGN KEY (newsgroup_id) REFERENCES public.newsgroups(id) ON DELETE CASCADE;


--
-- Name: release_stage_dirty_families release_stage_dirty_families_provider_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_stage_dirty_families
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
-- Name: yenc_recovery_work_items yenc_recovery_work_items_article_header_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.yenc_recovery_work_items
    ADD CONSTRAINT yenc_recovery_work_items_article_header_id_fkey FOREIGN KEY (article_header_id) REFERENCES public.article_headers(id) ON DELETE CASCADE;


--
-- Name: yenc_recovery_work_items yenc_recovery_work_items_binary_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.yenc_recovery_work_items
    ADD CONSTRAINT yenc_recovery_work_items_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE;


--
-- PostgreSQL database dump complete
--
