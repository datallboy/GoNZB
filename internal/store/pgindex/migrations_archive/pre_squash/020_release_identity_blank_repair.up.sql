-- Repair legacy release rows that still carry blank family/source identity.

UPDATE releases
SET release_family_key = COALESCE(
	NULLIF(BTRIM(release_family_key), ''),
	NULLIF(BTRIM(release_key), ''),
	NULLIF(BTRIM(source_release_key), ''),
	NULLIF(BTRIM(group_name), '')
)
WHERE BTRIM(release_family_key) = '';

UPDATE releases
SET source_release_key = COALESCE(
	NULLIF(BTRIM(source_release_key), ''),
	NULLIF(BTRIM(release_family_key), ''),
	NULLIF(BTRIM(release_key), ''),
	NULLIF(BTRIM(group_name), '')
)
WHERE BTRIM(source_release_key) = '';
