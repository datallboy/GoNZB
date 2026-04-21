-- release_family_key is now the operational binary-family key; the legacy release_key
-- index duplicates that hot path and can be removed.

UPDATE binaries
SET release_key = release_family_key
WHERE BTRIM(release_family_key) <> ''
  AND release_key IS DISTINCT FROM release_family_key;

DROP INDEX IF EXISTS idx_binaries_release_key;
