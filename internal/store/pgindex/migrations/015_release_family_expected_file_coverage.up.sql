ALTER TABLE public.release_family_readiness_summaries
    ADD COLUMN IF NOT EXISTS expected_file_count integer DEFAULT 0 NOT NULL;

ALTER TABLE public.release_family_readiness_summaries
    ADD COLUMN IF NOT EXISTS complete_main_payload_binary_count integer DEFAULT 0 NOT NULL;

ALTER TABLE public.release_family_readiness_summaries
    ADD COLUMN IF NOT EXISTS expected_file_coverage_pct double precision DEFAULT 0 NOT NULL;

WITH release_family_rollup AS (
    SELECT
        b.provider_id,
        b.newsgroup_id,
        'release_family'::text AS key_kind,
        b.release_family_key AS family_key,
        COALESCE(MAX(b.expected_file_count), 0)::integer AS expected_file_count,
        COALESCE(SUM(
            CASE
                WHEN (b.is_main_payload OR NOT b.is_auxiliary)
                 AND b.observed_parts = b.total_parts
                 AND b.total_parts > 0 THEN 1
                ELSE 0
            END
        ), 0)::integer AS complete_main_payload_binary_count
    FROM public.binaries b
    WHERE BTRIM(b.release_family_key) <> ''
    GROUP BY b.provider_id, b.newsgroup_id, b.release_family_key
),
base_stem_rollup AS (
    SELECT
        b.provider_id,
        b.newsgroup_id,
        'base_stem'::text AS key_kind,
        LOWER(BTRIM(b.base_stem)) AS family_key,
        COALESCE(MAX(b.expected_file_count), 0)::integer AS expected_file_count,
        COALESCE(SUM(
            CASE
                WHEN (b.is_main_payload OR NOT b.is_auxiliary)
                 AND b.observed_parts = b.total_parts
                 AND b.total_parts > 0 THEN 1
                ELSE 0
            END
        ), 0)::integer AS complete_main_payload_binary_count
    FROM public.binaries b
    WHERE b.expected_file_count > 1
      AND BTRIM(b.base_stem) <> ''
    GROUP BY b.provider_id, b.newsgroup_id, LOWER(BTRIM(b.base_stem))
),
merged AS (
    SELECT * FROM release_family_rollup
    UNION ALL
    SELECT * FROM base_stem_rollup
)
UPDATE public.release_family_readiness_summaries s
SET expected_file_count = m.expected_file_count,
    complete_main_payload_binary_count = m.complete_main_payload_binary_count,
    expected_file_coverage_pct = CASE
        WHEN m.expected_file_count > 0 THEN LEAST(100.0, (m.complete_main_payload_binary_count::double precision / m.expected_file_count::double precision) * 100.0)
        ELSE 0
    END,
    readiness_bucket = CASE
        WHEN m.complete_main_payload_binary_count > 0 THEN 'actionable'
        ELSE 'fragment_only'
    END,
    updated_at = NOW()
FROM merged m
WHERE s.provider_id = m.provider_id
  AND s.newsgroup_id = m.newsgroup_id
  AND s.key_kind = m.key_kind
  AND s.family_key = m.family_key;
