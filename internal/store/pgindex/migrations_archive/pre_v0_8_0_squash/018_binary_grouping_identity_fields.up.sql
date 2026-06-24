ALTER TABLE public.binaries
    ADD COLUMN IF NOT EXISTS file_set_key text NOT NULL DEFAULT ''::text;

ALTER TABLE public.binaries
    ADD COLUMN IF NOT EXISTS identity_strength text NOT NULL DEFAULT ''::text;

ALTER TABLE public.binaries
    ADD COLUMN IF NOT EXISTS identity_reason text NOT NULL DEFAULT ''::text;

ALTER TABLE public.binaries
    ADD COLUMN IF NOT EXISTS subject_set_token text NOT NULL DEFAULT ''::text;

ALTER TABLE public.binaries
    ADD COLUMN IF NOT EXISTS subject_set_kind text NOT NULL DEFAULT ''::text;

CREATE INDEX IF NOT EXISTS idx_binaries_file_set_key
    ON public.binaries (provider_id, newsgroup_id, file_set_key)
    WHERE btrim(file_set_key) <> '';

CREATE INDEX IF NOT EXISTS idx_binaries_identity_strength
    ON public.binaries (provider_id, newsgroup_id, identity_strength, identity_reason)
    WHERE btrim(identity_strength) <> '';
