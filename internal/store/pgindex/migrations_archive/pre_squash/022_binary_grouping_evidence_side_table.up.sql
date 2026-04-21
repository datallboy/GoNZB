-- Move verbose binary grouping evidence out of the hot binaries table.

CREATE TABLE IF NOT EXISTS binary_grouping_evidence (
	binary_id BIGINT PRIMARY KEY REFERENCES binaries(id) ON DELETE CASCADE,
	evidence_source TEXT NOT NULL DEFAULT 'matcher',
	evidence_version TEXT NOT NULL DEFAULT 'v1',
	payload_json JSONB NOT NULL DEFAULT '{}'::jsonb,
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO binary_grouping_evidence (
	binary_id,
	evidence_source,
	evidence_version,
	payload_json,
	updated_at
)
SELECT
	id,
	'matcher',
	'v1',
	grouping_evidence_json,
	NOW()
FROM binaries
WHERE grouping_evidence_json <> '{}'::jsonb
ON CONFLICT (binary_id) DO UPDATE
SET payload_json = EXCLUDED.payload_json,
	updated_at = EXCLUDED.updated_at;

UPDATE binaries
SET grouping_evidence_json = '{}'::jsonb
WHERE grouping_evidence_json <> '{}'::jsonb;
