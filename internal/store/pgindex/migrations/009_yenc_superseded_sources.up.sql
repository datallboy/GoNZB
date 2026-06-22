CREATE TABLE IF NOT EXISTS public.binary_superseded_sources (
    source_binary_id bigint PRIMARY KEY,
    target_binary_id bigint NOT NULL,
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    release_family_key text NOT NULL DEFAULT '',
    source_binary_key text NOT NULL DEFAULT '',
    target_binary_key text NOT NULL DEFAULT '',
    superseded_reason text NOT NULL DEFAULT 'yenc_recovery_merge',
    superseded_at timestamp with time zone NOT NULL DEFAULT now(),
    purged_at timestamp with time zone,
    CONSTRAINT binary_superseded_sources_source_fkey
        FOREIGN KEY (source_binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE,
    CONSTRAINT binary_superseded_sources_target_fkey
        FOREIGN KEY (target_binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_binary_superseded_sources_target
    ON public.binary_superseded_sources (target_binary_id);

CREATE INDEX IF NOT EXISTS idx_binary_superseded_sources_release_family
    ON public.binary_superseded_sources (provider_id, newsgroup_id, release_family_key);
