ALTER TABLE public.release_family_readiness_summaries
    ADD COLUMN IF NOT EXISTS dominant_family_kind text NOT NULL DEFAULT ''::text;

ALTER TABLE public.release_family_readiness_summaries
    ADD COLUMN IF NOT EXISTS dominant_file_name text NOT NULL DEFAULT ''::text;

ALTER TABLE public.release_family_readiness_summaries
    ADD COLUMN IF NOT EXISTS dominant_match_confidence double precision NOT NULL DEFAULT 0;
