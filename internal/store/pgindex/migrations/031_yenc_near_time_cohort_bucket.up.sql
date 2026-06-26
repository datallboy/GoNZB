ALTER TABLE IF EXISTS public.indexer_recovery_capacity_state
    ADD COLUMN IF NOT EXISTS near_time_cohort_bucket_minutes integer DEFAULT 5 NOT NULL;

UPDATE public.indexer_recovery_capacity_state
SET near_time_cohort_bucket_minutes = 5
WHERE near_time_cohort_bucket_minutes <= 0;
