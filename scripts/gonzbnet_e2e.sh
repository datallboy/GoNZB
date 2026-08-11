#!/usr/bin/env sh
# TEST FIXTURE ONLY. All state and generated credentials stay under .e2e/.
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
STATE="$ROOT/.e2e/gonzbnet"
COMPOSE="$ROOT/docker-compose.gonzbnet-e2e.yml"
COMPOSE_PROJECT="gonzbnet-e2e"
BIN="$STATE/gonzb"
NNTP_BIN="$STATE/nntpfixture"
TLS_PROXY_BIN="$STATE/tlsproxy"
TLS_DIR="$STATE/tls"
TLS_CA="$TLS_DIR/ca.pem"
NODE_A_CONFIG=${GONZBNET_NODE_A_CONFIG:-$ROOT/test/e2e/gonzbnet/node-a.yaml}
NODE_B_CONFIG=${GONZBNET_NODE_B_CONFIG:-$ROOT/test/e2e/gonzbnet/node-b.yaml}
NODE_C_CONFIG=${GONZBNET_NODE_C_CONFIG:-$ROOT/test/e2e/gonzbnet/node-c.yaml}
NODE_D_CONFIG=${GONZBNET_NODE_D_CONFIG:-$ROOT/test/e2e/gonzbnet/node-d.yaml}

usage() {
  echo "usage: $0 {test|start|bootstrap|configure-pool|seed-traversal-peers|admission-smoke|quorum-smoke|smoke|federation-smoke|release-smoke|indexer-federation-smoke|nntp-smoke|observability-smoke|stop|status|logs|reset}"
}

direct_peer_url() {
  internal_port="$1"
  echo "https://localhost:$((internal_port + 400))"
}

