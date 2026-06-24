
-- Dumped from database version 17.9 (Debian 17.9-1.pgdg13+1)
-- Dumped by pg_dump version 17.9 (Debian 17.9-1.pgdg13+1)

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

SET default_tablespace = '';

SET default_table_access_method = heap;

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
    created_at timestamp with time zone DEFAULT now() NOT NULL
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
    bytes bigint DEFAULT 0 NOT NULL,
    lines integer DEFAULT 0 NOT NULL,
    scraped_at timestamp with time zone DEFAULT now() NOT NULL,
    assembled_at timestamp with time zone
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
-- Name: binaries; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.binaries (
    id bigint NOT NULL,
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    poster_id bigint,
    release_key text NOT NULL,
    release_name text DEFAULT ''::text NOT NULL,
    binary_key text NOT NULL,
    binary_name text DEFAULT ''::text NOT NULL,
    file_name text DEFAULT ''::text NOT NULL,
    total_parts integer DEFAULT 0 NOT NULL,
    observed_parts integer DEFAULT 0 NOT NULL,
    total_bytes bigint DEFAULT 0 NOT NULL,
    first_article_number bigint DEFAULT 0 NOT NULL,
    last_article_number bigint DEFAULT 0 NOT NULL,
    posted_at timestamp with time zone,
    status text DEFAULT 'assembled'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    match_confidence double precision DEFAULT 0 NOT NULL,
    match_status text DEFAULT 'low_confidence'::text NOT NULL,
    grouping_evidence_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    file_index integer DEFAULT 0 NOT NULL,
    expected_file_count integer DEFAULT 0 NOT NULL,
    expected_archive_file_count integer DEFAULT 0 NOT NULL,
    source_release_key text DEFAULT ''::text NOT NULL,
    release_family_key text DEFAULT ''::text NOT NULL,
    file_family_key text DEFAULT ''::text NOT NULL,
    family_kind text DEFAULT ''::text NOT NULL,
    base_stem text DEFAULT ''::text NOT NULL,
    recovered_kind text DEFAULT ''::text NOT NULL,
    recovered_extension text DEFAULT ''::text NOT NULL,
    recovered_source text DEFAULT ''::text NOT NULL,
    recovered_confidence double precision DEFAULT 0 NOT NULL,
    recovered_at timestamp with time zone,
    is_auxiliary boolean DEFAULT false NOT NULL,
    is_main_payload boolean DEFAULT false NOT NULL
);


