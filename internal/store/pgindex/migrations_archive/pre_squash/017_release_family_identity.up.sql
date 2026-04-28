-- Separate source matcher keys from stable release-family grouping keys.

ALTER TABLE binaries
ADD COLUMN IF NOT EXISTS source_release_key TEXT NOT NULL DEFAULT '',
ADD COLUMN IF NOT EXISTS release_family_key TEXT NOT NULL DEFAULT '',
ADD COLUMN IF NOT EXISTS file_family_key TEXT NOT NULL DEFAULT '',
ADD COLUMN IF NOT EXISTS family_kind TEXT NOT NULL DEFAULT '',
ADD COLUMN IF NOT EXISTS base_stem TEXT NOT NULL DEFAULT '',
ADD COLUMN IF NOT EXISTS posting_bucket TEXT NOT NULL DEFAULT '',
ADD COLUMN IF NOT EXISTS is_auxiliary BOOLEAN NOT NULL DEFAULT FALSE,
ADD COLUMN IF NOT EXISTS is_main_payload BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE binaries
SET source_release_key = release_key
WHERE source_release_key = '';

UPDATE binaries
SET release_family_key = release_key
WHERE release_family_key = '';

UPDATE binaries
SET file_family_key = COALESCE(NULLIF(release_key, ''), NULLIF(file_name, ''), binary_key)
WHERE file_family_key = '';

UPDATE binaries
SET base_stem = trim(regexp_replace(lower(COALESCE(NULLIF(file_name, ''), binary_name)), '(\.vol[0-9]+\+[0-9]+\.par2|\.par2|\.7z\.[0-9]{3}|\.zip\.[0-9]{3}|\.part[0-9]+\.rar|\.r[0-9]{2,3}|\.rar|\.nfo|\.sfv|\.srr|\.mkv|\.mp4|\.avi|\.mp3|\.flac)$', '', 'i'))
WHERE base_stem = '';

UPDATE binaries
SET posting_bucket = CASE
	WHEN posted_at IS NULL THEN ''
	ELSE to_char(posted_at AT TIME ZONE 'UTC', 'YYYYMMDD') || '-' || floor(extract(hour from posted_at AT TIME ZONE 'UTC') / 6)::text
END
WHERE posting_bucket = '';

UPDATE binaries
SET is_auxiliary = (
	lower(file_name) ~ '\.vol[0-9]+\+[0-9]+\.par2$' OR
	lower(file_name) ~ '\.par2$' OR
	lower(file_name) ~ '\.nfo$' OR
	lower(file_name) ~ '\.sfv$' OR
	lower(file_name) ~ '\.srr$' OR
	lower(file_name) LIKE '%sample%'
)
WHERE is_auxiliary = FALSE;

UPDATE binaries
SET is_main_payload = (file_name <> '' AND NOT is_auxiliary)
WHERE is_main_payload = FALSE;

UPDATE binaries
SET family_kind = CASE
	WHEN family_kind <> '' THEN family_kind
	WHEN base_stem <> '' AND base_stem <> release_family_key THEN 'archive_stem'
	ELSE 'legacy'
END
WHERE family_kind = '';

CREATE INDEX IF NOT EXISTS idx_binaries_release_family_key
ON binaries(provider_id, newsgroup_id, release_family_key);

CREATE INDEX IF NOT EXISTS idx_binaries_source_release_key
ON binaries(provider_id, newsgroup_id, source_release_key);

ALTER TABLE releases
ADD COLUMN IF NOT EXISTS source_release_key TEXT NOT NULL DEFAULT '',
ADD COLUMN IF NOT EXISTS release_family_key TEXT NOT NULL DEFAULT '';

UPDATE releases
SET source_release_key = release_key
WHERE source_release_key = '';

UPDATE releases
SET release_family_key = release_key
WHERE release_family_key = '';

CREATE INDEX IF NOT EXISTS idx_releases_provider_release_family_key
ON releases(provider_id, release_family_key);
