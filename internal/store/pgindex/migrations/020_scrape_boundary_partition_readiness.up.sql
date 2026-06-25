ALTER TABLE IF EXISTS public.article_headers
    ADD COLUMN IF NOT EXISTS source_posted_at timestamptz;

ALTER TABLE IF EXISTS public.article_header_ingest_payloads
    ADD COLUMN IF NOT EXISTS source_posted_at timestamptz;

ALTER TABLE IF EXISTS public.article_header_crosspost_groups
    ADD COLUMN IF NOT EXISTS source_posted_at timestamptz;

ALTER TABLE IF EXISTS public.article_header_poster_refs
    ADD COLUMN IF NOT EXISTS source_posted_at timestamptz;

ALTER TABLE IF EXISTS public.article_header_assembly_queue
    ADD COLUMN IF NOT EXISTS source_posted_at timestamptz;

ALTER TABLE IF EXISTS public.poster_materialization_queue
    ADD COLUMN IF NOT EXISTS source_posted_at timestamptz;

ALTER TABLE IF EXISTS public.binary_core
    ADD COLUMN IF NOT EXISTS source_posted_at timestamptz;

ALTER TABLE IF EXISTS public.binary_parts
    ADD COLUMN IF NOT EXISTS source_posted_at timestamptz;

ALTER TABLE IF EXISTS public.binary_observation_stats
    ADD COLUMN IF NOT EXISTS source_posted_at timestamptz;

ALTER TABLE IF EXISTS public.binary_identity_current
    ADD COLUMN IF NOT EXISTS source_posted_at timestamptz;

ALTER TABLE IF EXISTS public.binary_recovery_current
    ADD COLUMN IF NOT EXISTS source_posted_at timestamptz;

ALTER TABLE IF EXISTS public.binary_inspection_ready_queue
    ADD COLUMN IF NOT EXISTS source_posted_at timestamptz;

ALTER TABLE IF EXISTS public.release_family_readiness_summaries
    ADD COLUMN IF NOT EXISTS source_posted_at timestamptz;

ALTER TABLE IF EXISTS public.release_ready_candidates
    ADD COLUMN IF NOT EXISTS source_posted_at timestamptz;

ALTER TABLE IF EXISTS public.release_recovered_file_set_candidates
    ADD COLUMN IF NOT EXISTS source_posted_at timestamptz;

ALTER TABLE IF EXISTS public.indexer_daily_bucket_stats
    ADD COLUMN IF NOT EXISTS scrape_progress_pct double precision DEFAULT 0 NOT NULL;

UPDATE public.article_headers
SET source_posted_at = COALESCE(source_posted_at, date_utc)
WHERE source_posted_at IS NULL
  AND date_utc IS NOT NULL;

UPDATE public.article_header_ingest_payloads p
SET source_posted_at = COALESCE(p.source_posted_at, ah.source_posted_at, ah.date_utc)
FROM public.article_headers ah
WHERE p.article_header_id = ah.id
  AND p.source_posted_at IS NULL;

UPDATE public.article_header_crosspost_groups cg
SET source_posted_at = COALESCE(cg.source_posted_at, ah.source_posted_at, ah.date_utc)
FROM public.article_headers ah
WHERE cg.article_header_id = ah.id
  AND cg.source_posted_at IS NULL;

UPDATE public.article_header_poster_refs pr
SET source_posted_at = COALESCE(pr.source_posted_at, ah.source_posted_at, ah.date_utc)
FROM public.article_headers ah
WHERE pr.article_header_id = ah.id
  AND pr.source_posted_at IS NULL;

UPDATE public.article_header_assembly_queue q
SET source_posted_at = COALESCE(q.source_posted_at, ah.source_posted_at, ah.date_utc)
FROM public.article_headers ah
WHERE q.article_header_id = ah.id
  AND q.source_posted_at IS NULL;

UPDATE public.poster_materialization_queue q
SET source_posted_at = COALESCE(q.source_posted_at, ah.source_posted_at, ah.date_utc)
FROM public.article_headers ah
WHERE q.article_header_id = ah.id
  AND q.source_posted_at IS NULL;

UPDATE public.binary_observation_stats
SET source_posted_at = COALESCE(source_posted_at, posted_at)
WHERE source_posted_at IS NULL
  AND posted_at IS NOT NULL;

UPDATE public.binary_core bc
SET source_posted_at = COALESCE(bc.source_posted_at, bos.source_posted_at, bos.posted_at)
FROM public.binary_observation_stats bos
WHERE bos.binary_id = bc.binary_id
  AND bc.source_posted_at IS NULL;

UPDATE public.binary_parts bp
SET source_posted_at = COALESCE(bp.source_posted_at, ah.source_posted_at, ah.date_utc)
FROM public.article_headers ah
WHERE bp.article_header_id = ah.id
  AND bp.source_posted_at IS NULL;

UPDATE public.binary_identity_current bic
SET source_posted_at = COALESCE(bic.source_posted_at, bc.source_posted_at, bos.source_posted_at, bos.posted_at)
FROM public.binary_core bc
LEFT JOIN public.binary_observation_stats bos ON bos.binary_id = bc.binary_id
WHERE bic.binary_id = bc.binary_id
  AND bic.source_posted_at IS NULL;

UPDATE public.binary_recovery_current brc
SET source_posted_at = COALESCE(brc.source_posted_at, bc.source_posted_at, bos.source_posted_at, bos.posted_at)
FROM public.binary_core bc
LEFT JOIN public.binary_observation_stats bos ON bos.binary_id = bc.binary_id
WHERE brc.binary_id = bc.binary_id
  AND brc.source_posted_at IS NULL;

