ALTER TABLE public.yenc_recovery_work_items
    ADD COLUMN IF NOT EXISTS newsgroup_name TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS article_number BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS subject TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS poster TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS date_utc TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS article_bytes BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS article_lines INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS xref TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS subject_file_name TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS subject_file_index INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS subject_file_total INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS yenc_part_number INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS yenc_total_parts INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS yenc_file_size BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS lease_owner TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS lease_expires_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_yenc_recovery_work_items_ready_order
    ON public.yenc_recovery_work_items (priority_rank, updated_at DESC, binary_id)
    WHERE status = 'ready';

CREATE INDEX IF NOT EXISTS idx_yenc_recovery_work_items_expired_running
    ON public.yenc_recovery_work_items (lease_expires_at, priority_rank, updated_at DESC, binary_id)
    WHERE status = 'running';
