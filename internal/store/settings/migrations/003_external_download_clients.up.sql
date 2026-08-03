DROP TABLE IF EXISTS settings_arr_integrations;
DROP TABLE IF EXISTS settings_download;

CREATE TABLE settings_download_clients (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL DEFAULT '',
  enabled BOOLEAN NOT NULL DEFAULT 1,
  is_default BOOLEAN NOT NULL DEFAULT 0,
  base_url TEXT NOT NULL DEFAULT '',
  api_key_ciphertext TEXT NOT NULL DEFAULT '',
  category TEXT NOT NULL DEFAULT '',
  priority INTEGER NOT NULL DEFAULT -100,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX idx_settings_download_clients_one_default
ON settings_download_clients(is_default)
WHERE is_default = 1 AND enabled = 1;
