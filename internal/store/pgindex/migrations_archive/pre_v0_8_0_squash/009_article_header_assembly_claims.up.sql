ALTER TABLE public.article_headers
    ADD COLUMN IF NOT EXISTS assembly_claimed_by TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS assembly_claimed_until TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_article_headers_pending_assembly_claims
ON public.article_headers (assembly_claimed_until, id DESC)
WHERE assembled_at IS NULL;
