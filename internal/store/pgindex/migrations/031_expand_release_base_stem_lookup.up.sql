DROP INDEX IF EXISTS public.idx_binary_identity_base_stem_lookup;

CREATE INDEX idx_binary_identity_base_stem_lookup
ON public.binary_identity_current (
    provider_id,
    newsgroup_id,
    lower(btrim(base_stem)),
    source_posted_at,
    binary_id
)
WHERE btrim(base_stem) <> '';
