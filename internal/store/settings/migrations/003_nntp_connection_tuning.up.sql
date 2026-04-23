ALTER TABLE settings_nntp_servers ADD COLUMN dial_timeout_seconds INTEGER NOT NULL DEFAULT 10;
ALTER TABLE settings_nntp_servers ADD COLUMN tcp_keepalive_seconds INTEGER NOT NULL DEFAULT 30;
ALTER TABLE settings_nntp_servers ADD COLUMN pool_idle_timeout_seconds INTEGER NOT NULL DEFAULT 120;
ALTER TABLE settings_nntp_servers ADD COLUMN pool_max_age_seconds INTEGER NOT NULL DEFAULT 900;
