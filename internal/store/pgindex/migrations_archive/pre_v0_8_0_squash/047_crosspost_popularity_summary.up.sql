CREATE TABLE IF NOT EXISTS article_header_crosspost_group_summary (
    observed_group_name text PRIMARY KEY,
    observed_article_count bigint NOT NULL DEFAULT 0,
    distinct_message_count bigint NOT NULL DEFAULT 0,
    distinct_source_group_count bigint NOT NULL DEFAULT 0,
    last_seen_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_article_header_crosspost_group_summary_rank
ON article_header_crosspost_group_summary (
    distinct_message_count DESC,
    observed_article_count DESC,
    last_seen_at DESC,
    observed_group_name
);

CREATE TABLE IF NOT EXISTS article_header_crosspost_group_messages (
    observed_group_name text NOT NULL,
    message_id text NOT NULL,
    first_seen_at timestamptz NOT NULL DEFAULT NOW(),
    PRIMARY KEY (observed_group_name, message_id)
);

CREATE TABLE IF NOT EXISTS article_header_crosspost_group_sources (
    observed_group_name text NOT NULL,
    source_newsgroup_id bigint NOT NULL REFERENCES newsgroups(id) ON DELETE RESTRICT,
    first_seen_at timestamptz NOT NULL DEFAULT NOW(),
    PRIMARY KEY (observed_group_name, source_newsgroup_id)
);
