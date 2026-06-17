ALTER TABLE public.release_family_readiness_summaries
    ADD COLUMN IF NOT EXISTS recover_pending boolean DEFAULT false NOT NULL;

CREATE INDEX IF NOT EXISTS idx_release_family_readiness_summaries_release_queue
    ON public.release_family_readiness_summaries (
        updated_at,
        provider_id,
        newsgroup_id,
        key_kind,
        family_key
    )
    WHERE recover_pending = false;

CREATE TABLE IF NOT EXISTS public.release_recovered_file_set_candidates (
    provider_id bigint NOT NULL,
    file_set_key text NOT NULL,
    representative_newsgroup_id bigint NOT NULL DEFAULT 0,
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
    total_bytes bigint NOT NULL DEFAULT 0,
    earliest_posted_at timestamptz,
    expected_file_coverage_pct double precision NOT NULL DEFAULT 0,
    archive_file_coverage_pct double precision NOT NULL DEFAULT 0,
    readiness_bucket text NOT NULL DEFAULT 'fragment_only'::text,
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    CONSTRAINT release_recovered_file_set_candidates_pkey PRIMARY KEY (provider_id, file_set_key),
    CONSTRAINT release_recovered_file_set_candidates_provider_id_fkey
        FOREIGN KEY (provider_id) REFERENCES public.usenet_providers(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_release_recovered_file_set_candidates_queue
    ON public.release_recovered_file_set_candidates (
        updated_at,
        provider_id,
        representative_newsgroup_id,
        file_set_key
    );

CREATE TABLE IF NOT EXISTS public.release_recovered_file_set_candidate_acks (
    provider_id bigint NOT NULL,
    file_set_key text NOT NULL,
    processed_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    CONSTRAINT release_recovered_file_set_candidate_acks_pkey PRIMARY KEY (provider_id, file_set_key),
    CONSTRAINT release_recovered_file_set_candidate_acks_provider_id_fkey
        FOREIGN KEY (provider_id) REFERENCES public.usenet_providers(id) ON DELETE CASCADE
);
