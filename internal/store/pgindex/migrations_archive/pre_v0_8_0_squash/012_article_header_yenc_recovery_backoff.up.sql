ALTER TABLE article_header_ingest_payloads
    ADD COLUMN IF NOT EXISTS yenc_recovery_missing_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS yenc_recovery_last_missing_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS yenc_recovery_retry_after TIMESTAMPTZ;
