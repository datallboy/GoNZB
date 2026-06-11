CREATE INDEX IF NOT EXISTS idx_release_family_readiness_bucket_lookup
ON release_family_readiness_summaries (
	readiness_bucket,
	provider_id,
	newsgroup_id,
	key_kind,
	family_key
);
