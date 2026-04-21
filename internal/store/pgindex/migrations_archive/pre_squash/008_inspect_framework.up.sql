CREATE TABLE IF NOT EXISTS binary_inspections (
  id BIGSERIAL PRIMARY KEY,
  stage_name TEXT NOT NULL,
  binary_id BIGINT NOT NULL REFERENCES binaries(id) ON DELETE CASCADE,
  release_id TEXT REFERENCES releases(release_id) ON DELETE SET NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  started_at TIMESTAMPTZ,
  finished_at TIMESTAMPTZ,
  error_text TEXT NOT NULL DEFAULT '',
  materialized_bytes BIGINT NOT NULL DEFAULT 0,
  tool_provenance_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  summary_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  source_updated_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (stage_name, binary_id)
);

CREATE INDEX IF NOT EXISTS idx_binary_inspections_stage_status
ON binary_inspections(stage_name, status, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_binary_inspections_release_id
ON binary_inspections(release_id);

CREATE TABLE IF NOT EXISTS release_password_candidates (
  id BIGSERIAL PRIMARY KEY,
  release_id TEXT NOT NULL REFERENCES releases(release_id) ON DELETE CASCADE,
  binary_id BIGINT REFERENCES binaries(id) ON DELETE SET NULL,
  artifact_id BIGINT,
  password_value TEXT NOT NULL,
  source_kind TEXT NOT NULL DEFAULT '',
  source_ref TEXT NOT NULL DEFAULT '',
  confidence DOUBLE PRECISION NOT NULL DEFAULT 0,
  verification_status TEXT NOT NULL DEFAULT 'pending',
  last_verified_at TIMESTAMPTZ,
  last_error TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (release_id, password_value, source_kind, source_ref)
);

CREATE INDEX IF NOT EXISTS idx_release_password_candidates_release_status
ON release_password_candidates(release_id, verification_status, updated_at DESC);
