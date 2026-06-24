CREATE TABLE IF NOT EXISTS indexer_provider_group_inventory (
    provider_id text NOT NULL,
    provider_name text NOT NULL DEFAULT '',
    group_name text NOT NULL,
    high bigint NOT NULL DEFAULT 0,
    low bigint NOT NULL DEFAULT 0,
    status text NOT NULL DEFAULT '',
    scanned_at timestamptz NOT NULL DEFAULT NOW(),
    PRIMARY KEY (provider_id, group_name)
);

CREATE INDEX IF NOT EXISTS idx_indexer_provider_group_inventory_group_name
ON indexer_provider_group_inventory (lower(group_name));

CREATE INDEX IF NOT EXISTS idx_indexer_provider_group_inventory_scanned_at
ON indexer_provider_group_inventory (scanned_at DESC);
