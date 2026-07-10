ALTER TABLE coverage_assignments
  ADD COLUMN IF NOT EXISTS mode TEXT,
  ADD COLUMN IF NOT EXISTS assignment_role TEXT,
  ADD COLUMN IF NOT EXISTS provider_scope_hash TEXT,
  ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ;

UPDATE coverage_assignments
SET expires_at = COALESCE(expires_at, due_at)
WHERE expires_at IS NULL AND due_at IS NOT NULL;

ALTER TABLE coverage_claims
  ADD COLUMN IF NOT EXISTS provider_scope_hash TEXT,
  ADD COLUMN IF NOT EXISTS claim_mode TEXT,
  ADD COLUMN IF NOT EXISTS expected_checkpoint_interval_seconds INTEGER;

ALTER TABLE coverage_range_outcomes
  ADD COLUMN IF NOT EXISTS provider_scope_hash TEXT,
  ADD COLUMN IF NOT EXISTS articles_seen BIGINT,
  ADD COLUMN IF NOT EXISTS headers_processed BIGINT,
  ADD COLUMN IF NOT EXISTS manifests_emitted INTEGER,
  ADD COLUMN IF NOT EXISTS dedup_candidates_skipped INTEGER,
  ADD COLUMN IF NOT EXISTS error_count INTEGER,
  ADD COLUMN IF NOT EXISTS range_fingerprint TEXT,
  ADD COLUMN IF NOT EXISTS retryable BOOLEAN;
