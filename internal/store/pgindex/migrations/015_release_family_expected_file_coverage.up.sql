ALTER TABLE public.release_family_readiness_summaries
    ADD COLUMN IF NOT EXISTS expected_file_count integer DEFAULT 0 NOT NULL;

ALTER TABLE public.release_family_readiness_summaries
    ADD COLUMN IF NOT EXISTS complete_main_payload_binary_count integer DEFAULT 0 NOT NULL;

ALTER TABLE public.release_family_readiness_summaries
    ADD COLUMN IF NOT EXISTS expected_file_coverage_pct double precision DEFAULT 0 NOT NULL;

-- Keep startup migrations lightweight on large databases.
-- Existing summary rows retain safe defaults and will be refreshed incrementally
-- by the normal release-family summary maintenance path as binaries are updated.
