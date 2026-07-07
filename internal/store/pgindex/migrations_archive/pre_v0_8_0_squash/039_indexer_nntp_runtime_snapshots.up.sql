CREATE TABLE IF NOT EXISTS indexer_nntp_runtime_snapshots (
    publisher_id text PRIMARY KEY,
    module_name text NOT NULL,
    scope text NOT NULL,
    payload jsonb NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_indexer_nntp_runtime_snapshots_module_updated_at
ON indexer_nntp_runtime_snapshots (module_name, updated_at DESC);
