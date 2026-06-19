CREATE INDEX IF NOT EXISTS idx_yenc_recovery_work_items_ready_posted_order
    ON public.yenc_recovery_work_items (priority_rank, date_utc DESC, updated_at DESC, binary_id)
    WHERE status = 'ready';
