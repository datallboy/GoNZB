DROP INDEX IF EXISTS public.idx_yenc_recovery_work_items_ready_posted_order;

CREATE INDEX IF NOT EXISTS idx_yenc_recovery_work_items_ready_posted_nulls_last
    ON public.yenc_recovery_work_items (priority_rank, date_utc DESC NULLS LAST, updated_at DESC, binary_id)
    WHERE status = 'ready';
