DROP TABLE IF EXISTS public.article_header_crosspost_group_messages;
DROP TABLE IF EXISTS public.article_header_crosspost_group_sources;

DELETE FROM public.indexer_table_write_ownership
WHERE table_name IN (
    'article_header_crosspost_group_messages',
    'article_header_crosspost_group_sources'
);

INSERT INTO public.indexer_table_write_ownership (table_name, owner_stage, allowed_writer_stages, notes)
VALUES (
    'article_header_crosspost_group_summary',
    'crosspost_popularity_refresh',
    ARRAY['crosspost_popularity_refresh'],
    'Crosspost popularity summary derived directly from raw article_header_crosspost_groups observations; exact distinct helper tables were removed to avoid telemetry write amplification.'
)
ON CONFLICT (table_name) DO UPDATE
SET owner_stage = EXCLUDED.owner_stage,
    allowed_writer_stages = EXCLUDED.allowed_writer_stages,
    notes = EXCLUDED.notes,
    updated_at = now();
