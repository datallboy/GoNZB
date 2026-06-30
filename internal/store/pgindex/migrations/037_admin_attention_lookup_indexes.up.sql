CREATE INDEX IF NOT EXISTS idx_binary_inspections_release_status_lookup
    ON public.binary_inspections (release_id, status, updated_at DESC, source_posted_at)
    WHERE release_id IS NOT NULL;
