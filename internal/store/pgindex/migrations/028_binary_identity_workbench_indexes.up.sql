CREATE INDEX IF NOT EXISTS idx_binary_identity_strength_updated
    ON public.binary_identity_current (
        LOWER(COALESCE(identity_strength, '')),
        updated_at DESC,
        binary_id DESC,
        source_posted_at
    );
