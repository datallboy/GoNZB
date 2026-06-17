INSERT OR IGNORE INTO settings_nntp_servers (
  id,
  host,
  port,
  username,
  password_ciphertext,
  tls,
  max_connections,
  priority,
  updated_at,
  dial_timeout_seconds,
  tcp_keepalive_seconds,
  pool_idle_timeout_seconds,
  pool_max_age_seconds,
  enable_pool_logging,
  scope
)
SELECT
  CASE
    WHEN scope = 'downloader' AND id LIKE 'downloader:%' THEN substr(id, length('downloader:') + 1)
    WHEN scope = 'indexer' AND id LIKE 'indexer:%' THEN substr(id, length('indexer:') + 1)
    ELSE id
  END AS shared_id,
  host,
  port,
  username,
  password_ciphertext,
  tls,
  max_connections,
  priority,
  updated_at,
  dial_timeout_seconds,
  tcp_keepalive_seconds,
  pool_idle_timeout_seconds,
  pool_max_age_seconds,
  enable_pool_logging,
  'shared'
FROM settings_nntp_servers
WHERE scope <> 'shared';

DELETE FROM settings_nntp_servers
WHERE scope <> 'shared';
