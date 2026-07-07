CREATE TABLE IF NOT EXISTS public.yenc_recovery_work_items (
    binary_id BIGINT PRIMARY KEY REFERENCES public.binaries(id) ON DELETE CASCADE,
    article_header_id BIGINT NOT NULL UNIQUE REFERENCES public.article_headers(id) ON DELETE CASCADE,
    provider_id BIGINT NOT NULL,
    newsgroup_id BIGINT NOT NULL,
    message_id TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'ready',
    ready_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    priority_rank INTEGER NOT NULL DEFAULT 0,
    missing_count INTEGER NOT NULL DEFAULT 0,
    current_binary_key TEXT NOT NULL DEFAULT '',
    current_release_family_key TEXT NOT NULL DEFAULT '',
    current_base_stem TEXT NOT NULL DEFAULT '',
    current_readiness_bucket TEXT NOT NULL DEFAULT '',
    structured_identity_binary_matched BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_yenc_recovery_work_items_ready
    ON public.yenc_recovery_work_items (status, ready_at, priority_rank, updated_at DESC, binary_id)
    WHERE status = 'ready';

CREATE INDEX IF NOT EXISTS idx_yenc_recovery_work_items_article_header_id
    ON public.yenc_recovery_work_items (article_header_id);
