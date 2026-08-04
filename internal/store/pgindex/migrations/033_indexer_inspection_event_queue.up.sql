CREATE TABLE public.indexer_inspection_reconcile_state (
    stage_name text PRIMARY KEY,
    cursor_updated_at timestamp with time zone NOT NULL DEFAULT 'epoch',
    cursor_release_id text NOT NULL DEFAULT '',
    reconciled_count bigint NOT NULL DEFAULT 0,
    updated_at timestamp with time zone NOT NULL DEFAULT now(),
    CONSTRAINT indexer_inspection_reconcile_state_stage_check
        CHECK (stage_name IN ('inspect_discovery', 'inspect_par2', 'inspect_archive', 'inspect_media'))
);

CREATE INDEX idx_releases_inspection_reconcile
    ON public.releases (updated_at, release_id);

CREATE INDEX idx_binary_inspection_ready_queue_claim
    ON public.binary_inspection_ready_queue
        (stage_name, status, ready_at, source_updated_at DESC, binary_id DESC);

CREATE INDEX idx_binary_inspections_stage_binary_source
    ON public.binary_inspections (stage_name, binary_id, source_posted_at);
