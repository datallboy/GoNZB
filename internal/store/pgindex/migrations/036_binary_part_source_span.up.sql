ALTER TABLE IF EXISTS public.binary_observation_stats
    ADD COLUMN IF NOT EXISTS part_source_posted_at_min timestamp with time zone,
    ADD COLUMN IF NOT EXISTS part_source_posted_at_max timestamp with time zone;

CREATE INDEX IF NOT EXISTS idx_binary_parts_binary_source_part
    ON public.binary_parts (binary_id, source_posted_at, part_number);