UPDATE public.binary_inspection_ready_queue irq
SET source_posted_at = COALESCE(irq.source_posted_at, bc.source_posted_at, bos.source_posted_at, bos.posted_at)
FROM public.binary_core bc
LEFT JOIN public.binary_observation_stats bos ON bos.binary_id = bc.binary_id
WHERE irq.binary_id = bc.binary_id
  AND irq.source_posted_at IS NULL;

UPDATE public.release_family_readiness_summaries
SET source_posted_at = COALESCE(source_posted_at, earliest_posted_at)
WHERE source_posted_at IS NULL
  AND earliest_posted_at IS NOT NULL;

UPDATE public.release_ready_candidates
SET source_posted_at = COALESCE(source_posted_at, earliest_posted_at)
WHERE source_posted_at IS NULL
  AND earliest_posted_at IS NOT NULL;

UPDATE public.release_recovered_file_set_candidates
SET source_posted_at = COALESCE(source_posted_at, earliest_posted_at)
WHERE source_posted_at IS NULL
  AND earliest_posted_at IS NOT NULL;

CREATE TABLE IF NOT EXISTS public.indexer_scrape_day_boundaries (
    provider_id bigint NOT NULL REFERENCES public.usenet_providers(id) ON DELETE CASCADE,
    newsgroup_id bigint NOT NULL REFERENCES public.newsgroups(id) ON DELETE CASCADE,
    bucket_day date NOT NULL,
    lower_boundary_crossed boolean DEFAULT false NOT NULL,
    upper_boundary_crossed boolean DEFAULT false NOT NULL,
    bucket_article_low bigint DEFAULT 0 NOT NULL,
    bucket_article_high bigint DEFAULT 0 NOT NULL,
    observed_article_count bigint DEFAULT 0 NOT NULL,
    first_observed_at timestamptz DEFAULT now() NOT NULL,
    last_observed_at timestamptz DEFAULT now() NOT NULL,
    PRIMARY KEY (provider_id, newsgroup_id, bucket_day)
);

CREATE INDEX IF NOT EXISTS idx_indexer_scrape_day_boundaries_day
    ON public.indexer_scrape_day_boundaries (bucket_day DESC, provider_id, newsgroup_id);

CREATE INDEX IF NOT EXISTS idx_article_headers_source_posted_group_article
    ON public.article_headers (source_posted_at, provider_id, newsgroup_id, article_number)
    WHERE source_posted_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_article_header_assembly_queue_source_posted
    ON public.article_header_assembly_queue (source_posted_at, claim_until, article_header_id)
    WHERE source_posted_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_binary_core_source_posted
    ON public.binary_core (source_posted_at, binary_id)
    WHERE source_posted_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_binary_parts_source_posted
    ON public.binary_parts (source_posted_at, binary_id, article_header_id)
    WHERE source_posted_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_binary_observation_stats_source_posted
    ON public.binary_observation_stats (source_posted_at, provider_id, newsgroup_id, binary_id)
    WHERE source_posted_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_binary_identity_current_source_posted
    ON public.binary_identity_current (source_posted_at, provider_id, newsgroup_id, binary_id)
    WHERE source_posted_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_binary_recovery_current_source_posted
    ON public.binary_recovery_current (source_posted_at, provider_id, newsgroup_id, binary_id)
    WHERE source_posted_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_binary_inspection_ready_queue_source_posted
    ON public.binary_inspection_ready_queue (source_posted_at, stage_name, status, binary_id)
    WHERE source_posted_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_release_family_readiness_source_posted
    ON public.release_family_readiness_summaries (source_posted_at, provider_id, newsgroup_id, key_kind, family_key)
    WHERE source_posted_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_release_ready_candidates_source_posted
    ON public.release_ready_candidates (source_posted_at, provider_id, newsgroup_id, key_kind, family_key)
    WHERE source_posted_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_release_recovered_file_set_candidates_source_posted
    ON public.release_recovered_file_set_candidates (source_posted_at, provider_id, file_set_key)
    WHERE source_posted_at IS NOT NULL;

CREATE OR REPLACE FUNCTION public.pgindex_set_source_posted_from_earliest()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    NEW.source_posted_at := COALESCE(NEW.source_posted_at, NEW.earliest_posted_at);
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_release_family_readiness_source_posted ON public.release_family_readiness_summaries;
CREATE TRIGGER trg_release_family_readiness_source_posted
BEFORE INSERT OR UPDATE ON public.release_family_readiness_summaries
FOR EACH ROW
EXECUTE FUNCTION public.pgindex_set_source_posted_from_earliest();

DROP TRIGGER IF EXISTS trg_release_ready_candidates_source_posted ON public.release_ready_candidates;
CREATE TRIGGER trg_release_ready_candidates_source_posted
BEFORE INSERT OR UPDATE ON public.release_ready_candidates
FOR EACH ROW
EXECUTE FUNCTION public.pgindex_set_source_posted_from_earliest();

DROP TRIGGER IF EXISTS trg_release_recovered_file_set_candidates_source_posted ON public.release_recovered_file_set_candidates;
CREATE TRIGGER trg_release_recovered_file_set_candidates_source_posted
BEFORE INSERT OR UPDATE ON public.release_recovered_file_set_candidates
FOR EACH ROW
EXECUTE FUNCTION public.pgindex_set_source_posted_from_earliest();
