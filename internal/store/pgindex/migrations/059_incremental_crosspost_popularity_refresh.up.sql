ALTER TABLE public.article_header_crosspost_group_summary
    ADD COLUMN IF NOT EXISTS last_refreshed_article_header_id bigint NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_article_header_crosspost_groups_group_article
    ON public.article_header_crosspost_groups (
        observed_group_name,
        article_header_id
    )
    INCLUDE (
        message_id,
        source_newsgroup_id,
        observed_at
    )
    WHERE btrim(observed_group_name) <> '';

WITH max_seen AS (
    SELECT
        observed_group_name,
        MAX(article_header_id)::bigint AS last_refreshed_article_header_id
    FROM public.article_header_crosspost_groups
    WHERE btrim(coalesce(observed_group_name, '')) <> ''
    GROUP BY observed_group_name
)
UPDATE public.article_header_crosspost_group_summary s
SET last_refreshed_article_header_id = GREATEST(
        s.last_refreshed_article_header_id,
        m.last_refreshed_article_header_id
    )
FROM max_seen m
WHERE m.observed_group_name = s.observed_group_name;

DROP INDEX IF EXISTS public.idx_article_header_crosspost_group_summary_rank;

CREATE INDEX IF NOT EXISTS idx_article_header_crosspost_group_summary_rank
ON public.article_header_crosspost_group_summary (
    observed_article_count DESC,
    distinct_message_count DESC,
    last_seen_at DESC,
    observed_group_name
);

INSERT INTO public.indexer_table_write_ownership (table_name, owner_stage, allowed_writer_stages, notes)
VALUES
    (
        'article_header_crosspost_group_summary',
        'crosspost_popularity_refresh',
        ARRAY['crosspost_popularity_refresh'],
        'Incremental popularity rollup derived from raw article_header_crosspost_groups observations. The hot path advances last_refreshed_article_header_id instead of recomputing exact distinct counts from all raw rows.'
    )
ON CONFLICT (table_name) DO UPDATE
SET owner_stage = EXCLUDED.owner_stage,
    allowed_writer_stages = EXCLUDED.allowed_writer_stages,
    notes = EXCLUDED.notes,
    updated_at = now();
