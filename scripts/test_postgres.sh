#!/usr/bin/env bash
set -euo pipefail

container_name="gonzb-postgres-test-$$"
port="${GONZB_TEST_PG_PORT:-55433}"
database="gonzb_local_test"
dsn="postgres://gonzb_test:gonzb_test@127.0.0.1:${port}/${database}?sslmode=disable"

cleanup() {
    docker rm -f "${container_name}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

docker run --detach --rm \
    --name "${container_name}" \
    --publish "127.0.0.1:${port}:5432" \
    --env POSTGRES_DB="${database}" \
    --env POSTGRES_USER=gonzb_test \
    --env POSTGRES_PASSWORD=gonzb_test \
    --env POSTGRES_INITDB_ARGS=--data-checksums \
    --tmpfs /var/lib/postgresql/data:rw,nosuid,size=4g \
    postgres:17.10 \
    -c shared_preload_libraries=pg_stat_statements \
    -c track_io_timing=on >/dev/null

for _ in $(seq 1 30); do
    if docker exec "${container_name}" pg_isready -U gonzb_test -d "${database}" >/dev/null 2>&1; then
        GONZB_REQUIRE_TEST_PG=1 \
        GONZB_TEST_PG_DSN="${dsn}" \
        GONZB_QUERY_SOAK="${GONZB_QUERY_SOAK:-1}" \
        ./scripts/test_ci.sh
        exit
    fi
    sleep 1
done

echo "disposable PostgreSQL did not become ready" >&2
exit 1
