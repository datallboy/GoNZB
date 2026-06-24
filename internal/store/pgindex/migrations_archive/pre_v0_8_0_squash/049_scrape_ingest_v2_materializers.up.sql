CREATE TABLE IF NOT EXISTS public.article_header_poster_refs (
    article_header_id bigint PRIMARY KEY REFERENCES public.article_headers(id) ON DELETE CASCADE,
    poster_id bigint NOT NULL REFERENCES public.posters(id) ON DELETE CASCADE,
    poster_name text DEFAULT ''::text NOT NULL,
    poster_key text DEFAULT ''::text NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    updated_at timestamptz DEFAULT now() NOT NULL
) WITH (fillfactor = 90);

CREATE INDEX IF NOT EXISTS idx_article_header_poster_refs_poster_id
    ON public.article_header_poster_refs (poster_id);

CREATE INDEX IF NOT EXISTS idx_article_header_poster_refs_poster_key
    ON public.article_header_poster_refs (poster_key);

CREATE TABLE IF NOT EXISTS public.poster_materialization_queue (
    article_header_id bigint PRIMARY KEY REFERENCES public.article_headers(id) ON DELETE CASCADE,
    poster_name text DEFAULT ''::text NOT NULL,
    poster_key text DEFAULT ''::text NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    ready_at timestamptz DEFAULT now() NOT NULL,
    lease_owner text DEFAULT ''::text NOT NULL,
    lease_expires_at timestamptz,
    attempt_count integer DEFAULT 0 NOT NULL,
    last_error text DEFAULT ''::text NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    updated_at timestamptz DEFAULT now() NOT NULL
) WITH (fillfactor = 80);

CREATE INDEX IF NOT EXISTS idx_poster_materialization_queue_ready
    ON public.poster_materialization_queue (ready_at, article_header_id)
    WHERE status IN ('pending', 'failed');

CREATE INDEX IF NOT EXISTS idx_poster_materialization_queue_poster_key
    ON public.poster_materialization_queue (poster_key)
    WHERE status IN ('pending', 'failed');

CREATE TABLE IF NOT EXISTS public.crosspost_popularity_refresh_queue (
    observed_group_name text PRIMARY KEY,
    status text DEFAULT 'pending'::text NOT NULL,
    ready_at timestamptz DEFAULT now() NOT NULL,
    lease_owner text DEFAULT ''::text NOT NULL,
    lease_expires_at timestamptz,
    attempt_count integer DEFAULT 0 NOT NULL,
    last_error text DEFAULT ''::text NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    updated_at timestamptz DEFAULT now() NOT NULL
) WITH (fillfactor = 80);

CREATE INDEX IF NOT EXISTS idx_crosspost_popularity_refresh_queue_ready
    ON public.crosspost_popularity_refresh_queue (ready_at, observed_group_name)
    WHERE status IN ('pending', 'failed');

ALTER TABLE public.poster_materialization_queue SET (
    autovacuum_vacuum_scale_factor = 0.01,
    autovacuum_analyze_scale_factor = 0.02,
    autovacuum_vacuum_threshold = 5000,
    autovacuum_analyze_threshold = 5000
);

ALTER TABLE public.crosspost_popularity_refresh_queue SET (
    autovacuum_vacuum_scale_factor = 0.01,
    autovacuum_analyze_scale_factor = 0.02,
    autovacuum_vacuum_threshold = 5000,
    autovacuum_analyze_threshold = 5000
);

INSERT INTO public.indexer_table_write_ownership (table_name, owner_stage, allowed_writer_stages, notes)
VALUES
    ('posters', 'poster_materialize', ARRAY['poster_materialize', 'assemble_lane_a', 'assemble_lane_b'], 'Poster dimension. Scrape queues poster materialization instead of writing this table inline; assemble legacy poster writes remain transitional.'),
    ('article_header_poster_refs', 'poster_materialize', ARRAY['poster_materialize'], 'Per-header poster projection written by poster_materialize from raw ingest payloads.'),
    ('poster_materialization_queue', 'scrape', ARRAY['scrape_latest', 'scrape_backfill', 'poster_materialize'], 'Scrape enqueues raw poster names; poster_materialize claims and completes queue rows.'),
    ('article_header_crosspost_groups', 'scrape', ARRAY['scrape_latest', 'scrape_backfill'], 'Immutable/raw Xref observations seeded by scrape and manual backfill.'),
    ('crosspost_popularity_refresh_queue', 'scrape', ARRAY['scrape_latest', 'scrape_backfill', 'crosspost_popularity_refresh'], 'Scrape queues dirty observed cross-post groups; crosspost_popularity_refresh claims and completes queue rows.'),
    ('article_header_crosspost_group_summary', 'crosspost_popularity_refresh', ARRAY['crosspost_popularity_refresh'], 'Exact popularity rollup derived from raw cross-post observations.'),
    ('article_header_crosspost_group_messages', 'crosspost_popularity_refresh', ARRAY['crosspost_popularity_refresh'], 'Distinct observed message ids per cross-post group.'),
    ('article_header_crosspost_group_sources', 'crosspost_popularity_refresh', ARRAY['crosspost_popularity_refresh'], 'Distinct source newsgroups per cross-post group.')
ON CONFLICT (table_name) DO UPDATE
SET owner_stage = EXCLUDED.owner_stage,
    allowed_writer_stages = EXCLUDED.allowed_writer_stages,
    notes = EXCLUDED.notes,
    updated_at = now();

INSERT INTO public.poster_materialization_queue (
    article_header_id,
    poster_name,
    poster_key,
    status,
    ready_at,
    created_at,
    updated_at
)
SELECT
    aip.article_header_id,
    COALESCE(NULLIF(BTRIM(aip.poster), ''), p.poster_name, '') AS poster_name,
    LOWER(BTRIM(COALESCE(NULLIF(aip.poster, ''), p.poster_name, ''))) AS poster_key,
    'pending',
    now(),
    now(),
    now()
FROM public.article_header_ingest_payloads aip
LEFT JOIN public.posters p ON p.id = aip.poster_id
WHERE BTRIM(COALESCE(aip.poster, p.poster_name, '')) <> ''
ON CONFLICT (article_header_id) DO NOTHING;

INSERT INTO public.crosspost_popularity_refresh_queue (
    observed_group_name,
    status,
    ready_at,
    created_at,
    updated_at
)
SELECT DISTINCT
    observed_group_name,
    'pending',
    now(),
    now(),
    now()
FROM public.article_header_crosspost_groups
WHERE BTRIM(COALESCE(observed_group_name, '')) <> ''
ON CONFLICT (observed_group_name) DO NOTHING;
