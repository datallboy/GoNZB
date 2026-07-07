CREATE TABLE IF NOT EXISTS public.indexer_group_profiles (
    provider_id bigint NOT NULL REFERENCES public.usenet_providers(id) ON DELETE CASCADE,
    newsgroup_id bigint NOT NULL REFERENCES public.newsgroups(id) ON DELETE CASCADE,
    tier text DEFAULT 'warm'::text NOT NULL CHECK (tier IN ('hot', 'warm', 'cold', 'disabled')),
    tier_override text CHECK (tier_override IS NULL OR tier_override IN ('hot', 'warm', 'cold', 'disabled')),
    score double precision DEFAULT 0 NOT NULL,
    articles_scraped_1d bigint DEFAULT 0 NOT NULL,
    recovery_queued_1d bigint DEFAULT 0 NOT NULL,
    yenc_probes_attempted_1d bigint DEFAULT 0 NOT NULL,
    yenc_probes_successful_1d bigint DEFAULT 0 NOT NULL,
    binaries_completed_1d bigint DEFAULT 0 NOT NULL,
    releases_created_1d bigint DEFAULT 0 NOT NULL,
    avg_recovery_lag_seconds double precision DEFAULT 0 NOT NULL,
    max_recovery_lag_seconds double precision DEFAULT 0 NOT NULL,
    last_scored_at timestamptz,
    created_at timestamptz DEFAULT now() NOT NULL,
    updated_at timestamptz DEFAULT now() NOT NULL,
    PRIMARY KEY (provider_id, newsgroup_id)
);

CREATE INDEX IF NOT EXISTS idx_indexer_group_profiles_tier_score
    ON public.indexer_group_profiles (COALESCE(tier_override, tier), score DESC, updated_at DESC);

CREATE TABLE IF NOT EXISTS public.deferred_article_ranges (
    id bigserial PRIMARY KEY,
    provider_id bigint NOT NULL REFERENCES public.usenet_providers(id) ON DELETE CASCADE,
    newsgroup_id bigint NOT NULL REFERENCES public.newsgroups(id) ON DELETE CASCADE,
    article_low bigint NOT NULL,
    article_high bigint NOT NULL,
    posted_at_min timestamptz,
    posted_at_max timestamptz,
    observed_at timestamptz DEFAULT now() NOT NULL,
    estimated_article_count bigint DEFAULT 0 NOT NULL,
    estimated_obfuscated_count bigint DEFAULT 0 NOT NULL,
    reason text DEFAULT ''::text NOT NULL,
    priority_score double precision DEFAULT 0 NOT NULL,
    state text DEFAULT 'ready'::text NOT NULL CHECK (state IN ('ready', 'running', 'completed', 'abandoned')),
    attempts integer DEFAULT 0 NOT NULL,
    last_attempt_at timestamptz,
    created_at timestamptz DEFAULT now() NOT NULL,
    updated_at timestamptz DEFAULT now() NOT NULL,
    CHECK (article_low > 0),
    CHECK (article_high >= article_low),
    UNIQUE (provider_id, newsgroup_id, article_low, article_high)
);

CREATE INDEX IF NOT EXISTS idx_deferred_article_ranges_ready
    ON public.deferred_article_ranges (state, priority_score DESC, posted_at_max DESC NULLS LAST, updated_at)
    WHERE state = 'ready';

CREATE TABLE IF NOT EXISTS public.indexer_recovery_capacity_state (
    id boolean PRIMARY KEY DEFAULT true CHECK (id),
    probes_per_hour_ewma double precision DEFAULT 25000 NOT NULL,
    soft_cap bigint DEFAULT 100000 NOT NULL,
    hard_cap bigint DEFAULT 200000 NOT NULL,
    open_ready bigint DEFAULT 0 NOT NULL,
    open_running bigint DEFAULT 0 NOT NULL,
    oldest_ready_at timestamptz,
    newest_ready_at timestamptz,
    calculated_at timestamptz DEFAULT now() NOT NULL
);

