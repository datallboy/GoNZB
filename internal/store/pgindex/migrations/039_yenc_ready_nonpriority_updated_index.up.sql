CREATE INDEX IF NOT EXISTS idx_yenc_recovery_ready_nonpriority_updated
    ON public.yenc_recovery_work_items (
        updated_at DESC,
        binary_id,
        source_posted_at
    )
    WHERE status = 'ready'
      AND priority_rank > 0;
