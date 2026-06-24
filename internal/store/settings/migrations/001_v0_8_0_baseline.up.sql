-- v0.8.0 settings SQLite baseline generated from migrations 001-008.
-- module_schema_version is created by the shared SQLite migrator.

CREATE TABLE settings_revision (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  payload_json TEXT NOT NULL DEFAULT '{}',
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE settings_nntp_servers (
  id TEXT PRIMARY KEY,
  host TEXT NOT NULL DEFAULT '',
  port INTEGER NOT NULL DEFAULT 0,
  username TEXT NOT NULL DEFAULT '',
  password_ciphertext TEXT NOT NULL DEFAULT '',
  tls BOOLEAN NOT NULL DEFAULT 0,
  max_connections INTEGER NOT NULL DEFAULT 0,
  priority INTEGER NOT NULL DEFAULT 0,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  dial_timeout_seconds INTEGER NOT NULL DEFAULT 10,
  tcp_keepalive_seconds INTEGER NOT NULL DEFAULT 30,
  pool_idle_timeout_seconds INTEGER NOT NULL DEFAULT 45,
  pool_max_age_seconds INTEGER NOT NULL DEFAULT 600,
  enable_pool_logging BOOLEAN NOT NULL DEFAULT 0,
  scope TEXT NOT NULL DEFAULT 'shared'
);
CREATE TABLE settings_indexers (
  id TEXT PRIMARY KEY,
  base_url TEXT NOT NULL DEFAULT '',
  api_path TEXT NOT NULL DEFAULT '',
  api_key_ciphertext TEXT NOT NULL DEFAULT '',
  redirect BOOLEAN NOT NULL DEFAULT 0,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE settings_download (
  singleton_id INTEGER PRIMARY KEY CHECK (singleton_id = 1),
  out_dir TEXT NOT NULL DEFAULT '',
  completed_dir TEXT NOT NULL DEFAULT '',
  cleanup_extensions_json TEXT NOT NULL DEFAULT '[]',
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE settings_module_options (
  module_name TEXT PRIMARY KEY,
  options_json TEXT NOT NULL DEFAULT '{}',
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE settings_arr_integrations (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL DEFAULT '',                    -- radarr | sonarr
  enabled BOOLEAN NOT NULL DEFAULT 0,
  base_url TEXT NOT NULL DEFAULT '',
  api_key_ciphertext TEXT NOT NULL DEFAULT '',
  client_name TEXT NOT NULL DEFAULT '',
  category TEXT NOT NULL DEFAULT '',
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE auth_users (
  id TEXT PRIMARY KEY,
  username TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL DEFAULT '',
  enabled BOOLEAN NOT NULL DEFAULT 1,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE auth_roles (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  builtin BOOLEAN NOT NULL DEFAULT 0,
  permissions_json TEXT NOT NULL DEFAULT '[]',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE auth_user_roles (
  user_id TEXT NOT NULL,
  role_id TEXT NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (user_id, role_id),
  FOREIGN KEY (user_id) REFERENCES auth_users(id) ON DELETE CASCADE,
  FOREIGN KEY (role_id) REFERENCES auth_roles(id) ON DELETE CASCADE
);
CREATE TABLE auth_sessions (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL,
  expires_at DATETIME NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  last_seen_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (user_id) REFERENCES auth_users(id) ON DELETE CASCADE
);
CREATE TABLE auth_api_tokens (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL,
  name TEXT NOT NULL DEFAULT '',
  prefix TEXT NOT NULL DEFAULT '',
  token_hash TEXT NOT NULL UNIQUE,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  last_used_at DATETIME,
  revoked_at DATETIME,
  FOREIGN KEY (user_id) REFERENCES auth_users(id) ON DELETE CASCADE
);
