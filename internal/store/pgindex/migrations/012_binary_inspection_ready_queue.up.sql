CREATE TABLE IF NOT EXISTS public.binary_inspection_ready_queue (
    stage_name text NOT NULL,
    binary_id bigint NOT NULL,
    release_id text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT 'ready',
    ready_at timestamp with time zone NOT NULL DEFAULT now(),
    source_updated_at timestamp with time zone,
    claimed_by text NOT NULL DEFAULT '',
    claimed_until timestamp with time zone,
    last_error text NOT NULL DEFAULT '',
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    updated_at timestamp with time zone NOT NULL DEFAULT now(),
    CONSTRAINT binary_inspection_ready_queue_pkey PRIMARY KEY (stage_name, binary_id),
    CONSTRAINT binary_inspection_ready_queue_binary_id_fkey FOREIGN KEY (binary_id) REFERENCES public.binary_core(binary_id) ON DELETE CASCADE,
    CONSTRAINT binary_inspection_ready_queue_status_check CHECK (status IN ('ready', 'running', 'completed', 'blocked'))
);

CREATE INDEX IF NOT EXISTS idx_binary_inspection_ready_queue_ready
    ON public.binary_inspection_ready_queue (stage_name, status, ready_at, source_updated_at DESC, binary_id)
    WHERE status = 'ready';

CREATE INDEX IF NOT EXISTS idx_binary_inspection_ready_queue_running
    ON public.binary_inspection_ready_queue (stage_name, status, claimed_until, binary_id)
    WHERE status = 'running';

DELETE FROM public.indexer_dashboard_stats
WHERE stat_key = 'pending_inspect_discovery_binaries';
