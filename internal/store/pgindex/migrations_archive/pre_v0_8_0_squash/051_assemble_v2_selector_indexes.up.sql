CREATE INDEX IF NOT EXISTS idx_binary_identity_current_normalized_name
    ON public.binary_identity_current (
        provider_id,
        newsgroup_id,
        lower(btrim(coalesce(nullif(file_name, ''), nullif(binary_name, '')))),
        is_main_payload,
        binary_id DESC
    )
    WHERE btrim(coalesce(nullif(file_name, ''), nullif(binary_name, ''))) <> '';

CREATE INDEX IF NOT EXISTS idx_binary_observation_incomplete
    ON public.binary_observation_stats (
        binary_id,
        observed_parts DESC,
        total_parts
    )
    WHERE total_parts > 0
      AND observed_parts < total_parts;
