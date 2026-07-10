#!/usr/bin/env sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
STATE="$ROOT/.e2e/gonzbnet"
COMPOSE="$ROOT/docker-compose.gonzbnet-e2e.yml"
COMPOSE_PROJECT="gonzbnet-e2e"
BIN="$STATE/gonzb"

usage() {
  echo "usage: $0 {start|bootstrap|configure-pool|smoke|federation-smoke|stop|status|logs|reset}"
}

wait_http() {
  port="$1"
  attempts=0
  until curl -fsS "http://127.0.0.1:$port/healthz" >/dev/null 2>&1; do
    attempts=$((attempts + 1))
    if [ "$attempts" -ge 60 ]; then
      echo "node on port $port did not become healthy" >&2
      return 1
    fi
    sleep 1
  done
}

start_node() {
  name="$1"
  config="$2"
  dir="$STATE/$name"
  mkdir -p "$dir/keys" "$dir/blobs"
  if [ -f "$dir/pid" ] && kill -0 "$(cat "$dir/pid")" 2>/dev/null; then
    return
  fi
  if command -v setsid >/dev/null 2>&1; then
    setsid "$BIN" serve --config "$config" </dev/null >"$dir/stdout.log" 2>&1 &
  else
    nohup "$BIN" serve --config "$config" </dev/null >"$dir/stdout.log" 2>&1 &
  fi
  echo "$!" >"$dir/pid"
}

stop_nodes() {
  for name in node-a node-b node-c; do
    pidfile="$STATE/$name/pid"
    if [ -f "$pidfile" ]; then
      pid=$(cat "$pidfile")
      kill "$pid" 2>/dev/null || true
      rm -f "$pidfile"
    fi
  done
}

bootstrap_node() {
  name="$1"
  port="$2"
  password="$3"
  dir="$STATE/$name"
  setup_required=$(curl -fsS "http://127.0.0.1:$port/api/v1/auth/setup" | jq -r '.setup_required')
  payload=$(jq -n --arg username admin --arg password "$password" '{username:$username,password:$password}')
  if [ "$setup_required" = "true" ]; then
    response=$(curl -fsS -c "$dir/cookies.txt" -H 'Content-Type: application/json' -d "$payload" "http://127.0.0.1:$port/api/v1/auth/setup")
  else
    response=$(curl -fsS -c "$dir/cookies.txt" -H 'Content-Type: application/json' -d "$payload" "http://127.0.0.1:$port/api/v1/auth/session")
  fi
  echo "$response" | jq -r '.session.csrf_token' >"$dir/csrf-token"
  echo "$name admin session ready"
}

admin_request() {
  name="$1"
  port="$2"
  path="$3"
  payload="$4"
  dir="$STATE/$name"
  csrf=$(cat "$dir/csrf-token")
  curl -fsS -b "$dir/cookies.txt" -H "X-CSRF-Token: $csrf" -H 'Content-Type: application/json' -d "$payload" "http://127.0.0.1:$port$path"
}

admin_post() {
  admin_request "$@" >/dev/null
}

admin_put() {
  name="$1"
  port="$2"
  path="$3"
  payload="$4"
  dir="$STATE/$name"
  csrf=$(cat "$dir/csrf-token")
  curl -fsS -X PUT -b "$dir/cookies.txt" -H "X-CSRF-Token: $csrf" \
    -H 'Content-Type: application/json' -d "$payload" \
    "http://127.0.0.1:$port$path" >/dev/null
}

db_scalar() {
  database="$1"
  query="$2"
  docker compose -p "$COMPOSE_PROJECT" -f "$COMPOSE" exec -T postgres \
    psql -U gonzb -d "$database" -Atc "$query"
}

