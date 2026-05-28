CREATE TABLE IF NOT EXISTS public.release_family_summary_refresh_queue (
    provider_id BIGINT NOT NULL,
    newsgroup_id BIGINT NOT NULL,
    key_kind TEXT NOT NULL,
    family_key TEXT NOT NULL,
    queued_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (provider_id, newsgroup_id, key_kind, family_key)
);

CREATE INDEX IF NOT EXISTS idx_release_family_summary_refresh_queue_queued_at
    ON public.release_family_summary_refresh_queue (queued_at, provider_id, newsgroup_id, key_kind, family_key);
