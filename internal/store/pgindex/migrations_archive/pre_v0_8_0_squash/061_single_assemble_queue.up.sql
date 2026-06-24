INSERT INTO public.indexer_table_write_ownership (table_name, owner_stage, allowed_writer_stages, notes)
VALUES
    ('article_header_assembly_queue', 'assemble', ARRAY['scrape_latest', 'scrape_backfill', 'assemble'], 'Canonical assemble work surface. Scrape seeds rows; assemble owns claims, retries, and completion deletes.'),
    ('binary_completion_keys', 'assemble', ARRAY['assemble', 'recover_yenc'], 'Binary-derived incomplete-file selector projection for assemble Lane A. Recovery may refresh it after yEnc identity promotion.'),
    ('binary_core', 'assemble', ARRAY['assemble'], 'Canonical binary anchor projection.'),
    ('binary_observation_stats', 'assemble', ARRAY['assemble', 'recover_yenc'], 'Mutable part/byte/article bounds. Recovery may refresh stats after identity promotion.'),
    ('binary_identity_current', 'assemble', ARRAY['assemble', 'recover_yenc'], 'Current release-family/file-set identity. Recovery may promote stronger identity discovered from yEnc headers.'),
    ('binary_parts', 'assemble', ARRAY['assemble'], 'Canonical binary part membership rows written by assemble.')
ON CONFLICT (table_name) DO UPDATE
SET owner_stage = EXCLUDED.owner_stage,
    allowed_writer_stages = EXCLUDED.allowed_writer_stages,
    notes = EXCLUDED.notes,
    updated_at = now();

CREATE TABLE IF NOT EXISTS public.article_header_assembly_queue (
    article_header_id bigint PRIMARY KEY REFERENCES public.article_headers(id) ON DELETE CASCADE,
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    article_number bigint NOT NULL,
    message_id text NOT NULL,
    normalized_file_name text DEFAULT '' NOT NULL,
    queue_kind text DEFAULT 'general' NOT NULL,
    claim_owner text DEFAULT '' NOT NULL,
    claim_token uuid,
    claim_until timestamptz,
    attempt_count integer DEFAULT 0 NOT NULL,
    last_error text DEFAULT '' NOT NULL,
    queued_at timestamptz DEFAULT now() NOT NULL,
    updated_at timestamptz DEFAULT now() NOT NULL,
    CHECK (queue_kind IN ('structured', 'general')),
    CHECK (queue_kind = 'general' OR btrim(normalized_file_name) <> '')
) WITH (fillfactor = 90);

CREATE INDEX IF NOT EXISTS idx_article_assembly_queue_claim
    ON public.article_header_assembly_queue (claim_until, article_header_id DESC);

CREATE INDEX IF NOT EXISTS idx_article_assembly_queue_structured_match
    ON public.article_header_assembly_queue (
        provider_id,
        newsgroup_id,
        normalized_file_name,
        claim_until,
        article_header_id DESC
    )
    WHERE normalized_file_name <> '';

CREATE INDEX IF NOT EXISTS idx_binary_parts_article_header_id
    ON public.binary_parts (article_header_id);

INSERT INTO public.article_header_assembly_queue (
    article_header_id,
    provider_id,
    newsgroup_id,
    article_number,
    message_id,
    normalized_file_name,
    queue_kind,
    queued_at,
    updated_at
)
SELECT
    ah.id,
    ah.provider_id,
    ah.newsgroup_id,
    ah.article_number,
    ah.message_id,
    lower(btrim(coalesce(p.subject_file_name, ''))),
    CASE WHEN btrim(coalesce(p.subject_file_name, '')) <> '' THEN 'structured' ELSE 'general' END,
    now(),
    now()
FROM public.article_headers ah
JOIN public.article_header_ingest_payloads p
  ON p.article_header_id = ah.id
WHERE NOT EXISTS (
    SELECT 1
    FROM public.binary_parts bp
    WHERE bp.article_header_id = ah.id
)
ON CONFLICT (article_header_id) DO UPDATE
SET provider_id = EXCLUDED.provider_id,
    newsgroup_id = EXCLUDED.newsgroup_id,
    article_number = EXCLUDED.article_number,
    message_id = EXCLUDED.message_id,
    normalized_file_name = EXCLUDED.normalized_file_name,
    queue_kind = EXCLUDED.queue_kind,
    updated_at = now();

DELETE FROM public.article_header_assembly_queue q
USING public.binary_parts bp
WHERE bp.article_header_id = q.article_header_id;

DROP INDEX IF EXISTS public.idx_article_header_assembly_keys_match;
DROP TABLE IF EXISTS public.article_header_assembly_keys;

DELETE FROM public.indexer_table_write_ownership
WHERE table_name = 'article_header_assembly_keys';
