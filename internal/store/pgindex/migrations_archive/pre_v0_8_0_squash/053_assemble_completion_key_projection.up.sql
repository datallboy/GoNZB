INSERT INTO public.indexer_table_write_ownership (table_name, owner_stage, allowed_writer_stages, notes)
VALUES
    ('article_header_assembly_keys', 'assemble', ARRAY['scrape_latest', 'scrape_backfill', 'assemble_lane_a', 'assemble_lane_b'], 'Assembly work surface seeded inline by scrape and completed by assemble after article headers are claimed into binary parts.'),
    ('binary_completion_keys', 'assemble', ARRAY['assemble_lane_a', 'assemble_lane_b', 'recover_yenc'], 'Binary-derived incomplete-file selector projection for assemble lane A. Recovery may refresh it after yEnc identity promotion.')
ON CONFLICT (table_name) DO UPDATE
SET owner_stage = EXCLUDED.owner_stage,
    allowed_writer_stages = EXCLUDED.allowed_writer_stages,
    notes = EXCLUDED.notes,
    updated_at = now();

CREATE TABLE IF NOT EXISTS public.article_header_assembly_keys (
    article_header_id bigint PRIMARY KEY REFERENCES public.article_headers(id) ON DELETE CASCADE,
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    normalized_file_name text NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    updated_at timestamptz DEFAULT now() NOT NULL,
    CHECK (btrim(normalized_file_name) <> '')
) WITH (fillfactor = 95);

CREATE INDEX IF NOT EXISTS idx_article_header_assembly_keys_match
    ON public.article_header_assembly_keys (
        provider_id,
        newsgroup_id,
        normalized_file_name,
        article_header_id DESC
    );

CREATE TABLE IF NOT EXISTS public.binary_completion_keys (
    binary_id bigint PRIMARY KEY REFERENCES public.binary_core(binary_id) ON DELETE CASCADE,
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    normalized_file_name text NOT NULL,
    is_main_payload boolean DEFAULT false NOT NULL,
    observed_parts integer DEFAULT 0 NOT NULL,
    total_parts integer DEFAULT 0 NOT NULL,
    completion_ratio double precision DEFAULT 0 NOT NULL,
    updated_at timestamptz DEFAULT now() NOT NULL,
    CHECK (btrim(normalized_file_name) <> '')
) WITH (fillfactor = 80);

CREATE INDEX IF NOT EXISTS idx_binary_completion_keys_rank
    ON public.binary_completion_keys (
        is_main_payload DESC,
        completion_ratio DESC,
        observed_parts DESC,
        binary_id DESC
    );

CREATE INDEX IF NOT EXISTS idx_binary_completion_keys_match
    ON public.binary_completion_keys (
        provider_id,
        newsgroup_id,
        normalized_file_name,
        is_main_payload DESC,
        observed_parts DESC,
        binary_id DESC
    );

INSERT INTO public.article_header_assembly_keys (
    article_header_id,
    provider_id,
    newsgroup_id,
    normalized_file_name,
    created_at,
    updated_at
)
SELECT
    ah.id,
    ah.provider_id,
    ah.newsgroup_id,
    lower(btrim(p.subject_file_name)),
    now(),
    now()
FROM public.article_headers ah
JOIN public.article_header_ingest_payloads p
  ON p.article_header_id = ah.id
WHERE btrim(coalesce(p.subject_file_name, '')) <> ''
ON CONFLICT (article_header_id) DO UPDATE
SET provider_id = EXCLUDED.provider_id,
    newsgroup_id = EXCLUDED.newsgroup_id,
    normalized_file_name = EXCLUDED.normalized_file_name,
    updated_at = now();

INSERT INTO public.binary_completion_keys (
    binary_id,
    provider_id,
    newsgroup_id,
    normalized_file_name,
    is_main_payload,
    observed_parts,
    total_parts,
    completion_ratio,
    updated_at
)
SELECT
    bic.binary_id,
    bic.provider_id,
    bic.newsgroup_id,
    lower(btrim(coalesce(nullif(bic.file_name, ''), nullif(bic.binary_name, '')))),
    bic.is_main_payload,
    bos.observed_parts,
    bos.total_parts,
    CASE
        WHEN bos.total_parts > 0 THEN bos.observed_parts::double precision / bos.total_parts::double precision
        ELSE 0
    END,
    now()
FROM public.binary_identity_current bic
JOIN public.binary_observation_stats bos
  ON bos.binary_id = bic.binary_id
WHERE bos.total_parts > 0
  AND bos.observed_parts < bos.total_parts
  AND btrim(coalesce(nullif(bic.file_name, ''), nullif(bic.binary_name, ''))) <> ''
ON CONFLICT (binary_id) DO UPDATE
SET provider_id = EXCLUDED.provider_id,
    newsgroup_id = EXCLUDED.newsgroup_id,
    normalized_file_name = EXCLUDED.normalized_file_name,
    is_main_payload = EXCLUDED.is_main_payload,
    observed_parts = EXCLUDED.observed_parts,
    total_parts = EXCLUDED.total_parts,
    completion_ratio = EXCLUDED.completion_ratio,
    updated_at = now();
