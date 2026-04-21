CREATE TABLE IF NOT EXISTS binary_inspection_artifacts (
  id BIGSERIAL PRIMARY KEY,
  binary_id BIGINT NOT NULL REFERENCES binaries(id) ON DELETE CASCADE,
  release_id TEXT REFERENCES releases(release_id) ON DELETE SET NULL,
  stage_name TEXT NOT NULL DEFAULT '',
  artifact_role TEXT NOT NULL DEFAULT '',
  artifact_name TEXT NOT NULL DEFAULT '',
  artifact_path TEXT NOT NULL DEFAULT '',
  bytes_total BIGINT NOT NULL DEFAULT 0,
  mime_type TEXT NOT NULL DEFAULT '',
  signature TEXT NOT NULL DEFAULT '',
  source_kind TEXT NOT NULL DEFAULT '',
  metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (binary_id, stage_name, artifact_role, artifact_name)
);

CREATE INDEX IF NOT EXISTS idx_binary_inspection_artifacts_binary_stage
ON binary_inspection_artifacts(binary_id, stage_name, updated_at DESC);

CREATE TABLE IF NOT EXISTS binary_archive_entries (
  id BIGSERIAL PRIMARY KEY,
  binary_id BIGINT NOT NULL REFERENCES binaries(id) ON DELETE CASCADE,
  release_id TEXT REFERENCES releases(release_id) ON DELETE SET NULL,
  entry_name TEXT NOT NULL DEFAULT '',
  is_dir BOOLEAN NOT NULL DEFAULT FALSE,
  uncompressed_bytes BIGINT NOT NULL DEFAULT 0,
  compressed_bytes BIGINT NOT NULL DEFAULT 0,
  encrypted BOOLEAN NOT NULL DEFAULT FALSE,
  comment_text TEXT NOT NULL DEFAULT '',
  media_type TEXT NOT NULL DEFAULT '',
  signature TEXT NOT NULL DEFAULT '',
  metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (binary_id, entry_name)
);

CREATE INDEX IF NOT EXISTS idx_binary_archive_entries_binary
ON binary_archive_entries(binary_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS binary_media_streams (
  id BIGSERIAL PRIMARY KEY,
  binary_id BIGINT NOT NULL REFERENCES binaries(id) ON DELETE CASCADE,
  release_id TEXT REFERENCES releases(release_id) ON DELETE SET NULL,
  stream_index INTEGER NOT NULL DEFAULT 0,
  stream_type TEXT NOT NULL DEFAULT '',
  codec_name TEXT NOT NULL DEFAULT '',
  codec_long_name TEXT NOT NULL DEFAULT '',
  profile TEXT NOT NULL DEFAULT '',
  width INTEGER NOT NULL DEFAULT 0,
  height INTEGER NOT NULL DEFAULT 0,
  channels INTEGER NOT NULL DEFAULT 0,
  language TEXT NOT NULL DEFAULT '',
  duration_seconds DOUBLE PRECISION NOT NULL DEFAULT 0,
  bit_rate BIGINT NOT NULL DEFAULT 0,
  default_disposition BOOLEAN NOT NULL DEFAULT FALSE,
  forced_disposition BOOLEAN NOT NULL DEFAULT FALSE,
  metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (binary_id, stream_index, stream_type)
);

CREATE INDEX IF NOT EXISTS idx_binary_media_streams_binary
ON binary_media_streams(binary_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS binary_text_evidence (
  id BIGSERIAL PRIMARY KEY,
  binary_id BIGINT NOT NULL REFERENCES binaries(id) ON DELETE CASCADE,
  release_id TEXT REFERENCES releases(release_id) ON DELETE SET NULL,
  stage_name TEXT NOT NULL DEFAULT '',
  evidence_kind TEXT NOT NULL DEFAULT '',
  text_value TEXT NOT NULL DEFAULT '',
  tokens_json JSONB NOT NULL DEFAULT '[]'::jsonb,
  metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (binary_id, stage_name, evidence_kind)
);

CREATE INDEX IF NOT EXISTS idx_binary_text_evidence_binary_stage
ON binary_text_evidence(binary_id, stage_name, updated_at DESC);

CREATE TABLE IF NOT EXISTS binary_par2_sets (
  id BIGSERIAL PRIMARY KEY,
  binary_id BIGINT NOT NULL REFERENCES binaries(id) ON DELETE CASCADE,
  release_id TEXT REFERENCES releases(release_id) ON DELETE SET NULL,
  set_name TEXT NOT NULL DEFAULT '',
  base_name TEXT NOT NULL DEFAULT '',
  is_volume BOOLEAN NOT NULL DEFAULT FALSE,
  volume_number INTEGER NOT NULL DEFAULT 0,
  recovery_blocks INTEGER NOT NULL DEFAULT 0,
  signature_ok BOOLEAN NOT NULL DEFAULT FALSE,
  metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (binary_id, set_name)
);

CREATE INDEX IF NOT EXISTS idx_binary_par2_sets_binary
ON binary_par2_sets(binary_id, updated_at DESC);
