CREATE INDEX IF NOT EXISTS idx_article_headers_id_source_posted
    ON public.article_headers (id, source_posted_at);
