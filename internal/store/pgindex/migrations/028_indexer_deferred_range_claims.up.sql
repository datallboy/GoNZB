ALTER TABLE public.deferred_article_ranges
    ADD COLUMN last_error text DEFAULT ''::text NOT NULL;
