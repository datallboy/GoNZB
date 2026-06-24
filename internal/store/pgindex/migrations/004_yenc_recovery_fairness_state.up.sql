CREATE TABLE IF NOT EXISTS public.yenc_recovery_fairness_state (
    stage_name text PRIMARY KEY,
    cursor_before timestamp with time zone,
    bucket_start timestamp with time zone,
    bucket_end timestamp with time zone,
    quota_percent integer NOT NULL DEFAULT 25,
    repeat_full_count integer NOT NULL DEFAULT 0,
    wrapped_count bigint NOT NULL DEFAULT 0,
    updated_at timestamp with time zone NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_yenc_recovery_work_items_ready_date_range
ON public.yenc_recovery_work_items (date_utc DESC NULLS LAST, priority_rank, newsgroup_id, article_number, binary_id)
WHERE status = 'ready' AND date_utc IS NOT NULL;
