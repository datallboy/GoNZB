CREATE TABLE IF NOT EXISTS public.indexer_table_write_ownership (
    table_name text PRIMARY KEY,
    owner_stage text NOT NULL,
    allowed_writer_stages text[] NOT NULL DEFAULT ARRAY[]::text[],
    notes text DEFAULT ''::text NOT NULL,
    updated_at timestamptz DEFAULT now() NOT NULL
);

INSERT INTO public.indexer_table_write_ownership (table_name, owner_stage, allowed_writer_stages, notes)
VALUES
    ('binary_core', 'assemble', ARRAY['assemble_lane_a', 'assemble_lane_b'], 'V2 binary anchor projection. Existing binaries table remains the temporary FK/read compatibility anchor during Phase A.'),
    ('binary_observation_stats', 'assemble', ARRAY['assemble_lane_a', 'assemble_lane_b', 'recover_yenc'], 'Mutable part/byte/article bounds; recover_yenc may refresh stats after merge.'),
    ('binary_identity_current', 'assemble', ARRAY['assemble_lane_a', 'assemble_lane_b', 'recover_yenc'], 'Current release-family/file-set identity. Recovery may promote stronger identity discovered from yEnc headers.'),
    ('binary_recovery_current', 'recover_yenc', ARRAY['recover_yenc', 'inspect_discovery', 'inspect_par2'], 'Recovered filename/kind/source confidence projection.'),
    ('binary_lifecycle', 'release_archive', ARRAY['release_archive_nzb', 'release_purge_archived_sources'], 'Release/archive/purge lifecycle projection.'),
    ('binary_projection_events', 'projector', ARRAY['assemble_lane_a', 'assemble_lane_b', 'recover_yenc', 'inspect_discovery', 'inspect_par2', 'inspect_archive', 'inspect_media', 'release'], 'Append-only future bridge for cross-stage state transitions.')
ON CONFLICT (table_name) DO UPDATE
SET owner_stage = EXCLUDED.owner_stage,
    allowed_writer_stages = EXCLUDED.allowed_writer_stages,
    notes = EXCLUDED.notes,
    updated_at = now();

CREATE TABLE IF NOT EXISTS public.binary_core (
    binary_id bigint PRIMARY KEY REFERENCES public.binaries(id) ON DELETE CASCADE,
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    poster_id bigint,
    binary_key text NOT NULL,
    original_binary_name text DEFAULT ''::text NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    updated_at timestamptz DEFAULT now() NOT NULL,
    UNIQUE (provider_id, newsgroup_id, binary_key)
) WITH (fillfactor = 100);

CREATE TABLE IF NOT EXISTS public.binary_observation_stats (
    binary_id bigint PRIMARY KEY REFERENCES public.binaries(id) ON DELETE CASCADE,
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    total_parts integer DEFAULT 0 NOT NULL,
    observed_parts integer DEFAULT 0 NOT NULL,
    total_bytes bigint DEFAULT 0 NOT NULL,
    first_article_number bigint DEFAULT 0 NOT NULL,
    last_article_number bigint DEFAULT 0 NOT NULL,
    posted_at timestamptz,
    refreshed_at timestamptz DEFAULT now() NOT NULL,
    updated_at timestamptz DEFAULT now() NOT NULL
) WITH (fillfactor = 80);

CREATE TABLE IF NOT EXISTS public.binary_identity_current (
    binary_id bigint PRIMARY KEY REFERENCES public.binaries(id) ON DELETE CASCADE,
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
    updated_at timestamptz DEFAULT now() NOT NULL
) WITH (fillfactor = 80);

