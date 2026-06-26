CREATE INDEX IF NOT EXISTS idx_binary_identity_subject_regroup_lookup
    ON public.binary_identity_current (
        provider_id,
        newsgroup_id,
        lower(btrim(file_name)),
        source_posted_at,
        binary_id
    )
    WHERE family_kind = 'contextual_obfuscated'
      AND identity_reason = 'contextual_fallback'
      AND btrim(COALESCE(file_name, '')) <> '';

CREATE INDEX IF NOT EXISTS idx_yenc_recovery_ready_date_priority_claim
    ON public.yenc_recovery_work_items (
        date_utc DESC NULLS LAST,
        priority_rank,
        updated_at DESC,
        binary_id
    )
    WHERE status = 'ready'
      AND btrim(COALESCE(message_id, '')) <> '';

DELETE FROM public.release_family_summary_refresh_queue q
USING public.release_family_summary_refresh_queue older
WHERE q.ctid > older.ctid
  AND q.provider_id = older.provider_id
  AND q.newsgroup_id = older.newsgroup_id
  AND q.key_kind = older.key_kind
  AND q.family_key = older.family_key;

CREATE UNIQUE INDEX IF NOT EXISTS idx_release_family_summary_refresh_queue_key
    ON public.release_family_summary_refresh_queue (
        provider_id,
        newsgroup_id,
        key_kind,
        family_key
    );
