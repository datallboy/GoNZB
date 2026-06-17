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

UPDATE settings_nntp_servers AS shared
SET
  username = CASE
    WHEN shared.username = '' THEN scoped.username
    ELSE shared.username
  END,
  password_ciphertext = CASE
    WHEN shared.password_ciphertext = '' THEN scoped.password_ciphertext
    ELSE shared.password_ciphertext
  END,
  port = CASE
    WHEN shared.port = 0 THEN scoped.port
    ELSE shared.port
  END,
  tls = CASE
    WHEN shared.tls = 0 THEN scoped.tls
    ELSE shared.tls
  END,
  max_connections = CASE
    WHEN shared.max_connections = 0 THEN scoped.max_connections
    ELSE shared.max_connections
  END,
  dial_timeout_seconds = CASE
    WHEN shared.dial_timeout_seconds = 0 THEN scoped.dial_timeout_seconds
    ELSE shared.dial_timeout_seconds
  END,
  tcp_keepalive_seconds = CASE
    WHEN shared.tcp_keepalive_seconds = 0 THEN scoped.tcp_keepalive_seconds
    ELSE shared.tcp_keepalive_seconds
  END,
  pool_idle_timeout_seconds = CASE
    WHEN shared.pool_idle_timeout_seconds = 0 THEN scoped.pool_idle_timeout_seconds
    ELSE shared.pool_idle_timeout_seconds
  END,
  pool_max_age_seconds = CASE
    WHEN shared.pool_max_age_seconds = 0 THEN scoped.pool_max_age_seconds
    ELSE shared.pool_max_age_seconds
  END,
  updated_at = CURRENT_TIMESTAMP
FROM (
  SELECT
    CASE
      WHEN scope = 'downloader' AND id LIKE 'downloader:%' THEN substr(id, length('downloader:') + 1)
      WHEN scope = 'indexer' AND id LIKE 'indexer:%' THEN substr(id, length('indexer:') + 1)
      ELSE id
    END AS shared_id,
    username,
    password_ciphertext,
    port,
    tls,
    max_connections,
    dial_timeout_seconds,
    tcp_keepalive_seconds,
    pool_idle_timeout_seconds,
    pool_max_age_seconds
  FROM settings_nntp_servers
  WHERE scope <> 'shared'
    AND (username <> '' OR password_ciphertext <> '')
  ORDER BY CASE scope WHEN 'downloader' THEN 0 WHEN 'indexer' THEN 1 ELSE 2 END
) AS scoped
WHERE shared.scope = 'shared'
  AND shared.id = scoped.shared_id
  AND (
    shared.username = '' OR
    shared.password_ciphertext = '' OR
    shared.port = 0 OR
    shared.max_connections = 0
  );

DELETE FROM settings_nntp_servers
WHERE scope <> 'shared';
