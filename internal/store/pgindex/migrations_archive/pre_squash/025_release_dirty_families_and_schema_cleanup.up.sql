-- Track changed release families directly and remove dead schema/indexes.

CREATE TABLE IF NOT EXISTS release_stage_dirty_families (
  provider_id BIGINT NOT NULL REFERENCES usenet_providers(id) ON DELETE CASCADE,
  newsgroup_id BIGINT NOT NULL REFERENCES newsgroups(id) ON DELETE CASCADE,
  key_kind TEXT NOT NULL,
  family_key TEXT NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (provider_id, newsgroup_id, key_kind, family_key)
);

CREATE INDEX IF NOT EXISTS idx_release_stage_dirty_families_updated_at
ON release_stage_dirty_families(updated_at, provider_id, newsgroup_id);

INSERT INTO release_stage_dirty_families (
  provider_id,
  newsgroup_id,
  key_kind,
  family_key,
  updated_at
)
SELECT DISTINCT
  provider_id,
  newsgroup_id,
  'release_family',
  release_family_key,
  NOW()
FROM binaries
WHERE BTRIM(release_family_key) <> ''
ON CONFLICT (provider_id, newsgroup_id, key_kind, family_key) DO UPDATE
SET updated_at = EXCLUDED.updated_at;

INSERT INTO release_stage_dirty_families (
  provider_id,
  newsgroup_id,
  key_kind,
  family_key,
  updated_at
)
SELECT DISTINCT
  provider_id,
  newsgroup_id,
  'base_stem',
  LOWER(BTRIM(base_stem)),
  NOW()
FROM binaries
WHERE expected_file_count > 1
  AND BTRIM(base_stem) <> ''
ON CONFLICT (provider_id, newsgroup_id, key_kind, family_key) DO UPDATE
SET updated_at = EXCLUDED.updated_at;

DROP INDEX IF EXISTS idx_binary_parts_message_id;
DROP INDEX IF EXISTS idx_binaries_source_release_key;

DROP TABLE IF EXISTS regex_hits;
DROP TABLE IF EXISTS regex_rules;
DROP TABLE IF EXISTS part_repair_queue;
