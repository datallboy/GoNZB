CREATE TABLE IF NOT EXISTS release_archive_state (
    release_id text PRIMARY KEY REFERENCES public.releases(release_id) ON DELETE CASCADE,
    archive_status text NOT NULL DEFAULT 'active',
    archive_store text NOT NULL DEFAULT 'indexer_archive',
    object_store_kind text NOT NULL DEFAULT 'fs',
    object_key text NOT NULL DEFAULT '',
    content_hash_sha256 text NOT NULL DEFAULT '',
    object_size_bytes bigint NOT NULL DEFAULT 0,
    content_encoding text NOT NULL DEFAULT 'identity',
    source_module text NOT NULL DEFAULT 'usenet_index',
    archived_at timestamp with time zone,
    purge_eligible_at timestamp with time zone,
    purge_completed_at timestamp with time zone,
    last_archive_error text NOT NULL DEFAULT '',
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    updated_at timestamp with time zone NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_release_archive_state_status
ON release_archive_state(archive_status, purge_eligible_at, archived_at);

CREATE TABLE IF NOT EXISTS release_archive_lineage_binaries (
    release_id text NOT NULL REFERENCES public.release_archive_state(release_id) ON DELETE CASCADE,
    binary_id bigint NOT NULL,
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    PRIMARY KEY (release_id, binary_id)
);

CREATE INDEX IF NOT EXISTS idx_release_archive_lineage_binaries_binary_id
ON release_archive_lineage_binaries(binary_id);

CREATE TABLE IF NOT EXISTS release_archive_lineage_article_headers (
    release_id text NOT NULL REFERENCES public.release_archive_state(release_id) ON DELETE CASCADE,
    article_header_id bigint NOT NULL,
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    PRIMARY KEY (release_id, article_header_id)
);

CREATE INDEX IF NOT EXISTS idx_release_archive_lineage_article_headers_article_header_id
ON release_archive_lineage_article_headers(article_header_id);
