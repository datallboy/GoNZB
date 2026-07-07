CREATE INDEX IF NOT EXISTS idx_article_header_crosspost_groups_refresh_lookup
    ON public.article_header_crosspost_groups (
        observed_group_name,
        article_header_id,
        source_posted_at
    )
    WHERE BTRIM(observed_group_name) <> '';
