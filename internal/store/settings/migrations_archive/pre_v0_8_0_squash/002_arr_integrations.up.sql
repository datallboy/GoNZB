CREATE TABLE IF NOT EXISTS settings_arr_integrations (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL DEFAULT '',                    -- radarr | sonarr
  enabled BOOLEAN NOT NULL DEFAULT 0,
  base_url TEXT NOT NULL DEFAULT '',
  api_key_ciphertext TEXT NOT NULL DEFAULT '',
  client_name TEXT NOT NULL DEFAULT '',
  category TEXT NOT NULL DEFAULT '',
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);