--
-- Name: binaries_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.binaries_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: binaries_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.binaries_id_seq OWNED BY public.binaries.id;


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
-- Name: binary_grouping_evidence; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.binary_grouping_evidence (
    binary_id bigint NOT NULL,
    evidence_source text DEFAULT 'matcher'::text NOT NULL,
    evidence_version text DEFAULT 'v1'::text NOT NULL,
    payload_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
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
-- Name: release_file_articles; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.release_file_articles (
    id bigint NOT NULL,
    release_file_id bigint NOT NULL,
    article_header_id bigint NOT NULL,
    part_number integer DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: release_file_articles_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.release_file_articles_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: release_file_articles_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.release_file_articles_id_seq OWNED BY public.release_file_articles.id;


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
    release_family_key text DEFAULT ''::text NOT NULL
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
-- Name: article_headers id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_headers ALTER COLUMN id SET DEFAULT nextval('public.article_headers_id_seq'::regclass);


--
-- Name: binaries id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binaries ALTER COLUMN id SET DEFAULT nextval('public.binaries_id_seq'::regclass);


--
-- Name: binary_archive_entries id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_archive_entries ALTER COLUMN id SET DEFAULT nextval('public.binary_archive_entries_id_seq'::regclass);


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
-- Name: release_file_articles id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_file_articles ALTER COLUMN id SET DEFAULT nextval('public.release_file_articles_id_seq'::regclass);


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
-- Name: article_header_ingest_payloads article_header_ingest_payloads_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.article_header_ingest_payloads
    ADD CONSTRAINT article_header_ingest_payloads_pkey PRIMARY KEY (article_header_id);


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
-- Name: binaries binaries_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binaries
    ADD CONSTRAINT binaries_pkey PRIMARY KEY (id);


--
-- Name: binaries binaries_provider_id_newsgroup_id_binary_key_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binaries
    ADD CONSTRAINT binaries_provider_id_newsgroup_id_binary_key_key UNIQUE (provider_id, newsgroup_id, binary_key);


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
-- Name: binary_grouping_evidence binary_grouping_evidence_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_grouping_evidence
    ADD CONSTRAINT binary_grouping_evidence_pkey PRIMARY KEY (binary_id);


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
-- Name: release_file_articles release_file_articles_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_file_articles
    ADD CONSTRAINT release_file_articles_pkey PRIMARY KEY (id);


--
-- Name: release_file_articles release_file_articles_release_file_id_article_header_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_file_articles
    ADD CONSTRAINT release_file_articles_release_file_id_article_header_id_key UNIQUE (release_file_id, article_header_id);


--
-- Name: release_file_articles release_file_articles_release_file_id_part_number_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_file_articles
    ADD CONSTRAINT release_file_articles_release_file_id_part_number_key UNIQUE (release_file_id, part_number);


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
-- Name: idx_article_headers_newsgroup_id_date_utc; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_article_headers_newsgroup_id_date_utc ON public.article_headers USING btree (newsgroup_id, date_utc DESC);


--
-- Name: idx_article_headers_pending_assembly; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_article_headers_pending_assembly ON public.article_headers USING btree (id DESC) WHERE (assembled_at IS NULL);


--
-- Name: idx_binaries_base_stem_expected_family; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binaries_base_stem_expected_family ON public.binaries USING btree (provider_id, newsgroup_id, expected_file_count, lower(btrim(base_stem))) WHERE (btrim(base_stem) <> ''::text);


--
-- Name: idx_binaries_poster_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binaries_poster_id ON public.binaries USING btree (poster_id);


--
-- Name: idx_binaries_release_family_key; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binaries_release_family_key ON public.binaries USING btree (provider_id, newsgroup_id, release_family_key);


--
-- Name: idx_binaries_updated_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binaries_updated_at ON public.binaries USING btree (updated_at DESC);


--
-- Name: idx_binary_archive_entries_binary; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_archive_entries_binary ON public.binary_archive_entries USING btree (binary_id, updated_at DESC);


--
-- Name: idx_binary_inspection_artifacts_binary_stage; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_inspection_artifacts_binary_stage ON public.binary_inspection_artifacts USING btree (binary_id, stage_name, updated_at DESC);


--
-- Name: idx_binary_inspections_release_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_inspections_release_id ON public.binary_inspections USING btree (release_id);


--
-- Name: idx_binary_inspections_stage_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_inspections_stage_status ON public.binary_inspections USING btree (stage_name, status, updated_at DESC);

CREATE INDEX idx_binary_inspections_claims ON public.binary_inspections USING btree (stage_name, inspection_claimed_until, binary_id) WHERE (inspection_claimed_by <> ''::text);


--
-- Name: idx_binary_media_streams_binary; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_media_streams_binary ON public.binary_media_streams USING btree (binary_id, updated_at DESC);


--
-- Name: idx_binary_par2_sets_binary; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_par2_sets_binary ON public.binary_par2_sets USING btree (binary_id, updated_at DESC);


--
-- Name: idx_binary_parts_binary_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_parts_binary_id ON public.binary_parts USING btree (binary_id);


--
-- Name: idx_binary_text_evidence_binary_stage; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_binary_text_evidence_binary_stage ON public.binary_text_evidence USING btree (binary_id, stage_name, updated_at DESC);


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
-- Name: idx_predb_backfill_checkpoints_updated_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_predb_backfill_checkpoints_updated_at ON public.predb_backfill_checkpoints USING btree (updated_at DESC);


--
-- Name: idx_predb_entries_source_external_id; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_predb_entries_source_external_id ON public.predb_entries USING btree (source, external_id) WHERE (external_id > 0);


--
-- Name: idx_release_file_articles_release_file_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_release_file_articles_release_file_id ON public.release_file_articles USING btree (release_file_id);


--
-- Name: idx_release_files_release_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_release_files_release_id ON public.release_files USING btree (release_id);


--
-- Name: idx_release_password_candidates_release_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_release_password_candidates_release_status ON public.release_password_candidates USING btree (release_id, verification_status, updated_at DESC);


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
-- Name: binaries binaries_newsgroup_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binaries
    ADD CONSTRAINT binaries_newsgroup_id_fkey FOREIGN KEY (newsgroup_id) REFERENCES public.newsgroups(id) ON DELETE RESTRICT;


--
-- Name: binaries binaries_poster_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binaries
    ADD CONSTRAINT binaries_poster_id_fkey FOREIGN KEY (poster_id) REFERENCES public.posters(id) ON DELETE SET NULL;


--
-- Name: binaries binaries_provider_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binaries
    ADD CONSTRAINT binaries_provider_id_fkey FOREIGN KEY (provider_id) REFERENCES public.usenet_providers(id) ON DELETE RESTRICT;


--
-- Name: binary_archive_entries binary_archive_entries_binary_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_archive_entries
    ADD CONSTRAINT binary_archive_entries_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binaries(id) ON DELETE CASCADE;


--
-- Name: binary_archive_entries binary_archive_entries_release_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_archive_entries
    ADD CONSTRAINT binary_archive_entries_release_id_fkey FOREIGN KEY (release_id) REFERENCES public.releases(release_id) ON DELETE SET NULL;


--
-- Name: binary_grouping_evidence binary_grouping_evidence_binary_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_grouping_evidence
    ADD CONSTRAINT binary_grouping_evidence_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binaries(id) ON DELETE CASCADE;


--
-- Name: binary_inspection_artifacts binary_inspection_artifacts_binary_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_inspection_artifacts
    ADD CONSTRAINT binary_inspection_artifacts_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binaries(id) ON DELETE CASCADE;


--
-- Name: binary_inspection_artifacts binary_inspection_artifacts_release_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_inspection_artifacts
    ADD CONSTRAINT binary_inspection_artifacts_release_id_fkey FOREIGN KEY (release_id) REFERENCES public.releases(release_id) ON DELETE SET NULL;


--
-- Name: binary_inspections binary_inspections_binary_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_inspections
    ADD CONSTRAINT binary_inspections_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binaries(id) ON DELETE CASCADE;


--
-- Name: binary_inspections binary_inspections_release_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_inspections
    ADD CONSTRAINT binary_inspections_release_id_fkey FOREIGN KEY (release_id) REFERENCES public.releases(release_id) ON DELETE SET NULL;


--
-- Name: binary_media_streams binary_media_streams_binary_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_media_streams
    ADD CONSTRAINT binary_media_streams_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binaries(id) ON DELETE CASCADE;


--
-- Name: binary_media_streams binary_media_streams_release_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_media_streams
    ADD CONSTRAINT binary_media_streams_release_id_fkey FOREIGN KEY (release_id) REFERENCES public.releases(release_id) ON DELETE SET NULL;


--
-- Name: binary_par2_sets binary_par2_sets_binary_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_par2_sets
    ADD CONSTRAINT binary_par2_sets_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binaries(id) ON DELETE CASCADE;


--
-- Name: binary_par2_sets binary_par2_sets_release_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_par2_sets
    ADD CONSTRAINT binary_par2_sets_release_id_fkey FOREIGN KEY (release_id) REFERENCES public.releases(release_id) ON DELETE SET NULL;


--
-- Name: binary_parts binary_parts_article_header_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_parts
    ADD CONSTRAINT binary_parts_article_header_id_fkey FOREIGN KEY (article_header_id) REFERENCES public.article_headers(id) ON DELETE CASCADE;


--
-- Name: binary_parts binary_parts_binary_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_parts
    ADD CONSTRAINT binary_parts_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binaries(id) ON DELETE CASCADE;


--
-- Name: binary_text_evidence binary_text_evidence_binary_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_text_evidence
    ADD CONSTRAINT binary_text_evidence_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binaries(id) ON DELETE CASCADE;


--
-- Name: binary_text_evidence binary_text_evidence_release_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.binary_text_evidence
    ADD CONSTRAINT binary_text_evidence_release_id_fkey FOREIGN KEY (release_id) REFERENCES public.releases(release_id) ON DELETE SET NULL;


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
-- Name: release_file_articles release_file_articles_article_header_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_file_articles
    ADD CONSTRAINT release_file_articles_article_header_id_fkey FOREIGN KEY (article_header_id) REFERENCES public.article_headers(id) ON DELETE CASCADE;


--
-- Name: release_file_articles release_file_articles_release_file_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_file_articles
    ADD CONSTRAINT release_file_articles_release_file_id_fkey FOREIGN KEY (release_file_id) REFERENCES public.release_files(id) ON DELETE CASCADE;


--
-- Name: release_files release_files_binary_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_files
    ADD CONSTRAINT release_files_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binaries(id) ON DELETE SET NULL;


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
-- Name: release_password_candidates release_password_candidates_binary_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_password_candidates
    ADD CONSTRAINT release_password_candidates_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binaries(id) ON DELETE SET NULL;


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
