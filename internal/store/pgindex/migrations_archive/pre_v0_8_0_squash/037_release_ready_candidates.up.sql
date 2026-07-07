CREATE TABLE IF NOT EXISTS public.release_ready_candidates (
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    key_kind text NOT NULL,
    family_key text NOT NULL,
    source_release_key text NOT NULL DEFAULT ''::text,
    release_key text NOT NULL DEFAULT ''::text,
    release_name text NOT NULL DEFAULT ''::text,
    binary_count integer NOT NULL DEFAULT 0,
    complete_binary_count integer NOT NULL DEFAULT 0,
    complete_main_payload_binary_count integer NOT NULL DEFAULT 0,
    expected_file_count integer NOT NULL DEFAULT 0,
    expected_archive_file_count integer NOT NULL DEFAULT 0,
    has_expected_file_count boolean NOT NULL DEFAULT false,
    has_expected_archive_file_count boolean NOT NULL DEFAULT false,
    expected_file_coverage_pct double precision NOT NULL DEFAULT 0,
    archive_file_coverage_pct double precision NOT NULL DEFAULT 0,
    total_bytes bigint NOT NULL DEFAULT 0,
    earliest_posted_at timestamptz,
    ready_reason text NOT NULL DEFAULT 'actionable'::text,
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    CONSTRAINT release_ready_candidates_pkey PRIMARY KEY (provider_id, newsgroup_id, key_kind, family_key),
    CONSTRAINT release_ready_candidates_provider_id_fkey
        FOREIGN KEY (provider_id) REFERENCES public.usenet_providers(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_release_ready_candidates_queue
    ON public.release_ready_candidates (
        updated_at,
        provider_id,
        newsgroup_id,
        key_kind,
        family_key
    );

CREATE TABLE IF NOT EXISTS public.release_ready_candidate_acks (
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    key_kind text NOT NULL,
    family_key text NOT NULL,
    processed_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    CONSTRAINT release_ready_candidate_acks_pkey PRIMARY KEY (provider_id, newsgroup_id, key_kind, family_key),
    CONSTRAINT release_ready_candidate_acks_provider_id_fkey
        FOREIGN KEY (provider_id) REFERENCES public.usenet_providers(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_release_ready_candidate_acks_processed_at
    ON public.release_ready_candidate_acks (processed_at);
