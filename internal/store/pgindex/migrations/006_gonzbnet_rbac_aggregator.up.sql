CREATE TABLE IF NOT EXISTS role_federation_pool_access (
  role_id TEXT NOT NULL,
  pool_id TEXT NOT NULL,
  can_search BOOLEAN NOT NULL DEFAULT TRUE,
  can_get BOOLEAN NOT NULL DEFAULT TRUE,
  can_resolve_manifest BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY(role_id, pool_id)
);

CREATE INDEX IF NOT EXISTS idx_role_federation_pool_access_search
  ON role_federation_pool_access(role_id, pool_id)
  WHERE can_search = TRUE;

CREATE TABLE IF NOT EXISTS user_federation_pool_access (
  user_id TEXT NOT NULL,
  pool_id TEXT NOT NULL,
  can_search BOOLEAN NOT NULL DEFAULT TRUE,
  can_get BOOLEAN NOT NULL DEFAULT TRUE,
  can_resolve_manifest BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY(user_id, pool_id)
);

CREATE INDEX IF NOT EXISTS idx_user_federation_pool_access_search
  ON user_federation_pool_access(user_id, pool_id)
  WHERE can_search = TRUE;
