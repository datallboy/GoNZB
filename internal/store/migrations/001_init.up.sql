-- Metadata lookup tables
CREATE TABLE IF NOT EXISTS posters (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT UNIQUE NOT NULL
);

CREATE TABLE IF NOT EXISTS groups (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT UNIQUE NOT NULL
);

-- Release Metadata
CREATE TABLE IF NOT EXISTS releases (
		id TEXT PRIMARY KEY,				-- Indexer GUID or NZB hash
        file_hash TEXT NOT NULL,            -- SHA256 content fingerprint
		poster_id INTEGER,
		title TEXT NOT NULL,
		password TEXT,
		guid TEXT,							-- Original indexer GUID
		source TEXT,						-- Indexer name or manual for upload
		download_url TEXT,
		size INTEGER NOT NULL DEFAULT 0,	-- Total bytes of all files in NZB
		publish_date INTEGER,
		category TEXT,
		redirect_allowed BOOLEAN DEFAULT 1, -- 0 for false, 1 for true
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(poster_id) REFERENCES posters(id)
);

CREATE INDEX IF NOT EXISTS idx_releases_file_hash ON releases(file_hash);

-- Individual files within release
CREATE TABLE IF NOT EXISTS release_files (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    release_id TEXT NOT NULL,       -- Links back to 'releases'
    filename TEXT NOT NULL,
    size INTEGER NOT NULL,
    file_index INTEGER NOT NULL,    -- The order they appear in the NZB
    is_pars BOOLEAN DEFAULT 0,
	subject TEXT,
    date INTEGER,
    FOREIGN KEY(release_id) REFERENCES releases(id) ON DELETE CASCADE,
    UNIQUE(release_id, filename)
);

-- Map release_files to groups
CREATE TABLE IF NOT EXISTS release_file_groups (
    release_file_id INTEGER NOT NULL,
    group_id INTEGER NOT NULL,
    PRIMARY KEY (release_file_id, group_id),
    FOREIGN KEY(release_file_id) REFERENCES release_files(id) ON DELETE CASCADE,
    FOREIGN KEY(group_id) REFERENCES groups(id) ON DELETE CASCADE
);

-- The "Job"
CREATE TABLE IF NOT EXISTS queue_items (
    id TEXT PRIMARY KEY,                -- KSUID
    release_id TEXT NOT NULL,
    status TEXT NOT NULL,
    out_dir TEXT NOT NULL,              -- Unique path for this specific job attempt
    error TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(release_id) REFERENCES releases(id)
);

-- Trigger to automatically update the updated_at timestamp
CREATE TRIGGER IF NOT EXISTS update_queue_item_timestamp 
AFTER UPDATE ON queue_items
BEGIN
    UPDATE queue_items SET updated_at = CURRENT_TIMESTAMP WHERE id = OLD.id;
END;