CREATE TABLE IF NOT EXISTS article_header_crosspost_groups (
    article_header_id bigint NOT NULL REFERENCES article_headers(id) ON DELETE CASCADE,
    provider_id bigint NOT NULL,
    source_newsgroup_id bigint NOT NULL REFERENCES newsgroups(id) ON DELETE RESTRICT,
    message_id text NOT NULL DEFAULT '',
    observed_group_name text NOT NULL DEFAULT '',
    observed_article_number bigint NOT NULL DEFAULT 0,
    observed_at timestamptz NOT NULL DEFAULT NOW(),
    PRIMARY KEY (article_header_id, observed_group_name)
);

CREATE INDEX IF NOT EXISTS idx_article_header_crosspost_groups_group_name_observed_at
ON article_header_crosspost_groups (observed_group_name, observed_at DESC);

CREATE INDEX IF NOT EXISTS idx_article_header_crosspost_groups_provider_group
ON article_header_crosspost_groups (provider_id, observed_group_name);
