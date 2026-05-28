DROP INDEX IF EXISTS public.idx_binaries_file_set_key;

CREATE INDEX IF NOT EXISTS idx_binaries_file_set_key
    ON public.binaries (provider_id, file_set_key, newsgroup_id)
    WHERE btrim(file_set_key) <> '';
