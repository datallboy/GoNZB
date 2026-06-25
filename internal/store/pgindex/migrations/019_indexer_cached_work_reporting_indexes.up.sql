CREATE INDEX IF NOT EXISTS idx_binary_observation_posted_group
    ON public.binary_observation_stats (posted_at, provider_id, newsgroup_id, binary_id);

CREATE INDEX IF NOT EXISTS idx_article_headers_date_provider_group
    ON public.article_headers (date_utc, provider_id, newsgroup_id, article_number);
