ALTER TABLE public.binaries
    ADD COLUMN IF NOT EXISTS expected_archive_file_count integer DEFAULT 0 NOT NULL;

ALTER TABLE public.releases
    ADD COLUMN IF NOT EXISTS expected_archive_file_count integer DEFAULT 0 NOT NULL;

ALTER TABLE public.release_family_readiness_summaries
    ADD COLUMN IF NOT EXISTS expected_archive_file_count integer DEFAULT 0 NOT NULL;

ALTER TABLE public.release_family_readiness_summaries
    ADD COLUMN IF NOT EXISTS has_expected_archive_file_count boolean DEFAULT false NOT NULL;

ALTER TABLE public.release_family_readiness_summaries
    ADD COLUMN IF NOT EXISTS archive_file_coverage_pct double precision DEFAULT 0 NOT NULL;

CREATE INDEX IF NOT EXISTS idx_binaries_base_stem_archive_expected_family
    ON public.binaries(provider_id, newsgroup_id, expected_archive_file_count, lower(btrim(base_stem)))
    WHERE btrim(base_stem) <> '';

-- Existing summary rows retain safe defaults and will refresh incrementally.
