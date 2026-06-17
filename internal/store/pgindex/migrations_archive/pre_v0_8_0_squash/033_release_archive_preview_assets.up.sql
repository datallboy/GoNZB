ALTER TABLE IF EXISTS release_archive_state
    ADD COLUMN IF NOT EXISTS preview_object_key text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS preview_content_type text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS preview_source_kind text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS preview_updated_at timestamp with time zone;
