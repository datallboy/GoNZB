CREATE TABLE public.indexer_scrape_timeframe_progress (
    timeframe_id text NOT NULL,
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    window_start timestamp with time zone NOT NULL,
    window_end timestamp with time zone NOT NULL,
    article_low bigint DEFAULT 0 NOT NULL,
    article_high bigint DEFAULT 0 NOT NULL,
    next_article bigint DEFAULT 0 NOT NULL,
    state text DEFAULT 'pending'::text NOT NULL,
    resolved_at timestamp with time zone,
    completed_at timestamp with time zone,
    last_attempt_at timestamp with time zone,
    last_error text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT indexer_scrape_timeframe_progress_pkey PRIMARY KEY (timeframe_id, provider_id, newsgroup_id),
    CONSTRAINT indexer_scrape_timeframe_progress_window_check CHECK (window_end > window_start),
    CONSTRAINT indexer_scrape_timeframe_progress_range_check CHECK (
        (article_low = 0 AND article_high = 0) OR (article_low > 0 AND article_high >= article_low)
    ),
    CONSTRAINT indexer_scrape_timeframe_progress_state_check CHECK (
        state = ANY (ARRAY['pending'::text, 'active'::text, 'completed'::text, 'empty'::text, 'failed'::text])
    ),
    CONSTRAINT indexer_scrape_timeframe_progress_provider_id_fkey FOREIGN KEY (provider_id)
        REFERENCES public.usenet_providers(id) ON DELETE CASCADE,
    CONSTRAINT indexer_scrape_timeframe_progress_newsgroup_id_fkey FOREIGN KEY (newsgroup_id)
        REFERENCES public.newsgroups(id) ON DELETE CASCADE
);

CREATE INDEX idx_indexer_scrape_timeframe_progress_active
    ON public.indexer_scrape_timeframe_progress (state, updated_at, timeframe_id)
    WHERE state IN ('pending', 'active', 'failed');
