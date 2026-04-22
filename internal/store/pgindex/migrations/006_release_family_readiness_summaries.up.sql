CREATE TABLE IF NOT EXISTS public.release_family_readiness_summaries (
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    key_kind text NOT NULL,
    family_key text NOT NULL,
    source_release_key text DEFAULT ''::text NOT NULL,
    release_key text DEFAULT ''::text NOT NULL,
    release_name text DEFAULT ''::text NOT NULL,
    binary_count integer DEFAULT 0 NOT NULL,
    complete_binary_count integer DEFAULT 0 NOT NULL,
    incomplete_binary_count integer DEFAULT 0 NOT NULL,
    has_expected_file_count boolean DEFAULT false NOT NULL,
    total_bytes bigint DEFAULT 0 NOT NULL,
    earliest_posted_at timestamp with time zone,
    readiness_bucket text DEFAULT 'fragment_only'::text NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT release_family_readiness_summaries_pkey PRIMARY KEY (provider_id, newsgroup_id, key_kind, family_key),
    CONSTRAINT release_family_readiness_summaries_provider_id_fkey FOREIGN KEY (provider_id) REFERENCES public.usenet_providers(id) ON DELETE CASCADE,
    CONSTRAINT release_family_readiness_summaries_newsgroup_id_fkey FOREIGN KEY (newsgroup_id) REFERENCES public.newsgroups(id) ON DELETE CASCADE
);

INSERT INTO public.release_family_readiness_summaries (
    provider_id,
    newsgroup_id,
    key_kind,
    family_key,
    source_release_key,
    release_key,
    release_name,
    binary_count,
    complete_binary_count,
    incomplete_binary_count,
    has_expected_file_count,
    total_bytes,
    earliest_posted_at,
    readiness_bucket,
    updated_at
)
SELECT
    b.provider_id,
    b.newsgroup_id,
    'release_family'::text AS key_kind,
    b.release_family_key AS family_key,
    COALESCE(MAX(b.source_release_key), '') AS source_release_key,
    COALESCE(MAX(b.release_key), '') AS release_key,
    COALESCE(MAX(b.release_name), '') AS release_name,
    COUNT(*)::integer AS binary_count,
    COALESCE(SUM(
        CASE
            WHEN b.observed_parts = b.total_parts AND b.total_parts > 0 THEN 1
            ELSE 0
        END
    ), 0)::integer AS complete_binary_count,
    COUNT(*)::integer - COALESCE(SUM(
        CASE
            WHEN b.observed_parts = b.total_parts AND b.total_parts > 0 THEN 1
            ELSE 0
        END
    ), 0)::integer AS incomplete_binary_count,
    COALESCE(BOOL_OR(b.expected_file_count > 0), false) AS has_expected_file_count,
    COALESCE(SUM(b.total_bytes), 0)::bigint AS total_bytes,
    MIN(b.posted_at) AS earliest_posted_at,
    CASE
        WHEN COALESCE(SUM(
            CASE
                WHEN b.observed_parts = b.total_parts AND b.total_parts > 0 THEN 1
                ELSE 0
            END
        ), 0) > 0 THEN 'actionable'
        ELSE 'fragment_only'
    END AS readiness_bucket,
    NOW() AS updated_at
FROM public.binaries b
WHERE BTRIM(b.release_family_key) <> ''
GROUP BY b.provider_id, b.newsgroup_id, b.release_family_key
ON CONFLICT (provider_id, newsgroup_id, key_kind, family_key) DO UPDATE
SET source_release_key = EXCLUDED.source_release_key,
    release_key = EXCLUDED.release_key,
    release_name = EXCLUDED.release_name,
    binary_count = EXCLUDED.binary_count,
    complete_binary_count = EXCLUDED.complete_binary_count,
    incomplete_binary_count = EXCLUDED.incomplete_binary_count,
    has_expected_file_count = EXCLUDED.has_expected_file_count,
    total_bytes = EXCLUDED.total_bytes,
    earliest_posted_at = EXCLUDED.earliest_posted_at,
    readiness_bucket = EXCLUDED.readiness_bucket,
    updated_at = NOW();

INSERT INTO public.release_family_readiness_summaries (
    provider_id,
    newsgroup_id,
    key_kind,
    family_key,
    source_release_key,
    release_key,
    release_name,
    binary_count,
    complete_binary_count,
    incomplete_binary_count,
    has_expected_file_count,
    total_bytes,
    earliest_posted_at,
    readiness_bucket,
    updated_at
)
SELECT
    b.provider_id,
    b.newsgroup_id,
    'base_stem'::text AS key_kind,
    LOWER(BTRIM(b.base_stem)) AS family_key,
    COALESCE(MAX(b.source_release_key), '') AS source_release_key,
    COALESCE(MAX(b.release_key), '') AS release_key,
    COALESCE(MAX(b.release_name), '') AS release_name,
    COUNT(*)::integer AS binary_count,
    COALESCE(SUM(
        CASE
            WHEN b.observed_parts = b.total_parts AND b.total_parts > 0 THEN 1
            ELSE 0
        END
    ), 0)::integer AS complete_binary_count,
    COUNT(*)::integer - COALESCE(SUM(
        CASE
            WHEN b.observed_parts = b.total_parts AND b.total_parts > 0 THEN 1
            ELSE 0
        END
    ), 0)::integer AS incomplete_binary_count,
    COALESCE(BOOL_OR(b.expected_file_count > 0), false) AS has_expected_file_count,
    COALESCE(SUM(b.total_bytes), 0)::bigint AS total_bytes,
    MIN(b.posted_at) AS earliest_posted_at,
    CASE
        WHEN COALESCE(SUM(
            CASE
                WHEN b.observed_parts = b.total_parts AND b.total_parts > 0 THEN 1
                ELSE 0
            END
        ), 0) > 0 THEN 'actionable'
        ELSE 'fragment_only'
    END AS readiness_bucket,
    NOW() AS updated_at
FROM public.binaries b
WHERE b.expected_file_count > 1
  AND BTRIM(b.base_stem) <> ''
GROUP BY b.provider_id, b.newsgroup_id, LOWER(BTRIM(b.base_stem))
ON CONFLICT (provider_id, newsgroup_id, key_kind, family_key) DO UPDATE
SET source_release_key = EXCLUDED.source_release_key,
    release_key = EXCLUDED.release_key,
    release_name = EXCLUDED.release_name,
    binary_count = EXCLUDED.binary_count,
    complete_binary_count = EXCLUDED.complete_binary_count,
    incomplete_binary_count = EXCLUDED.incomplete_binary_count,
    has_expected_file_count = EXCLUDED.has_expected_file_count,
    total_bytes = EXCLUDED.total_bytes,
    earliest_posted_at = EXCLUDED.earliest_posted_at,
    readiness_bucket = EXCLUDED.readiness_bucket,
    updated_at = NOW();