peer_url() {
  internal_port="$1"
  if [ "${GONZBNET_E2E_TRANSPORT:-https}" = "traversal" ]; then
    coordinator_host=${GONZBNET_E2E_TRAVERSAL_HOST:?GONZBNET_E2E_TRAVERSAL_HOST is required for traversal}
    node_id=$(curl -fsS "http://127.0.0.1:$internal_port/gonzbnet/v1/node" | jq -er '.node_id')
    echo "gonzb+ice://$node_id@$coordinator_host/gonzbnet/v1"
    return
  fi
  direct_peer_url "$internal_port"
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

wait_https() {
  port="$1"
  attempts=0
  until curl -fsS --cacert "$TLS_CA" "https://localhost:$port/healthz" >/dev/null 2>&1; do
    attempts=$((attempts + 1))
    if [ "$attempts" -ge 60 ]; then
      echo "HTTPS proxy on port $port did not become healthy" >&2
      return 1
    fi
    sleep 1
  done
}

start_node() {
  name="$1"
  config="$2"
  dir="$STATE/$name"
  cert_file="$TLS_CA"
  if [ "${GONZBNET_E2E_TRANSPORT:-https}" = "traversal" ]; then
    cert_file=${GONZBNET_E2E_SYSTEM_CA_FILE:-/etc/ssl/certs/ca-certificates.crt}
  fi
  mkdir -p "$dir/keys" "$dir/blobs"
  if [ -f "$dir/pid" ] && kill -0 "$(cat "$dir/pid")" 2>/dev/null; then
    return
  fi
  if command -v setsid >/dev/null 2>&1; then
    setsid env SSL_CERT_FILE="$cert_file" "$BIN" serve --config "$config" </dev/null >"$dir/stdout.log" 2>&1 &
  else
    nohup env SSL_CERT_FILE="$cert_file" "$BIN" serve --config "$config" </dev/null >"$dir/stdout.log" 2>&1 &
  fi
  echo "$!" >"$dir/pid"
}

stop_nodes() {
  for name in node-a node-b node-c node-d; do
    pidfile="$STATE/$name/pid"
    if [ -f "$pidfile" ]; then
      pid=$(cat "$pidfile")
      kill "$pid" 2>/dev/null || true
      rm -f "$pidfile"
    fi
  done
}

stop_fixtures() {
  if [ -f "$STATE/nntpfixture.pid" ]; then
    kill "$(cat "$STATE/nntpfixture.pid")" 2>/dev/null || true
    rm -f "$STATE/nntpfixture.pid"
  fi
  for name in node-a node-b node-c node-d; do
    pidfile="$STATE/tls-$name.pid"
    if [ -f "$pidfile" ]; then
      kill "$(cat "$pidfile")" 2>/dev/null || true
      rm -f "$pidfile"
    fi
  done
}

stop_all() {
  stop_nodes
  stop_fixtures
}

cleanup_test_run() {
  result=$?
  trap - 0 1 2 15
  if [ "$result" -eq 0 ] || [ "${GONZBNET_E2E_KEEP_STATE_ON_FAILURE:-}" != "1" ]; then
    "$0" reset >/dev/null 2>&1 || true
  else
    stop_all
  fi
  exit "$result"
}

start_tls_proxies() {
  (cd "$ROOT" && GOCACHE="${GOCACHE:-/tmp/gocache}" go build -o "$TLS_PROXY_BIN" ./test/e2e/gonzbnet/tlsproxy)
  "$TLS_PROXY_BIN" -generate-dir "$TLS_DIR"
  for spec in "node-a:18081:18481" "node-b:18082:18482" "node-c:18083:18483" "node-d:18084:18484"; do
    name=${spec%%:*}
    remainder=${spec#*:}
    target_port=${remainder%%:*}
    listen_port=${remainder#*:}
    logfile="$STATE/$name/tls-proxy.log"
    mkdir -p "$STATE/$name"
    if command -v setsid >/dev/null 2>&1; then
      setsid "$TLS_PROXY_BIN" -listen "127.0.0.1:$listen_port" \
        -target "http://127.0.0.1:$target_port" \
        -cert "$TLS_DIR/server.pem" -key "$TLS_DIR/server-key.pem" \
        </dev/null >"$logfile" 2>&1 &
    else
      nohup "$TLS_PROXY_BIN" -listen "127.0.0.1:$listen_port" \
        -target "http://127.0.0.1:$target_port" \
        -cert "$TLS_DIR/server.pem" -key "$TLS_DIR/server-key.pem" \
        </dev/null >"$logfile" 2>&1 &
    fi
    echo "$!" >"$STATE/tls-$name.pid"
  done
}

start_nntp_fixture() {
  if [ -f "$STATE/nntpfixture.pid" ] && kill -0 "$(cat "$STATE/nntpfixture.pid")" 2>/dev/null; then
    return
  fi
  (cd "$ROOT" && GOCACHE="${GOCACHE:-/tmp/gocache}" go build -o "$NNTP_BIN" ./test/e2e/gonzbnet/nntpfixture)
  if command -v setsid >/dev/null 2>&1; then
    setsid "$NNTP_BIN" -listen 127.0.0.1:11119 </dev/null >"$STATE/nntpfixture.log" 2>&1 &
  else
    nohup "$NNTP_BIN" -listen 127.0.0.1:11119 </dev/null >"$STATE/nntpfixture.log" 2>&1 &
  fi
  echo "$!" >"$STATE/nntpfixture.pid"
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
  attempts=0
  until admin_request "$@" >/dev/null; do
    attempts=$((attempts + 1))
    if [ "$attempts" -ge 3 ]; then
      return 1
    fi
    sleep 1
  done
}

admin_get() {
  name="$1"
  port="$2"
  path="$3"
  dir="$STATE/$name"
  curl -fsS -b "$dir/cookies.txt" "http://127.0.0.1:$port$path"
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

db_exec() {
  database="$1"
  query="$2"
  docker compose -p "$COMPOSE_PROJECT" -f "$COMPOSE" exec -T postgres \
    psql -v ON_ERROR_STOP=1 -U gonzb -d "$database" -c "$query" >/dev/null
}

run_indexer_stage() {
  stage="$1"
  before=$(db_scalar gonzbnet_a "SELECT COALESCE(MAX(id), 0) FROM indexer_stage_runs WHERE stage_name = '$stage'")
  admin_post node-a 18081 "/api/v1/admin/indexer/stages/$stage/actions/run" '{}'
  attempts=0
  while [ "$attempts" -lt 180 ]; do
    row=$(db_scalar gonzbnet_a "
      SELECT status || '|' || COALESCE(error_text, '')
      FROM indexer_stage_runs
      WHERE stage_name = '$stage'
        AND trigger_kind = 'manual'
        AND id > $before
      ORDER BY id DESC
      LIMIT 1")
    case "$row" in
      completed\|*)
        echo "indexer stage completed: $stage"
        return 0
        ;;
      failed\|*)
        echo "indexer stage failed: $stage: ${row#*|}" >&2
        return 1
        ;;
    esac
    attempts=$((attempts + 1))
    if [ $((attempts % 15)) -eq 0 ] && [ -z "$row" ]; then
      admin_post node-a 18081 "/api/v1/admin/indexer/stages/$stage/actions/run" '{}'
    fi
    sleep 1
  done
  echo "timed out waiting for indexer stage: $stage" >&2
  return 1
}

wait_db_at_least() {
  database="$1"
  expected="$2"
  description="$3"
  query="$4"
  attempts=0
  actual=0
  while [ "$attempts" -lt 180 ]; do
    actual=$(db_scalar "$database" "$query")
    [ "$actual" -ge "$expected" ] && return 0
    attempts=$((attempts + 1))
    sleep 1
  done
  echo "$description was $actual, expected at least $expected" >&2
  return 1
}

configure_pool() {
  pool=$(jq -n '{pool_id:"pool.e2e",display_name:"GoNZBNet E2E",description:"Four-node admission test pool",membership_threshold:1,moderation_threshold:1,checkpoint_witness_threshold:1,accept_mode:"pool_member",min_node_trust_score:0,visibility:"unlisted",join_mode:"approval",admission_enabled:true,enabled:true}')
  admin_post node-a 18081 /api/v1/admin/gonzbnet/pools "$pool"
  join_pool node-b 18082 "$(peer_url 18081)" pool.e2e node-a 18081
  join_pool node-c 18083 "$(peer_url 18081)" pool.e2e node-a 18081
  join_pool node-d 18084 "$(peer_url 18082)" pool.e2e node-a 18081

  pool_two=$(jq -n '{pool_id:"pool.side",display_name:"Side Pool",description:"C and D isolation test",membership_threshold:1,moderation_threshold:1,checkpoint_witness_threshold:1,accept_mode:"pool_member",min_node_trust_score:0,visibility:"private",join_mode:"approval",admission_enabled:true,enabled:true}')
  admin_post node-d 18084 /api/v1/admin/gonzbnet/pools "$pool_two"
  invitation=$(admin_request node-d 18084 /api/v1/admin/gonzbnet/pools/pool.side/invitations '{}' | jq -r '.link')
  test -n "$invitation" && test "$invitation" != "null"
  join_pool node-c 18083 "$invitation" pool.side node-d 18084 '["consumer"]'

  seed_traversal_peers

  role_access='{"role_id":"admin","can_search":true,"can_get":true,"can_resolve_manifest":true}'
  for spec in "node-a:18081" "node-b:18082" "node-c:18083" "node-d:18084"; do
    name=${spec%:*}
    port=${spec#*:}
    admin_post "$name" "$port" /api/v1/admin/gonzbnet/pools/pool.e2e/role-access "$role_access"
  done
  for spec in "node-a:18081" "node-b:18082" "node-c:18083" "node-d:18084"; do
    name=${spec%:*}
    port=${spec#*:}
    admin_post "$name" "$port" /api/v1/admin/gonzbnet/sync/push '{}'
    admin_post "$name" "$port" /api/v1/admin/gonzbnet/sync/pull '{}'
  done
  echo "initial push/pull synchronization complete"
}

join_pool() {
  candidate="$1"
  candidate_port="$2"
  locator="$3"
  pool_id="$4"
  admin="$5"
  admin_port="$6"
  requested_capabilities="${7:-}"
  if [ -n "$requested_capabilities" ]; then
    payload=$(jq -n --arg locator "$locator" --arg pool "$pool_id" --argjson capabilities "$requested_capabilities" \
      '{locator:$locator,pool_id:$pool,requested_capabilities:$capabilities}')
  else
    payload=$(jq -n --arg locator "$locator" --arg pool "$pool_id" '{locator:$locator,pool_id:$pool}')
  fi
  proposal=$(admin_request "$candidate" "$candidate_port" /api/v1/admin/gonzbnet/admission/join "$payload" | jq -r '.proposal_event_id')
  test -n "$proposal" && test "$proposal" != "null"
  duplicate_proposal=$(admin_request "$candidate" "$candidate_port" /api/v1/admin/gonzbnet/admission/join "$payload" | jq -r '.proposal_event_id')
  [ "$duplicate_proposal" = "$proposal" ] || { echo "duplicate join created a second proposal" >&2; return 1; }

  # A non-admin relay distributes the candidate event before the administrator signs it.
  if [ "$locator" = "$(peer_url 18082)" ]; then
    admin_post node-b 18082 /api/v1/admin/gonzbnet/sync/push '{}'
    admin_post "$admin" "$admin_port" /api/v1/admin/gonzbnet/sync/pull '{}'
  fi
  approval_event=$(admin_request "$admin" "$admin_port" "/api/v1/admin/gonzbnet/admissions/$proposal/approve" '{}' | jq -r '.approval_event.event_id')
  duplicate_approval=$(admin_request "$admin" "$admin_port" "/api/v1/admin/gonzbnet/admissions/$proposal/approve" '{}' | jq -r '.approval_event.event_id')
  [ "$duplicate_approval" = "$approval_event" ] || { echo "duplicate approval created a second final event" >&2; return 1; }
  admin_post "$candidate" "$candidate_port" "/api/v1/admin/gonzbnet/admissions/$proposal/refresh" '{}'
  displayed_locator="$locator"
  case "$locator" in
    gonzbnet://*) displayed_locator="signed invitation" ;;
  esac
  echo "$candidate joined $pool_id through $displayed_locator"
}

seed_traversal_peers() {
  [ "${GONZBNET_E2E_TRANSPORT:-https}" = "traversal" ] || return 0
  for source in "node-a:18081" "node-b:18082" "node-c:18083" "node-d:18084"; do
    source_name=${source%:*}
    source_port=${source#*:}
    for target_port in 18081 18082 18083 18084; do
      [ "$source_port" = "$target_port" ] && continue
      locator=$(peer_url "$target_port")
      admin_post "$source_name" "$source_port" /api/v1/admin/gonzbnet/peers \
        "$(jq -cn --arg peer_url "$locator" '{peer_url:$peer_url}')"
    done
  done
  for source in "node-a:18081" "node-b:18082" "node-c:18083" "node-d:18084"; do
    source_name=${source%:*}
    source_port=${source#*:}
    admin_post "$source_name" "$source_port" /api/v1/admin/gonzbnet/sync/pull '{}'
    admin_post "$source_name" "$source_port" /api/v1/admin/gonzbnet/sync/push '{}'
  done
  echo "full-mesh traversal peer locators configured"
}

admission_smoke() {
  node_c=$(curl -fsS "http://127.0.0.1:18083/gonzbnet/v1/node" | jq -r '.node_id')
  node_d=$(curl -fsS "http://127.0.0.1:18084/gonzbnet/v1/node" | jq -r '.node_id')
  expected_approval=""
  for database in gonzbnet_a gonzbnet_b gonzbnet_c gonzbnet_d; do
    count=$(db_scalar "$database" "SELECT count(DISTINCT node_id) FROM pool_members WHERE pool_id = 'pool.e2e' AND status = 'active'")
    [ "$count" = "4" ] || { echo "$database has $count active pool.e2e members, expected 4" >&2; return 1; }
    approval=$(db_scalar "$database" "SELECT approved_event_id FROM pool_members WHERE pool_id = 'pool.e2e' AND node_id = '$node_d' AND role = 'member' AND status = 'active'")
    test -n "$approval" || { echo "$database is missing Node D's approval event" >&2; return 1; }
    if [ -z "$expected_approval" ]; then
      expected_approval="$approval"
    fi
    [ "$approval" = "$expected_approval" ] || { echo "$database projected a different Node D approval event" >&2; return 1; }
  done
  for database in gonzbnet_c gonzbnet_d; do
    count=$(db_scalar "$database" "SELECT count(DISTINCT node_id) FROM pool_members WHERE pool_id = 'pool.side' AND status = 'active'")
    [ "$count" = "2" ] || { echo "$database has $count active pool.side members, expected 2" >&2; return 1; }
  done
  for database in gonzbnet_a gonzbnet_b; do
    count=$(db_scalar "$database" "SELECT count(*) FROM trust_pools WHERE pool_id = 'pool.side'")
    [ "$count" = "0" ] || { echo "$database received isolated pool.side state" >&2; return 1; }
  done
  p1_capabilities=$(db_scalar gonzbnet_c "SELECT allowed_capabilities::text FROM pool_members WHERE pool_id = 'pool.e2e' AND node_id = '$node_c' AND role = 'member' AND status = 'active'")
  p2_capabilities=$(db_scalar gonzbnet_c "SELECT allowed_capabilities::text FROM pool_members WHERE pool_id = 'pool.side' AND node_id = '$node_c' AND role = 'member' AND status = 'active'")
  [ "$p2_capabilities" = '["consumer"]' ] || { echo "Node C did not receive the requested P2 capability grant" >&2; return 1; }
  [ "$p1_capabilities" != "$p2_capabilities" ] || { echo "Node C capability grants are not isolated by pool" >&2; return 1; }
  peers=$(db_scalar gonzbnet_d "SELECT count(*) FROM federation_peers")
  [ "$peers" -ge 1 ] || { echo "node D did not persist its discovered relay" >&2; return 1; }

  ids_before=""
  for port in 18081 18082 18083 18084; do
    ids_before="$ids_before $(curl -fsS "http://127.0.0.1:$port/gonzbnet/v1/node" | jq -r '.node_id')"
  done
  cursor_count_before=$(db_scalar gonzbnet_d "SELECT count(*) FROM federation_peer_cursors")
  stop_nodes
  start_node node-a "$NODE_A_CONFIG"
  start_node node-b "$NODE_B_CONFIG"
  start_node node-c "$NODE_C_CONFIG"
  start_node node-d "$NODE_D_CONFIG"
  for port in 18081 18082 18083 18084; do
    wait_http "$port"
  done
  ids_after=""
  for port in 18081 18082 18083 18084; do
    ids_after="$ids_after $(curl -fsS "http://127.0.0.1:$port/gonzbnet/v1/node" | jq -r '.node_id')"
  done
  [ "$ids_after" = "$ids_before" ] || { echo "node identity changed after restart" >&2; return 1; }
  cursor_count_after=$(db_scalar gonzbnet_d "SELECT count(*) FROM federation_peer_cursors")
  [ "$cursor_count_after" -ge "$cursor_count_before" ] || { echo "Node D lost synchronization cursors after restart" >&2; return 1; }

  revocation=$(jq -n '{reason:"pool isolation verification"}')
  revocation_event=$(admin_request node-d 18084 "/api/v1/admin/gonzbnet/pools/pool.side/members/$node_c/revocations" "$revocation" | jq -r '.event_id')
  test -n "$revocation_event" && test "$revocation_event" != "null"
  admin_post node-d 18084 /api/v1/admin/gonzbnet/sync/push '{}'
  admin_post node-c 18083 /api/v1/admin/gonzbnet/sync/pull '{}'
  for database in gonzbnet_c gonzbnet_d; do
    status=$(db_scalar "$database" "SELECT status FROM pool_members WHERE pool_id = 'pool.side' AND node_id = '$node_c' ORDER BY updated_at DESC LIMIT 1")
    [ "$status" = "revoked" ] || { echo "$database did not project pool.side revocation" >&2; return 1; }
    p1=$(db_scalar "$database" "SELECT count(*) FROM pool_members WHERE pool_id = 'pool.e2e' AND node_id = '$node_c' AND status = 'active'")
    [ "$p1" = "1" ] || { echo "pool.side revocation changed pool.e2e membership in $database" >&2; return 1; }
  done
  for database in gonzbnet_a gonzbnet_b; do
    count=$(db_scalar "$database" "SELECT count(*) FROM federation_events WHERE event_id = '$revocation_event'")
    [ "$count" = "0" ] || { echo "$database received isolated pool.side revocation event" >&2; return 1; }
  done
  echo "four-node admission and two-pool isolation verified"
}

quorum_smoke() {
  for name in node-a node-b node-c; do
    test -s "$STATE/$name/csrf-token" || {
      echo "run bootstrap before quorum-smoke" >&2
      return 1
    }
  done

  pool_one=$(jq -n '{pool_id:"pool.quorum",display_name:"Quorum Pool",description:"Two-admin admission quorum",membership_threshold:1,moderation_threshold:2,checkpoint_witness_threshold:1,accept_mode:"pool_member",min_node_trust_score:0,visibility:"unlisted",join_mode:"approval",admission_enabled:true,enabled:true}')
  admin_post node-a 18081 /api/v1/admin/gonzbnet/pools "$pool_one"

  admin_b_payload=$(jq -n --arg locator "$(peer_url 18081)" '{locator:$locator,pool_id:"pool.quorum",role:"admin"}')
  admin_b_proposal=$(admin_request node-b 18082 /api/v1/admin/gonzbnet/admission/join "$admin_b_payload" | jq -r '.proposal_event_id')
  if [ -z "$admin_b_proposal" ] || [ "$admin_b_proposal" = "null" ]; then
    echo "Node B did not create an admin admission proposal" >&2
    return 1
  fi
  admin_request node-a 18081 "/api/v1/admin/gonzbnet/admissions/$admin_b_proposal/approve" '{}' >/dev/null
  admin_post node-b 18082 "/api/v1/admin/gonzbnet/admissions/$admin_b_proposal/refresh" '{}'
  admin_post node-a 18081 /api/v1/admin/gonzbnet/sync/push '{}'
  admin_post node-b 18082 /api/v1/admin/gonzbnet/sync/pull '{}'

  pool_two=$(jq -n '{pool_id:"pool.quorum",display_name:"Quorum Pool",description:"Two-admin admission quorum",membership_threshold:2,moderation_threshold:2,checkpoint_witness_threshold:1,accept_mode:"pool_member",min_node_trust_score:0,visibility:"unlisted",join_mode:"approval",admission_enabled:true,enabled:true}')
  admin_post node-a 18081 /api/v1/admin/gonzbnet/pools "$pool_two"
  admin_post node-b 18082 /api/v1/admin/gonzbnet/pools "$pool_two"

  candidate_payload=$(jq -n --arg locator "$(peer_url 18081)" '{locator:$locator,pool_id:"pool.quorum",role:"member"}')
  proposal=$(admin_request node-c 18083 /api/v1/admin/gonzbnet/admission/join "$candidate_payload" | jq -r '.proposal_event_id')
  if [ -z "$proposal" ] || [ "$proposal" = "null" ]; then
    echo "Node C did not create a member admission proposal" >&2
    return 1
  fi
  admin_post node-a 18081 /api/v1/admin/gonzbnet/sync/push '{}'
  admin_post node-b 18082 /api/v1/admin/gonzbnet/sync/pull '{}'

  first=$(admin_request node-a 18081 "/api/v1/admin/gonzbnet/admissions/$proposal/approve" '{}')
  echo "$first" | jq -e '.status == "pending" and .approvals == 1 and .approvals_required == 2 and .approval_event == null' >/dev/null || {
    echo "first quorum approval did not remain pending" >&2
    return 1
  }
  second=$(admin_request node-b 18082 "/api/v1/admin/gonzbnet/admissions/$proposal/approve" '{}')
  echo "$second" | jq -e '.status == "approved" and .approvals == 2 and .approvals_required == 2 and .approval_event.event_id != null' >/dev/null || {
    echo "second quorum approval did not finalize admission" >&2
    return 1
  }
  final_event=$(echo "$second" | jq -r '.approval_event.event_id')
  admin_post node-c 18083 "/api/v1/admin/gonzbnet/admissions/$proposal/refresh" '{}'
  admin_post node-a 18081 /api/v1/admin/gonzbnet/sync/push '{}'
  admin_post node-b 18082 /api/v1/admin/gonzbnet/sync/pull '{}'
  admin_post node-c 18083 /api/v1/admin/gonzbnet/sync/pull '{}'

  fragments=$(db_scalar gonzbnet_a "SELECT count(*) FROM federation_pool_approval_fragments WHERE proposal_event_id = '$proposal'")
  [ "$fragments" = "2" ] || { echo "relay stored $fragments approval fragments, expected 2" >&2; return 1; }
  for database in gonzbnet_a gonzbnet_b gonzbnet_c; do
    active=$(db_scalar "$database" "SELECT count(DISTINCT node_id) FROM pool_members WHERE pool_id = 'pool.quorum' AND status = 'active'")
    [ "$active" = "3" ] || { echo "$database has $active active quorum-pool members, expected 3" >&2; return 1; }
    final=$(db_scalar "$database" "SELECT count(*) FROM federation_events WHERE event_id = '$final_event' AND event_type = 'PoolMemberApproved' AND validation_status = 'accepted'")
    [ "$final" = "1" ] || { echo "$database is missing quorum final event $final_event" >&2; return 1; }
  done
  echo "two-admin admission quorum verified: $final_event"
}

nntp_smoke() {
  mkdir -p "$STATE"
  (cd "$ROOT" && GOCACHE="${GOCACHE:-/tmp/gocache}" go build -o "$BIN" ./cmd/gonzb)
  start_nntp_fixture
  attempts=0
  result=""
  until result=$("$BIN" --config "$ROOT/test/e2e/gonzbnet/nntp-client.yaml" gonzbnet nntp-check 2>/dev/null); do
    attempts=$((attempts + 1))
    [ "$attempts" -lt 20 ] || { echo "NNTP fixture did not become ready" >&2; return 1; }
    sleep 1
  done
  echo "$result" | jq -e '.group == "alt.binaries.test" and .count == 4 and .overview_rows == 4 and .body_bytes > 0' >/dev/null || {
    echo "production NNTP client did not read the deterministic fixture" >&2
    return 1
  }
  echo "real NNTP DATE/GROUP/XOVER/BODY path verified"
}

federation_smoke() {
  for name in node-a node-b node-c node-d; do
    test -s "$STATE/$name/csrf-token" || {
      echo "run bootstrap before federation-smoke" >&2
      return 1
    }
  done

  unsigned_status=$(curl -sS --cacert "$TLS_CA" -o /dev/null -w '%{http_code}' "$(direct_peer_url 18081)/gonzbnet/v1/outbox?limit=1")
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
	admin_post node-d 18084 /api/v1/admin/gonzbnet/sync/pull '{}'

  for database in gonzbnet_a gonzbnet_b gonzbnet_c gonzbnet_d; do
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
  for database in gonzbnet_a gonzbnet_b gonzbnet_c gonzbnet_d; do
    count=$(db_scalar "$database" "SELECT count(*) FROM federation_events WHERE event_id = '$event_id'")
    [ "$count" = "1" ] || { echo "$event_id was appended more than once in $database" >&2; return 1; }
  done
  echo "signed event propagated exactly once: $event_id"
  echo "unsigned federation reads and cross-node local sessions were rejected"
}

release_smoke() {
  for name in node-a node-d; do
    test -s "$STATE/$name/csrf-token" || {
      echo "run bootstrap before release-smoke" >&2
      return 1
    }
  done

  scan_id="e2e-release-$(date +%s)"
  release_canary=${GONZBNET_E2E_RELEASE_CANARY:-}
  if [ -n "${GONZBNET_E2E_CAPTURE_SECRET_DIR:-}" ] && [ -z "$release_canary" ]; then
    echo "GONZBNET_E2E_RELEASE_CANARY is required when capture secrets are enabled" >&2
    return 1
  fi
  release_title="GoNZBNet E2E $scan_id"
  if [ -n "$release_canary" ]; then
    release_title="$release_title $release_canary"
  fi
  now=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  fixture=$(jq -cn \
    --arg scan_id "$scan_id" \
    --arg release_title "$release_title" \
    --arg now "$now" \
    '{
      LocalReleaseID:$scan_id,
      GUID:$scan_id,
      Title:$release_title,
      Category:"Movies",
      CategoryID:2000,
      Classification:"movie",
      SizeBytes:2048,
      PostedAt:$now,
      AddedAt:$now,
      FileCount:1,
      CompletionPct:100,
      Groups:["alt.binaries.test"],
      Files:[{
        ID:1,
        Name:"fixture.bin",
        Subject:"GoNZBNet E2E manifest fixture",
        Poster:"e2e@example.invalid",
        PostedAt:$now,
        SizeBytes:2048,
        FileIndex:1,
        IsPars:false,
        ArticleCount:1,
        TotalParts:1,
        Segments:[{
          Number:1,
          Bytes:2048,
          MessageID:("<" + $scan_id + "@example.invalid>")
        }]
      }],
      HasPAR2:false,
      HasNFO:false,
      PasswordState:"none",
      Availability:1
    }')
  db_exec gonzbnet_a "
    INSERT INTO gonzbnet_scan_outputs (scan_id, body_json, status, updated_at)
    VALUES ('$scan_id', '$fixture'::jsonb, 'pending', NOW())
    ON CONFLICT (scan_id) DO UPDATE SET
      body_json = EXCLUDED.body_json,
      status = 'pending',
      updated_at = NOW()"

  attempts=0
  publication=""
  while [ "$attempts" -lt 90 ]; do
    publication=$(db_scalar gonzbnet_a "
      SELECT (card.body_json->>'release_id') || '|' ||
             (card.body_json->>'manifest_id') || '|' || manifest.event_id
      FROM gonzbnet_scan_output_publications publication
      JOIN federation_events card ON card.event_id = publication.event_id
      JOIN resolution_manifests cached
        ON cached.manifest_id = card.body_json->>'manifest_id'
      JOIN federation_events manifest ON manifest.event_id = cached.source_event_id
      WHERE publication.scan_id = '$scan_id'
        AND publication.pool_id = 'pool.e2e'
        AND card.event_type = 'ReleaseCard'
        AND manifest.event_type = 'ResolutionManifest'
        AND cached.validation_status = 'accepted'
      LIMIT 1")
    [ -n "$publication" ] && break
    attempts=$((attempts + 1))
    sleep 1
  done
  [ -n "$publication" ] || {
    echo "Node A did not publish a signed ReleaseCard and ResolutionManifest" >&2
    return 1
  }
  release_id=$(printf '%s' "$publication" | cut -d'|' -f1)
  manifest_id=$(printf '%s' "$publication" | cut -d'|' -f2)

  admin_post node-a 18081 /api/v1/admin/gonzbnet/sync/push '{}'
  admin_post node-d 18084 /api/v1/admin/gonzbnet/sync/pull '{}'
  attempts=0
  projected=0
  while [ "$attempts" -lt 30 ]; do
    projected=$(db_scalar gonzbnet_d "
      SELECT count(*)
      FROM federated_release_sources
      WHERE release_id = '$release_id'
        AND pool_id = 'pool.e2e'
        AND resolvable")
    [ "$projected" = "1" ] && break
    attempts=$((attempts + 1))
    sleep 1
  done
  [ "$projected" = "1" ] || {
    echo "Node D did not project the ReleaseCard from Node A" >&2
    return 1
  }

  token=$(admin_request node-d 18084 /api/v1/auth/tokens \
    "$(jq -cn --arg name "gonzbnet-e2e-$scan_id" '{name:$name}')" | jq -r '.secret')
  test -n "$token" && test "$token" != "null"
  if [ -n "${GONZBNET_E2E_CAPTURE_SECRET_DIR:-}" ]; then
    install -d -m 700 "$GONZBNET_E2E_CAPTURE_SECRET_DIR"
    printf '%s\n' "$token" | install -m 600 /dev/stdin "$GONZBNET_E2E_CAPTURE_SECRET_DIR/api-canary"
    printf '%s\n' "$release_canary" | install -m 600 /dev/stdin "$GONZBNET_E2E_CAPTURE_SECRET_DIR/release-canary"
  fi
  search_xml="$STATE/release-search.xml"
  curl -fsS --get \
    --data-urlencode 't=search' \
    --data-urlencode "q=$scan_id" \
    --data-urlencode "apikey=$token" \
    http://127.0.0.1:18084/api >"$search_xml"
  grep -Fq "$release_title" "$search_xml" || {
    echo "Node D local Newznab search did not return the federated release" >&2
    return 1
  }
  composite_id=$(sed -n 's:.*<guid isPermaLink="false">\([^<]*\)</guid>.*:\1:p' "$search_xml" | head -n 1)
  test -n "$composite_id" || {
    echo "could not extract the local Newznab release ID" >&2
    return 1
  }

  request_path="/manifests/$manifest_id/request"
  source_requests_before=$(grep -F -c "$request_path" "$STATE/node-a/gonzb.log" || true)
  curl -fsS --get \
    --data-urlencode 't=get' \
    --data-urlencode "id=$composite_id" \
    --data-urlencode "apikey=$token" \
    http://127.0.0.1:18084/api >"$STATE/first-grab.nzb"
  grep -Fq "&lt;$scan_id@example.invalid&gt;" "$STATE/first-grab.nzb" || {
    echo "first Node D grab did not return the expected NZB" >&2
    return 1
  }
  source_requests_after_first=$(grep -F -c "$request_path" "$STATE/node-a/gonzb.log" || true)
  [ "$source_requests_after_first" -gt "$source_requests_before" ] || {
    echo "first Node D grab did not request the manifest from Node A" >&2
    return 1
  }

  cached=$(db_scalar gonzbnet_d "
    SELECT count(*)
    FROM resolution_manifests cached
    JOIN federation_events source ON source.event_id = cached.source_event_id
    WHERE cached.manifest_id = '$manifest_id'
      AND cached.validation_status = 'accepted'
      AND source.event_type = 'ResolutionManifest'")
  [ "$cached" = "1" ] || {
    echo "Node D did not cache the verified signed manifest" >&2
    return 1
  }

  curl -fsS --get \
    --data-urlencode 't=get' \
    --data-urlencode "id=$composite_id" \
    --data-urlencode "apikey=$token" \
    http://127.0.0.1:18084/api >"$STATE/second-grab.nzb"
  cmp "$STATE/first-grab.nzb" "$STATE/second-grab.nzb"
  source_requests_after_second=$(grep -F -c "$request_path" "$STATE/node-a/gonzb.log" || true)
  [ "$source_requests_after_second" = "$source_requests_after_first" ] || {
    echo "second Node D grab contacted Node A instead of using the local cache" >&2
    return 1
  }

  for database in gonzbnet_a gonzbnet_b gonzbnet_c gonzbnet_d; do
    leaked=$(db_scalar "$database" "SELECT count(*) FROM federation_events WHERE body_json::text LIKE '%$token%'")
    [ "$leaked" = "0" ] || {
      echo "local API token leaked into federation events in $database" >&2
      return 1
    }
  done
  for name in node-a node-b node-c node-d; do
    if grep -Fq "$token" "$STATE/$name/gonzb.log"; then
      echo "local API token was not redacted from $name logs" >&2
      return 1
    fi
  done

  echo "Node D searched, resolved, verified, and cached release $release_id"
  echo "repeat Newznab grab reused the local manifest/NZB cache"
}

indexer_federation_smoke() {
  for name in node-a node-d; do
    test -s "$STATE/$name/csrf-token" || {
      echo "run bootstrap before indexer-federation-smoke" >&2
      return 1
    }
  done

  run_indexer_stage scrape_latest
  wait_db_at_least gonzbnet_a 4 "scraped article headers" \
    "SELECT count(*) FROM article_headers WHERE message_id LIKE '<gonzbnet-e2e-%@example.invalid>'"

  run_indexer_stage assemble
  wait_db_at_least gonzbnet_a 3 "assembled binaries" \
    "SELECT count(*) FROM binary_identity_current WHERE release_name = 'GoNZBNet.E2E.Indexer.Release.2026.1080p'"
  multipart=$(db_scalar gonzbnet_a "
    SELECT count(*)
    FROM binary_identity_current identity
    JOIN binary_observation_stats observed
      ON observed.binary_id = identity.binary_id
     AND observed.source_posted_at = identity.source_posted_at
    WHERE identity.file_name = 'GoNZBNet.E2E.Indexer.Release.2026.1080p.mkv'
      AND observed.total_parts = 2
      AND observed.observed_parts = 2")
  [ "$multipart" = "1" ] || {
    echo "multipart video did not assemble from both NNTP segments" >&2
    return 1
  }

  run_indexer_stage release_summary_refresh
  wait_db_at_least gonzbnet_a 1 "actionable release families" \
    "SELECT count(*) FROM release_ready_candidates WHERE release_name = 'GoNZBNet.E2E.Indexer.Release.2026.1080p' AND ready_reason = 'actionable' AND binary_count = 3 AND complete_binary_count = 3 AND expected_file_coverage_pct = 100"

  run_indexer_stage release
  wait_db_at_least gonzbnet_a 1 "formed releases" \
    "SELECT count(*) FROM releases WHERE title = 'GoNZBNet.E2E.Indexer.Release.2026.1080p' AND file_count = 3 AND completion_pct = 100 AND has_par2 AND has_nfo"
  release_id=$(db_scalar gonzbnet_a "SELECT release_id FROM releases WHERE title = 'GoNZBNet.E2E.Indexer.Release.2026.1080p' ORDER BY created_at DESC LIMIT 1")
  test -n "$release_id" || { echo "formed release has no release ID" >&2; return 1; }

  run_indexer_stage release_generate_nzb
  wait_db_at_least gonzbnet_a 1 "archived local NZBs" \
    "SELECT count(*) FROM release_archive_state WHERE release_id = '$release_id' AND archive_status IN ('archived', 'purge_pending', 'purged') AND object_key <> '' AND object_size_bytes > 0"
  run_indexer_stage release_archive_nzb

  attempts=0
  publication=""
  while [ "$attempts" -lt 90 ]; do
    publication=$(db_scalar gonzbnet_a "
      SELECT (card.body_json->>'release_id') || '|' ||
             (card.body_json->>'manifest_id')
      FROM federation_events card
      JOIN resolution_manifests manifest
        ON manifest.manifest_id = card.body_json->>'manifest_id'
      WHERE card.event_type = 'ReleaseCard'
        AND card.validation_status = 'accepted'
        AND card.body_json->>'title' = 'GoNZBNet.E2E.Indexer.Release.2026.1080p'
        AND manifest.validation_status = 'accepted'
      ORDER BY card.created_at DESC
      LIMIT 1")
    [ -n "$publication" ] && break
    attempts=$((attempts + 1))
    sleep 1
  done
  [ -n "$publication" ] || {
    echo "Node A did not publish the indexed release and signed manifest" >&2
    return 1
  }
  federated_release_id=$(printf '%s' "$publication" | cut -d'|' -f1)

  admin_post node-a 18081 /api/v1/admin/gonzbnet/sync/push '{}'
  admin_post node-d 18084 /api/v1/admin/gonzbnet/sync/pull '{}'
  wait_db_at_least gonzbnet_d 1 "Node D federated release projection" \
    "SELECT count(*) FROM federated_release_sources WHERE release_id = '$federated_release_id' AND pool_id = 'pool.e2e' AND resolvable"

  token=$(admin_request node-d 18084 /api/v1/auth/tokens \
    "$(jq -cn '{name:"gonzbnet-indexer-e2e"}')" | jq -r '.secret')
  test -n "$token" && test "$token" != "null"
  curl -fsS --get \
    --data-urlencode 't=search' \
    --data-urlencode 'q=GoNZBNet E2E Indexer Release 2026 1080p' \
    --data-urlencode "apikey=$token" \
    http://127.0.0.1:18084/api >"$STATE/indexer-release-search.xml"
  grep -Fq 'GoNZBNet.E2E.Indexer.Release.2026.1080p' "$STATE/indexer-release-search.xml" || {
    echo "Node D Newznab search did not return the indexed federated release" >&2
    return 1
  }
  composite_id=$(sed -n 's:.*<guid isPermaLink="false">\([^<]*\)</guid>.*:\1:p' "$STATE/indexer-release-search.xml" | head -n 1)
  test -n "$composite_id" || { echo "could not extract indexed federated release ID" >&2; return 1; }
  curl -fsS --get \
    --data-urlencode 't=get' \
    --data-urlencode "id=$composite_id" \
    --data-urlencode "apikey=$token" \
    http://127.0.0.1:18084/api >"$STATE/indexer-release-grab.nzb"
  for article in 1 2 3 4; do
    grep -Fq "&lt;gonzbnet-e2e-$article@example.invalid&gt;" "$STATE/indexer-release-grab.nzb" || {
      echo "federated NZB is missing deterministic article $article" >&2
      return 1
    }
  done

  echo "NNTP headers formed release $release_id with 3 files and 4 segments"
  publication_transport=${GONZBNET_E2E_TRANSPORT:-https}
  echo "Node A published it over $publication_transport; Node D found and grabbed the signed manifest through Newznab"
}

observability_smoke() {
  admin_get node-a 18081 /api/v1/admin/gonzbnet/overview |
    jq -e '(.jobs | length) == 5
      and (.pools | length) >= 1
      and (.production_checks | length) >= 5
      and (.production_ready | type) == "boolean"
      and .storage.available == true
      and .storage.gonzbnet_bytes > 0' >/dev/null
  admin_get node-a 18081 /api/v1/admin/gonzbnet/roles |
    jq -e '.jobs[] | select(.key == "contribute" and .configured == true)' >/dev/null
  admin_get node-b 18082 /api/v1/admin/gonzbnet/roles |
    jq -e '.jobs[] | select(.key == "verify" and .configured == true)' >/dev/null
  admin_get node-c 18083 /api/v1/admin/gonzbnet/roles |
    jq -e '.jobs[] | select(.key == "connection" and .configured == true)' >/dev/null
  admin_get node-a 18081 '/api/v1/admin/gonzbnet/activity?window=24h&pool_id=pool.e2e' |
    jq -e '.window == "24h" and (.items | type == "array")' >/dev/null
  admin_get node-a 18081 /api/v1/admin/gonzbnet/pools/pool.e2e/health |
    jq -e '.pool_id == "pool.e2e" and (.contributors | type == "array")' >/dev/null
  admin_get node-b 18082 '/api/v1/admin/gonzbnet/diagnostics/article-availability?pool_id=pool.e2e' |
    jq -e '.items | type == "array"' >/dev/null
  echo "GoNZBNet grouped roles, activity history, and pool evidence reporting verified"
}

case "${1:-}" in
  test)
    "$0" reset
    trap cleanup_test_run 0 1 2 15
    "$0" start
    "$0" bootstrap
    "$0" configure-pool
    "$0" admission-smoke
    "$0" smoke
    "$0" quorum-smoke
    "$0" federation-smoke
    "$0" release-smoke
    "$0" indexer-federation-smoke
    "$0" nntp-smoke
    "$0" observability-smoke
    "$0" reset
    trap - 0 1 2 15
    echo "GoNZBNet E2E test passed"
    ;;
  start)
    mkdir -p "$STATE"
    docker compose -p "$COMPOSE_PROJECT" -f "$COMPOSE" up -d --wait
    (cd "$ROOT" && GOCACHE="${GOCACHE:-/tmp/gocache}" go build \
      -ldflags "-X github.com/datallboy/gonzb/internal/buildinfo.Version=${GONZBNET_E2E_VERSION:-v0.9.0-smoke}" \
      -o "$BIN" ./cmd/gonzb)
    start_nntp_fixture
    start_tls_proxies
    cd "$ROOT"
    start_node node-a "$NODE_A_CONFIG"
    start_node node-b "$NODE_B_CONFIG"
    start_node node-c "$NODE_C_CONFIG"
    start_node node-d "$NODE_D_CONFIG"
    wait_http 18081
    wait_http 18082
    wait_http 18083
    wait_http 18084
    wait_https 18481
    wait_https 18482
    wait_https 18483
    wait_https 18484
    sleep 1
    for name in node-a node-b node-c node-d; do
      kill -0 "$(cat "$STATE/$name/pid")" 2>/dev/null || {
        echo "$name exited after its health check; inspect $STATE/$name/stdout.log" >&2
        exit 1
      }
    done
    echo "GoNZBNet E2E nodes are ready behind trusted HTTPS: https://localhost:18481, :18482, :18483, :18484"
    ;;
  bootstrap)
    command -v jq >/dev/null 2>&1 || { echo "jq is required" >&2; exit 1; }
    password="${GONZBNET_E2E_PASSWORD:-gonzb-e2e-local}"
    bootstrap_node node-a 18081 "$password"
    bootstrap_node node-b 18082 "$password"
    bootstrap_node node-c 18083 "$password"
    bootstrap_node node-d 18084 "$password"
    aggregator='{"aggregator":{"sources":{"local_blob":{"enabled":false},"usenet_indexer":{"enabled":false},"gonzbnet":{"enabled":true}}}}'
    indexer_aggregator='{"aggregator":{"sources":{"local_blob":{"enabled":false},"usenet_indexer":{"enabled":true},"gonzbnet":{"enabled":true}}}}'
    admin_put node-a 18081 /api/v1/admin/settings "$indexer_aggregator"
    admin_put node-b 18082 /api/v1/admin/settings "$aggregator"
    admin_put node-c 18083 /api/v1/admin/settings "$aggregator"
    admin_put node-d 18084 /api/v1/admin/settings "$aggregator"
    echo "GoNZBNet aggregator source enabled on all nodes"
    echo "Local admin password: $password"
    ;;
  configure-pool)
    command -v jq >/dev/null 2>&1 || { echo "jq is required" >&2; exit 1; }
    configure_pool
    ;;
  seed-traversal-peers)
    command -v jq >/dev/null 2>&1 || { echo "jq is required" >&2; exit 1; }
    seed_traversal_peers
    ;;
  admission-smoke)
    admission_smoke
    ;;
  quorum-smoke)
    command -v jq >/dev/null 2>&1 || { echo "jq is required" >&2; exit 1; }
    quorum_smoke
    ;;
  smoke)
    ids=""
    if curl -fsS "https://localhost:18481/.well-known/gonzbnet" >/dev/null 2>&1; then
      echo "disposable HTTPS endpoint unexpectedly trusted without the fixture CA" >&2
      exit 1
    fi
    if curl -fsS --cacert "$TLS_CA" --tls-max 1.1 "https://localhost:18481/healthz" >/dev/null 2>&1; then
      echo "HTTPS endpoint accepted obsolete TLS 1.1" >&2
      exit 1
    fi
    curl -fsS --cacert "$TLS_CA" --tlsv1.2 --tls-max 1.2 "https://localhost:18481/healthz" >/dev/null
    for port in 18481 18482 18483 18484; do
      curl -fsS --cacert "$TLS_CA" "https://localhost:$port/.well-known/gonzbnet" | jq -e '.spec_version == "gonzbnet/1.0" and (.base_url | startswith("https://"))' >/dev/null
      profile=$(curl -fsS --cacert "$TLS_CA" "https://localhost:$port/gonzbnet/v1/node")
      node_id=$(echo "$profile" | jq -r '.node_id')
      test -n "$node_id"
      case " $ids " in *" $node_id "*) echo "duplicate node identity: $node_id" >&2; exit 1;; esac
      ids="$ids $node_id"
      echo "$profile" | jq -e '.software_version == "v0.9.0-smoke" and (.endpoints.base | startswith("https://"))' >/dev/null
      curl -fsS --cacert "$TLS_CA" "https://localhost:$port/gonzbnet/v1/caps" | jq -e '.spec_versions | index("gonzbnet/1.0") != null' >/dev/null
      echo "port=$port node_id=$node_id"
    done
    for database in gonzbnet_a gonzbnet_b gonzbnet_c gonzbnet_d; do
      insecure=$(db_scalar "$database" "SELECT count(*) FROM federation_peers WHERE enabled AND peer_url LIKE 'http://%'")
      [ "$insecure" = "0" ] || { echo "$database persisted an insecure HTTP peer" >&2; exit 1; }
      if [ "${GONZBNET_E2E_TRANSPORT:-https}" = "traversal" ]; then
        connected=$(db_scalar "$database" "
          SELECT count(*) FROM federation_node_endpoints
          WHERE enabled AND transport_type = 'ice' AND last_success_at IS NOT NULL
            AND path_type IN ('direct', 'relay') AND ice_state = 'connected'")
        [ "$connected" -ge 1 ] || { echo "$database has no successful traversal endpoint" >&2; exit 1; }
      else
        connected=$(db_scalar "$database" "SELECT count(*) FROM federation_peers WHERE enabled AND status = 'connected' AND peer_url LIKE 'https://%'")
        [ "$connected" -ge 1 ] || { echo "$database has no connected HTTPS peer" >&2; exit 1; }
      fi
    done
    echo "trusted TLS 1.2+, certificate rejection, selected peer transport persistence, advertisement, and release version verified"
    ;;
  federation-smoke)
    command -v jq >/dev/null 2>&1 || { echo "jq is required" >&2; exit 1; }
    federation_smoke
    ;;
  release-smoke)
    command -v jq >/dev/null 2>&1 || { echo "jq is required" >&2; exit 1; }
    release_smoke
    ;;
  indexer-federation-smoke)
    command -v jq >/dev/null 2>&1 || { echo "jq is required" >&2; exit 1; }
    indexer_federation_smoke
    ;;
  nntp-smoke)
    command -v jq >/dev/null 2>&1 || { echo "jq is required" >&2; exit 1; }
    nntp_smoke
    ;;
  observability-smoke)
    command -v jq >/dev/null 2>&1 || { echo "jq is required" >&2; exit 1; }
    observability_smoke
    ;;
  stop)
    stop_all
    docker compose -p "$COMPOSE_PROJECT" -f "$COMPOSE" down
    ;;
  status)
    for port in 18081 18082 18083 18084; do
      curl -fsS "http://127.0.0.1:$port/healthz" || true
      echo
    done
    docker compose -p "$COMPOSE_PROJECT" -f "$COMPOSE" ps
    ;;
  logs)
    tail -n 100 -F "$STATE/node-a/stdout.log" "$STATE/node-b/stdout.log" "$STATE/node-c/stdout.log" "$STATE/node-d/stdout.log"
    ;;
  reset)
    stop_all
    docker compose -p "$COMPOSE_PROJECT" -f "$COMPOSE" down -v
    rm -rf "$STATE"
    ;;
  *)
    usage
    exit 2
    ;;
esac