CREATE TABLE IF NOT EXISTS public.binary_recovery_current (
    binary_id bigint PRIMARY KEY REFERENCES public.binaries(id) ON DELETE CASCADE,
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    recovered_kind text DEFAULT ''::text NOT NULL,
    recovered_extension text DEFAULT ''::text NOT NULL,
    recovered_source text DEFAULT ''::text NOT NULL,
    recovered_confidence double precision DEFAULT 0 NOT NULL,
    recovered_file_name text DEFAULT ''::text NOT NULL,
    recovered_at timestamptz,
    updated_at timestamptz DEFAULT now() NOT NULL
) WITH (fillfactor = 80);

CREATE TABLE IF NOT EXISTS public.binary_lifecycle (
    binary_id bigint PRIMARY KEY REFERENCES public.binaries(id) ON DELETE CASCADE,
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    release_id text DEFAULT ''::text NOT NULL,
    lifecycle_status text DEFAULT 'active'::text NOT NULL,
    archived_at timestamptz,
    purge_eligible_at timestamptz,
    purged_at timestamptz,
    updated_at timestamptz DEFAULT now() NOT NULL
) WITH (fillfactor = 80);

CREATE TABLE IF NOT EXISTS public.binary_projection_events (
    id bigserial PRIMARY KEY,
    binary_id bigint REFERENCES public.binaries(id) ON DELETE CASCADE,
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    event_stage text NOT NULL,
    event_kind text NOT NULL,
    event_key text DEFAULT ''::text NOT NULL,
    event_value text DEFAULT ''::text NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL
) WITH (fillfactor = 100);

CREATE INDEX IF NOT EXISTS idx_binary_core_provider_group_key
    ON public.binary_core (provider_id, newsgroup_id, binary_key);

CREATE INDEX IF NOT EXISTS idx_binary_identity_release_family
    ON public.binary_identity_current (provider_id, newsgroup_id, release_family_key);

CREATE INDEX IF NOT EXISTS idx_binary_identity_file_set
    ON public.binary_identity_current (provider_id, file_set_key, newsgroup_id)
    WHERE btrim(file_set_key) <> '';

CREATE INDEX IF NOT EXISTS idx_binary_identity_base_stem
    ON public.binary_identity_current (provider_id, newsgroup_id, expected_file_count, lower(btrim(base_stem)))
    WHERE btrim(base_stem) <> '';

CREATE INDEX IF NOT EXISTS idx_binary_observation_completeness
    ON public.binary_observation_stats (provider_id, newsgroup_id, observed_parts, total_parts);

CREATE INDEX IF NOT EXISTS idx_binary_recovery_backlog
    ON public.binary_recovery_current (provider_id, newsgroup_id, recovered_source, recovered_confidence);

CREATE INDEX IF NOT EXISTS idx_binary_lifecycle_release
    ON public.binary_lifecycle (release_id, lifecycle_status);

CREATE INDEX IF NOT EXISTS idx_binary_projection_events_stage
    ON public.binary_projection_events (event_stage, event_kind, created_at DESC);

ALTER TABLE public.binary_observation_stats SET (
    autovacuum_vacuum_scale_factor = 0.01,
    autovacuum_analyze_scale_factor = 0.02,
    autovacuum_vacuum_threshold = 5000,
    autovacuum_analyze_threshold = 5000
);

ALTER TABLE public.binary_identity_current SET (
    autovacuum_vacuum_scale_factor = 0.01,
    autovacuum_analyze_scale_factor = 0.02,
    autovacuum_vacuum_threshold = 5000,
    autovacuum_analyze_threshold = 5000
);

ALTER TABLE public.binary_recovery_current SET (
    autovacuum_vacuum_scale_factor = 0.01,
    autovacuum_analyze_scale_factor = 0.02,
    autovacuum_vacuum_threshold = 5000,
    autovacuum_analyze_threshold = 5000
);

ALTER TABLE public.binary_lifecycle SET (
    autovacuum_vacuum_scale_factor = 0.01,
    autovacuum_analyze_scale_factor = 0.02,
    autovacuum_vacuum_threshold = 5000,
    autovacuum_analyze_threshold = 5000
);
