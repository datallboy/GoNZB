CREATE TABLE IF NOT EXISTS releases (
		id TEXT PRIMARY KEY,
		title TEXT,
		source TEXT,
		download_url TEXT,
		size INTEGER,
		category TEXT,
		redirect_allowed INTEGER, -- 0 for false, 1 for true
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);