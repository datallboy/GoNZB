CREATE TABLE public.indexer_source_bucket_state (
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    source_day date NOT NULL,
    state text DEFAULT 'active'::text NOT NULL,
    first_ingested_at timestamp with time zone NOT NULL,
    last_ingested_at timestamp with time zone NOT NULL,
    last_progress_at timestamp with time zone NOT NULL,
    settled_at timestamp with time zone,
    headers_ingested bigint DEFAULT 0 NOT NULL,
    open_work_count bigint DEFAULT 0 NOT NULL,
    exhausted_work_count bigint DEFAULT 0 NOT NULL,
    terminal_release_count bigint DEFAULT 0 NOT NULL,
    terminal_reason text DEFAULT ''::text NOT NULL,
    purge_eligible_at timestamp with time zone,
    purged_at timestamp with time zone,
    last_reconciled_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT indexer_source_bucket_state_pkey PRIMARY KEY (provider_id, newsgroup_id, source_day),
    CONSTRAINT indexer_source_bucket_state_state_check CHECK (
        state = ANY (ARRAY['active'::text, 'success'::text, 'no_yield'::text, 'purge_eligible'::text, 'purged'::text])
    ),
    CONSTRAINT indexer_source_bucket_state_provider_id_fkey FOREIGN KEY (provider_id)
        REFERENCES public.usenet_providers(id) ON DELETE CASCADE,
    CONSTRAINT indexer_source_bucket_state_newsgroup_id_fkey FOREIGN KEY (newsgroup_id)
        REFERENCES public.newsgroups(id) ON DELETE CASCADE
);

CREATE INDEX idx_indexer_source_bucket_state_retention
    ON public.indexer_source_bucket_state (state, purge_eligible_at, source_day, provider_id, newsgroup_id)
    WHERE state IN ('success', 'no_yield', 'purge_eligible');

CREATE INDEX idx_indexer_source_bucket_state_progress
    ON public.indexer_source_bucket_state (last_progress_at, last_ingested_at, source_day)
    WHERE state = 'active';

ALTER TABLE public.indexer_recovery_capacity_state
    ADD COLUMN latest_reserve_percent integer DEFAULT 10 NOT NULL,
    ADD CONSTRAINT indexer_recovery_capacity_state_latest_reserve_percent_check
        CHECK (latest_reserve_percent >= 0 AND latest_reserve_percent <= 50);

ALTER TABLE public.deferred_article_ranges
    ADD COLUMN claim_owner text DEFAULT ''::text NOT NULL,
    ADD COLUMN claim_until timestamp with time zone;

CREATE INDEX idx_deferred_article_ranges_claimable
    ON public.deferred_article_ranges (priority_score DESC, posted_at_max DESC NULLS LAST, updated_at, id)
    WHERE state = 'ready';
