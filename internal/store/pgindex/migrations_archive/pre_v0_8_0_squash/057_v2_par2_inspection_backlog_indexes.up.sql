CREATE INDEX IF NOT EXISTS idx_binary_identity_inspect_par2_backlog
    ON public.binary_identity_current (updated_at DESC, binary_id DESC)
    INCLUDE (
        release_family_key,
        release_name,
        binary_name,
        file_name,
        match_confidence
    )
    WHERE LOWER(COALESCE(NULLIF(file_name, ''), NULLIF(binary_name, ''), '')) LIKE '%.par2';

CREATE INDEX IF NOT EXISTS idx_binary_recovery_inspect_par2_backlog
    ON public.binary_recovery_current (updated_at DESC, binary_id DESC)
    WHERE recovered_kind = 'par2'
       OR recovered_extension = '.par2';