INSERT INTO public.indexer_recovery_capacity_state (id)
VALUES (true)
ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS public.indexer_daily_bucket_stats (
    provider_id bigint NOT NULL REFERENCES public.usenet_providers(id) ON DELETE CASCADE,
    newsgroup_id bigint NOT NULL REFERENCES public.newsgroups(id) ON DELETE CASCADE,
    bucket_day date NOT NULL,
    tier text DEFAULT 'warm'::text NOT NULL,
    scrape_progress_known boolean DEFAULT false NOT NULL,
    lower_boundary_crossed boolean DEFAULT false NOT NULL,
    upper_boundary_crossed boolean DEFAULT false NOT NULL,
    bucket_article_low bigint DEFAULT 0 NOT NULL,
    bucket_article_high bigint DEFAULT 0 NOT NULL,
    scrape_cursor_low bigint DEFAULT 0 NOT NULL,
    scrape_cursor_high bigint DEFAULT 0 NOT NULL,
    headers_staged bigint DEFAULT 0 NOT NULL,
    unassembled_headers bigint DEFAULT 0 NOT NULL,
    yenc_ready bigint DEFAULT 0 NOT NULL,
    yenc_running bigint DEFAULT 0 NOT NULL,
    yenc_done bigint DEFAULT 0 NOT NULL,
    yenc_stale bigint DEFAULT 0 NOT NULL,
    binaries_total bigint DEFAULT 0 NOT NULL,
    binaries_complete bigint DEFAULT 0 NOT NULL,
    binaries_weak bigint DEFAULT 0 NOT NULL,
    releases_created bigint DEFAULT 0 NOT NULL,
    archive_pending bigint DEFAULT 0 NOT NULL,
    purge_pending bigint DEFAULT 0 NOT NULL,
    blocker_count bigint DEFAULT 0 NOT NULL,
    last_refreshed_at timestamptz DEFAULT now() NOT NULL,
    PRIMARY KEY (provider_id, newsgroup_id, bucket_day)
);

CREATE INDEX IF NOT EXISTS idx_indexer_daily_bucket_stats_day_tier
    ON public.indexer_daily_bucket_stats (bucket_day DESC, tier, provider_id, newsgroup_id);

CREATE TABLE IF NOT EXISTS public.indexer_partition_maintenance_runs (
    id bigserial PRIMARY KEY,
    task_name text DEFAULT 'partition_retention'::text NOT NULL,
    dry_run boolean DEFAULT true NOT NULL,
    started_at timestamptz DEFAULT now() NOT NULL,
    finished_at timestamptz,
    status text DEFAULT 'running'::text NOT NULL,
    summary_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    error_text text DEFAULT ''::text NOT NULL
);

ALTER TABLE IF EXISTS public.yenc_recovery_work_items
    ADD COLUMN IF NOT EXISTS group_tier text DEFAULT 'warm'::text NOT NULL,
    ADD COLUMN IF NOT EXISTS admission_reason text DEFAULT ''::text NOT NULL,
    ADD COLUMN IF NOT EXISTS admission_score double precision DEFAULT 0 NOT NULL,
    ADD COLUMN IF NOT EXISTS deferred_range_id bigint REFERENCES public.deferred_article_ranges(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS source_posted_at timestamptz,
    ADD COLUMN IF NOT EXISTS partition_day date;

UPDATE public.yenc_recovery_work_items
SET source_posted_at = COALESCE(source_posted_at, date_utc, created_at),
    partition_day = COALESCE(partition_day, COALESCE(date_utc, created_at)::date)
WHERE source_posted_at IS NULL OR partition_day IS NULL;

CREATE INDEX IF NOT EXISTS idx_yenc_recovery_work_items_admission_pressure
    ON public.yenc_recovery_work_items (status, group_tier, priority_rank, source_posted_at DESC NULLS LAST, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_yenc_recovery_work_items_partition_day
    ON public.yenc_recovery_work_items (partition_day, status, provider_id, newsgroup_id);

CREATE INDEX IF NOT EXISTS idx_article_headers_provider_group_date_article
    ON public.article_headers (provider_id, newsgroup_id, date_utc, article_number);