configure_pool() {
  node_a=$(curl -fsS http://127.0.0.1:18081/gonzbnet/v1/node | jq -r '.node_id')
  node_b=$(curl -fsS http://127.0.0.1:18082/gonzbnet/v1/node | jq -r '.node_id')
  node_c=$(curl -fsS http://127.0.0.1:18083/gonzbnet/v1/node | jq -r '.node_id')
  pool=$(jq -n '{pool_id:"pool.e2e",display_name:"GoNZBNet E2E",description:"Local three-node federation test pool",membership_threshold:1,moderation_threshold:1,checkpoint_witness_threshold:1,accept_mode:"pool_member",min_node_trust_score:0,enabled:true}')
  member_a=$(jq -n --arg id "$node_a" '{node_id:$id,role:"admin",status:"active",allowed_capabilities:["admin","scanner","indexer","manifest_builder","manifest_cache","coverage","coverage_coordinator","scheduler","consumer"],limits:{}}')
  member_b=$(jq -n --arg id "$node_b" '{node_id:$id,role:"member",status:"active",allowed_capabilities:["validator","health_checker","manifest_cache","consumer"],limits:{}}')
  member_c=$(jq -n --arg id "$node_c" '{node_id:$id,role:"member",status:"active",allowed_capabilities:["consumer","manifest_cache","relay"],limits:{}}')
  role_access='{"role_id":"admin","can_search":true,"can_get":true,"can_resolve_manifest":true}'
  for spec in "node-a:18081" "node-b:18082" "node-c:18083"; do
    name=${spec%:*}
    port=${spec#*:}
    admin_post "$name" "$port" /api/v1/admin/gonzbnet/pools "$pool"
    admin_post "$name" "$port" /api/v1/admin/gonzbnet/pools/pool.e2e/members "$member_a"
    admin_post "$name" "$port" /api/v1/admin/gonzbnet/pools/pool.e2e/members "$member_b"
    admin_post "$name" "$port" /api/v1/admin/gonzbnet/pools/pool.e2e/members "$member_c"
    admin_post "$name" "$port" /api/v1/admin/gonzbnet/pools/pool.e2e/role-access "$role_access"
    echo "$name pool.e2e configured"
  done
  for spec in "node-a:18081" "node-b:18082" "node-c:18083"; do
    name=${spec%:*}
    port=${spec#*:}
    admin_post "$name" "$port" /api/v1/admin/gonzbnet/sync/push '{}'
    admin_post "$name" "$port" /api/v1/admin/gonzbnet/sync/pull '{}'
  done
  echo "initial push/pull synchronization complete"
}

federation_smoke() {
  for name in node-a node-b node-c; do
    test -s "$STATE/$name/csrf-token" || {
      echo "run bootstrap before federation-smoke" >&2
      return 1
    }
  done

  unsigned_status=$(curl -sS -o /dev/null -w '%{http_code}' "http://127.0.0.1:18081/gonzbnet/v1/outbox?limit=1")
  case "$unsigned_status" in 401|403) ;; *) echo "unsigned outbox read returned HTTP $unsigned_status" >&2; return 1;; esac

  foreign_session_status=$(curl -sS -o /dev/null -w '%{http_code}' \
    -b "$STATE/node-a/cookies.txt" "http://127.0.0.1:18082/api/v1/admin/gonzbnet/pools")
  case "$foreign_session_status" in 401|403) ;; *) echo "node A session returned HTTP $foreign_session_status on node B" >&2; return 1;; esac

  target_id="rel_e2e_$(date +%s)"
  payload=$(jq -n --arg target_id "$target_id" '{target_type:"release",target_id:$target_id,pool_id:"pool.e2e",reason:"three-node propagation probe",severity:"reject"}')
  event_id=$(admin_request node-a 18081 /api/v1/admin/gonzbnet/moderation/tombstones "$payload" | jq -r '.event_id')
  test -n "$event_id" && test "$event_id" != "null"

  admin_post node-a 18081 /api/v1/admin/gonzbnet/sync/push '{}'
  admin_post node-b 18082 /api/v1/admin/gonzbnet/sync/pull '{}'
  admin_post node-c 18083 /api/v1/admin/gonzbnet/sync/pull '{}'

  for database in gonzbnet_a gonzbnet_b gonzbnet_c; do
    attempts=0
    count=0
    while [ "$attempts" -lt 20 ]; do
      count=$(db_scalar "$database" "SELECT count(*) FROM federation_events WHERE event_id = '$event_id' AND event_type = 'Tombstone' AND validation_status = 'accepted'")
      [ "$count" = "1" ] && break
      attempts=$((attempts + 1))
      sleep 1
    done
    [ "$count" = "1" ] || { echo "$event_id did not reach $database" >&2; return 1; }
    projected=$(db_scalar "$database" "SELECT count(*) FROM tombstones WHERE source_event_id = '$event_id'")
    [ "$projected" = "1" ] || { echo "$event_id was not projected in $database" >&2; return 1; }
  done

  admin_post node-a 18081 /api/v1/admin/gonzbnet/sync/push '{}'
  for database in gonzbnet_a gonzbnet_b gonzbnet_c; do
    count=$(db_scalar "$database" "SELECT count(*) FROM federation_events WHERE event_id = '$event_id'")
    [ "$count" = "1" ] || { echo "$event_id was appended more than once in $database" >&2; return 1; }
  done
  echo "signed event propagated exactly once: $event_id"
  echo "unsigned federation reads and cross-node local sessions were rejected"
}

case "${1:-}" in
  start)
    mkdir -p "$STATE"
    docker compose -p "$COMPOSE_PROJECT" -f "$COMPOSE" up -d --wait
    (cd "$ROOT" && GOCACHE="${GOCACHE:-/tmp/gocache}" go build -o "$BIN" ./cmd/gonzb)
    cd "$ROOT"
    start_node node-a "$ROOT/test/e2e/gonzbnet/node-a.yaml"
    start_node node-b "$ROOT/test/e2e/gonzbnet/node-b.yaml"
    start_node node-c "$ROOT/test/e2e/gonzbnet/node-c.yaml"
    wait_http 18081
    wait_http 18082
    wait_http 18083
    sleep 1
    for name in node-a node-b node-c; do
      kill -0 "$(cat "$STATE/$name/pid")" 2>/dev/null || {
        echo "$name exited after its health check; inspect $STATE/$name/stdout.log" >&2
        exit 1
      }
    done
    echo "GoNZBNet E2E nodes are ready: http://127.0.0.1:18081, :18082, :18083"
    ;;
  bootstrap)
    command -v jq >/dev/null 2>&1 || { echo "jq is required" >&2; exit 1; }
    password="${GONZBNET_E2E_PASSWORD:-gonzb-e2e-local}"
    bootstrap_node node-a 18081 "$password"
    bootstrap_node node-b 18082 "$password"
    bootstrap_node node-c 18083 "$password"
    aggregator='{"aggregator":{"sources":{"local_blob":{"enabled":false},"usenet_indexer":{"enabled":false},"gonzbnet":{"enabled":true}}}}'
    admin_put node-a 18081 /api/v1/admin/settings "$aggregator"
    admin_put node-b 18082 /api/v1/admin/settings "$aggregator"
    admin_put node-c 18083 /api/v1/admin/settings "$aggregator"
    echo "GoNZBNet aggregator source enabled on all nodes"
    echo "Local admin password: $password"
    ;;
  configure-pool)
    command -v jq >/dev/null 2>&1 || { echo "jq is required" >&2; exit 1; }
    configure_pool
    ;;
  smoke)
    ids=""
    for port in 18081 18082 18083; do
      curl -fsS "http://127.0.0.1:$port/.well-known/gonzbnet" | jq -e '.spec_version == "gonzbnet/1.0"' >/dev/null
      node_id=$(curl -fsS "http://127.0.0.1:$port/gonzbnet/v1/node" | jq -r '.node_id')
      test -n "$node_id"
      case " $ids " in *" $node_id "*) echo "duplicate node identity: $node_id" >&2; exit 1;; esac
      ids="$ids $node_id"
      curl -fsS "http://127.0.0.1:$port/gonzbnet/v1/caps" | jq -e '.spec_versions | index("gonzbnet/1.0") != null' >/dev/null
      echo "port=$port node_id=$node_id"
    done
    ;;
  federation-smoke)
    command -v jq >/dev/null 2>&1 || { echo "jq is required" >&2; exit 1; }
    federation_smoke
    ;;
  stop)
    stop_nodes
    docker compose -p "$COMPOSE_PROJECT" -f "$COMPOSE" down
    ;;
  status)
    for port in 18081 18082 18083; do
      curl -fsS "http://127.0.0.1:$port/healthz" || true
      echo
    done
    docker compose -p "$COMPOSE_PROJECT" -f "$COMPOSE" ps
    ;;
  logs)
    tail -n 100 -F "$STATE/node-a/stdout.log" "$STATE/node-b/stdout.log" "$STATE/node-c/stdout.log"
    ;;
  reset)
    stop_nodes
    docker compose -p "$COMPOSE_PROJECT" -f "$COMPOSE" down -v
    rm -rf "$STATE"
    ;;
  *)
    usage
    exit 2
    ;;
esac
