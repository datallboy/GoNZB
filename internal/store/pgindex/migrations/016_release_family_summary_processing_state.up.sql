ALTER TABLE public.release_family_readiness_summaries
    ADD COLUMN IF NOT EXISTS processed_at timestamptz;

CREATE INDEX IF NOT EXISTS idx_release_family_readiness_summaries_pending
ON public.release_family_readiness_summaries (updated_at, provider_id, newsgroup_id)
WHERE updated_at > COALESCE(processed_at, updated_at);

UPDATE public.release_family_readiness_summaries s
SET updated_at = GREATEST(s.updated_at, d.updated_at),
    processed_at = TIMESTAMPTZ 'epoch'
FROM public.release_stage_dirty_families d
WHERE s.provider_id = d.provider_id
  AND s.newsgroup_id = d.newsgroup_id
  AND s.key_kind = d.key_kind
  AND s.family_key = d.family_key;

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
    complete_main_payload_binary_count,
    incomplete_binary_count,
    expected_file_count,
    has_expected_file_count,
    total_bytes,
    earliest_posted_at,
    readiness_bucket,
    expected_file_coverage_pct,
    updated_at,
    processed_at
)
SELECT
    d.provider_id,
    d.newsgroup_id,
    d.key_kind,
    d.family_key,
    ''::text,
    ''::text,
    ''::text,
    0,
    0,
    0,
    0,
    0,
    false,
    0,
    NULL,
    'stale_cleanup_only'::text,
    0,
    d.updated_at,
    TIMESTAMPTZ 'epoch'
FROM public.release_stage_dirty_families d
LEFT JOIN public.release_family_readiness_summaries s
  ON s.provider_id = d.provider_id
 AND s.newsgroup_id = d.newsgroup_id
 AND s.key_kind = d.key_kind
 AND s.family_key = d.family_key
WHERE s.provider_id IS NULL
ON CONFLICT (provider_id, newsgroup_id, key_kind, family_key) DO NOTHING;
