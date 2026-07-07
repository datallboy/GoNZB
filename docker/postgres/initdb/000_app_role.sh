#!/usr/bin/env bash
set -euo pipefail

app_user="${POSTGRES_APP_USER:-gonzb}"
app_password="${POSTGRES_APP_PASSWORD:-gonzb}"
app_db="${POSTGRES_DB:-gonzb}"

if [[ ! "$app_user" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]]; then
  echo "POSTGRES_APP_USER must be a simple PostgreSQL identifier" >&2
  exit 1
fi

if [[ ! "$app_db" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]]; then
  echo "POSTGRES_DB must be a simple PostgreSQL identifier when using the GoNZB app role init script" >&2
  exit 1
fi

escaped_app_password="${app_password//\'/\'\'}"

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$app_db" \
  -v app_user="$app_user" <<EOSQL
DO \$\$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '$app_user') THEN
    CREATE ROLE $app_user
      LOGIN
      PASSWORD '$escaped_app_password'
      NOSUPERUSER
      NOCREATEDB
      NOCREATEROLE
      NOREPLICATION
      NOBYPASSRLS;
  END IF;
END
\$\$;

ALTER DATABASE $app_db OWNER TO $app_user;
ALTER SCHEMA public OWNER TO $app_user;
GRANT CONNECT, TEMPORARY ON DATABASE $app_db TO $app_user;
GRANT USAGE, CREATE ON SCHEMA public TO $app_user;
EOSQL
