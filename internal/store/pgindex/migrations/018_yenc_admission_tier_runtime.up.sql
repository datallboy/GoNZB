ALTER TABLE IF EXISTS public.indexer_recovery_capacity_state
    ADD COLUMN IF NOT EXISTS soft_queue_hours integer DEFAULT 4 NOT NULL,
    ADD COLUMN IF NOT EXISTS hard_queue_multiplier integer DEFAULT 2 NOT NULL,
    ADD COLUMN IF NOT EXISTS absolute_hard_queue_cap bigint DEFAULT 250000 NOT NULL,
    ADD COLUMN IF NOT EXISTS bootstrap_probes_per_hour double precision DEFAULT 25000 NOT NULL,
    ADD COLUMN IF NOT EXISTS ewma_window_minutes integer DEFAULT 30 NOT NULL,
    ADD COLUMN IF NOT EXISTS priority0_overflow_cap bigint DEFAULT 25000 NOT NULL,
    ADD COLUMN IF NOT EXISTS config_updated_at timestamptz DEFAULT now() NOT NULL;

CREATE INDEX IF NOT EXISTS idx_indexer_group_profiles_yield
    ON public.indexer_group_profiles (releases_created_1d DESC, recovery_queued_1d DESC, score DESC);
