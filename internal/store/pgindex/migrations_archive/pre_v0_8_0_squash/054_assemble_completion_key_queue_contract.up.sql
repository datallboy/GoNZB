INSERT INTO public.indexer_table_write_ownership (table_name, owner_stage, allowed_writer_stages, notes)
VALUES
    ('article_header_assembly_keys', 'assemble', ARRAY['scrape_latest', 'scrape_backfill', 'assemble_lane_a', 'assemble_lane_b'], 'Assembly work surface seeded inline by scrape and completed by assemble after article headers are claimed into binary parts.'),
    ('binary_completion_keys', 'assemble', ARRAY['assemble_lane_a', 'assemble_lane_b', 'recover_yenc'], 'Binary-derived incomplete-file selector projection for assemble lane A. Recovery may refresh it after yEnc identity promotion.')
ON CONFLICT (table_name) DO UPDATE
SET owner_stage = EXCLUDED.owner_stage,
    allowed_writer_stages = EXCLUDED.allowed_writer_stages,
    notes = EXCLUDED.notes,
    updated_at = now();

DROP INDEX IF EXISTS public.idx_binary_completion_keys_rank;
CREATE INDEX IF NOT EXISTS idx_binary_completion_keys_rank
    ON public.binary_completion_keys (
        is_main_payload DESC,
        completion_ratio DESC,
        observed_parts DESC,
        binary_id DESC
    );

DELETE FROM public.article_header_assembly_keys hk
USING public.article_headers ah
WHERE ah.id = hk.article_header_id
  AND ah.assembled_at IS NOT NULL;
