CREATE TABLE IF NOT EXISTS release_family_readiness_acks (
    provider_id bigint NOT NULL,
    newsgroup_id bigint NOT NULL,
    key_kind text NOT NULL,
    family_key text NOT NULL,
    processed_at timestamp with time zone NOT NULL DEFAULT NOW(),
    updated_at timestamp with time zone NOT NULL DEFAULT NOW(),
    PRIMARY KEY (provider_id, newsgroup_id, key_kind, family_key)
);

CREATE INDEX IF NOT EXISTS idx_release_family_readiness_acks_processed_at
    ON release_family_readiness_acks (processed_at);
