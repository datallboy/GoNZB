-- Preserve existing source behavior during upgrade. New sources explicitly
-- persist the secure false default from runtime settings.
ALTER TABLE settings_indexers
    ADD COLUMN allow_private_addresses BOOLEAN NOT NULL DEFAULT 1;

ALTER TABLE settings_indexers
    ADD COLUMN allowed_cidrs_json TEXT NOT NULL DEFAULT '[]';
