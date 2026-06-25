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
