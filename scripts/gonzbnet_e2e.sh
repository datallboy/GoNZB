#!/usr/bin/env sh
# TEST FIXTURE ONLY. All state and generated credentials stay under .e2e/.
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
STATE="$ROOT/.e2e/gonzbnet"
COMPOSE="$ROOT/docker-compose.gonzbnet-e2e.yml"
COMPOSE_PROJECT="gonzbnet-e2e"
BIN="$STATE/gonzb"
NNTP_BIN="$STATE/nntpfixture"

usage() {
  echo "usage: $0 {test|start|bootstrap|configure-pool|admission-smoke|quorum-smoke|smoke|federation-smoke|release-smoke|nntp-smoke|observability-smoke|stop|status|logs|reset}"
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
  for name in node-a node-b node-c node-d; do
    pidfile="$STATE/$name/pid"
    if [ -f "$pidfile" ]; then
      pid=$(cat "$pidfile")
      kill "$pid" 2>/dev/null || true
      rm -f "$pidfile"
    fi
  done
  if [ -f "$STATE/nntpfixture.pid" ]; then
    kill "$(cat "$STATE/nntpfixture.pid")" 2>/dev/null || true
    rm -f "$STATE/nntpfixture.pid"
  fi
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
  admin_request "$@" >/dev/null
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

configure_pool() {
  pool=$(jq -n '{pool_id:"pool.e2e",display_name:"GoNZBNet E2E",description:"Four-node admission test pool",membership_threshold:1,moderation_threshold:1,checkpoint_witness_threshold:1,accept_mode:"pool_member",min_node_trust_score:0,visibility:"unlisted",join_mode:"approval",admission_enabled:true,enabled:true}')
  admin_post node-a 18081 /api/v1/admin/gonzbnet/pools "$pool"
  join_pool node-b 18082 "http://127.0.0.1:18081" pool.e2e node-a 18081
  join_pool node-c 18083 "http://127.0.0.1:18081" pool.e2e node-a 18081
  join_pool node-d 18084 "http://127.0.0.1:18082" pool.e2e node-a 18081

  pool_two=$(jq -n '{pool_id:"pool.side",display_name:"Side Pool",description:"C and D isolation test",membership_threshold:1,moderation_threshold:1,checkpoint_witness_threshold:1,accept_mode:"pool_member",min_node_trust_score:0,visibility:"private",join_mode:"approval",admission_enabled:true,enabled:true}')
  admin_post node-d 18084 /api/v1/admin/gonzbnet/pools "$pool_two"
  invitation=$(admin_request node-d 18084 /api/v1/admin/gonzbnet/pools/pool.side/invitations '{}' | jq -r '.link')
  test -n "$invitation" && test "$invitation" != "null"
  join_pool node-c 18083 "$invitation" pool.side node-d 18084 '["consumer"]'

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
  case "$locator" in
    *:18082*)
      admin_post node-b 18082 /api/v1/admin/gonzbnet/sync/push '{}'
      admin_post "$admin" "$admin_port" /api/v1/admin/gonzbnet/sync/pull '{}'
      ;;
  esac
  approval_event=$(admin_request "$admin" "$admin_port" "/api/v1/admin/gonzbnet/admissions/$proposal/approve" '{}' | jq -r '.approval_event.event_id')
  duplicate_approval=$(admin_request "$admin" "$admin_port" "/api/v1/admin/gonzbnet/admissions/$proposal/approve" '{}' | jq -r '.approval_event.event_id')
  [ "$duplicate_approval" = "$approval_event" ] || { echo "duplicate approval created a second final event" >&2; return 1; }
  admin_post "$candidate" "$candidate_port" "/api/v1/admin/gonzbnet/admissions/$proposal/refresh" '{}'
  echo "$candidate joined $pool_id through $locator"
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
  start_node node-a "$ROOT/test/e2e/gonzbnet/node-a.yaml"
  start_node node-b "$ROOT/test/e2e/gonzbnet/node-b.yaml"
  start_node node-c "$ROOT/test/e2e/gonzbnet/node-c.yaml"
  start_node node-d "$ROOT/test/e2e/gonzbnet/node-d.yaml"
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

  admin_b_payload=$(jq -n '{locator:"http://127.0.0.1:18081",pool_id:"pool.quorum",role:"admin"}')
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

  candidate_payload=$(jq -n '{locator:"http://127.0.0.1:18081",pool_id:"pool.quorum",role:"member"}')
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
  echo "$result" | jq -e '.group == "alt.binaries.test" and .count == 1 and .overview_rows == 1 and .body_bytes > 0' >/dev/null || {
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
  now=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  fixture=$(jq -cn \
    --arg scan_id "$scan_id" \
    --arg now "$now" \
    '{
      LocalReleaseID:$scan_id,
      GUID:$scan_id,
      Title:("GoNZBNet E2E " + $scan_id),
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
  search_xml="$STATE/release-search.xml"
  curl -fsS --get \
    --data-urlencode 't=search' \
    --data-urlencode "q=$scan_id" \
    --data-urlencode "apikey=$token" \
    http://127.0.0.1:18084/api >"$search_xml"
  grep -Fq "GoNZBNet E2E $scan_id" "$search_xml" || {
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

observability_smoke() {
  admin_get node-a 18081 /api/v1/admin/gonzbnet/overview |
    jq -e '(.jobs | length) == 5 and (.pools | length) >= 1' >/dev/null
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
    trap '"$0" reset >/dev/null 2>&1 || true' 0 1 2 15
    "$0" start
    "$0" bootstrap
    "$0" configure-pool
    "$0" admission-smoke
    "$0" smoke
    "$0" quorum-smoke
    "$0" federation-smoke
    "$0" release-smoke
    "$0" nntp-smoke
    "$0" observability-smoke
    "$0" reset
    trap - 0 1 2 15
    echo "GoNZBNet E2E test passed"
    ;;
  start)
    mkdir -p "$STATE"
    docker compose -p "$COMPOSE_PROJECT" -f "$COMPOSE" up -d --wait
    (cd "$ROOT" && GOCACHE="${GOCACHE:-/tmp/gocache}" go build -o "$BIN" ./cmd/gonzb)
    cd "$ROOT"
    start_node node-a "$ROOT/test/e2e/gonzbnet/node-a.yaml"
    start_node node-b "$ROOT/test/e2e/gonzbnet/node-b.yaml"
    start_node node-c "$ROOT/test/e2e/gonzbnet/node-c.yaml"
    start_node node-d "$ROOT/test/e2e/gonzbnet/node-d.yaml"
    wait_http 18081
    wait_http 18082
    wait_http 18083
    wait_http 18084
    sleep 1
    for name in node-a node-b node-c node-d; do
      kill -0 "$(cat "$STATE/$name/pid")" 2>/dev/null || {
        echo "$name exited after its health check; inspect $STATE/$name/stdout.log" >&2
        exit 1
      }
    done
    echo "GoNZBNet E2E nodes are ready: http://127.0.0.1:18081, :18082, :18083, :18084"
    ;;
  bootstrap)
    command -v jq >/dev/null 2>&1 || { echo "jq is required" >&2; exit 1; }
    password="${GONZBNET_E2E_PASSWORD:-gonzb-e2e-local}"
    bootstrap_node node-a 18081 "$password"
    bootstrap_node node-b 18082 "$password"
    bootstrap_node node-c 18083 "$password"
    bootstrap_node node-d 18084 "$password"
    aggregator='{"aggregator":{"sources":{"local_blob":{"enabled":false},"usenet_indexer":{"enabled":false},"gonzbnet":{"enabled":true}}}}'
    admin_put node-a 18081 /api/v1/admin/settings "$aggregator"
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
  admission-smoke)
    admission_smoke
    ;;
  quorum-smoke)
    command -v jq >/dev/null 2>&1 || { echo "jq is required" >&2; exit 1; }
    quorum_smoke
    ;;
  smoke)
    ids=""
    for port in 18081 18082 18083 18084; do
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
  release-smoke)
    command -v jq >/dev/null 2>&1 || { echo "jq is required" >&2; exit 1; }
    release_smoke
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
    stop_nodes
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
    stop_nodes
    docker compose -p "$COMPOSE_PROJECT" -f "$COMPOSE" down -v
    rm -rf "$STATE"
    ;;
  *)
    usage
    exit 2
    ;;
esac
