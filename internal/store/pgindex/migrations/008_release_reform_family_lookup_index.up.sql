CREATE INDEX IF NOT EXISTS idx_binary_identity_release_family_provider
    ON public.binary_identity_current USING btree (provider_id, release_family_key)
    WHERE (btrim(release_family_key) <> ''::text);
