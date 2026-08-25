#!/bin/sh
# Synthetic-only uploader failure, restart, and integrity soak. This harness
# never contacts a Usenet provider, tracker, torrent client, or downloader.
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
STATE="$ROOT/.e2e/uploader-negative-soak"
GONZBNET_STATE="$ROOT/.e2e/gonzbnet"
GONZBNET_SCRIPT="$ROOT/scripts/gonzbnet_e2e.sh"
COMPOSE="$ROOT/docker-compose.gonzbnet-e2e.yml"
COMPOSE_PROJECT="gonzbnet-e2e"
NODE_A_CONFIG="$STATE/node-a.yaml"
SOAK_ITERATIONS=${UPLOADER_SOAK_ITERATIONS:-20}
RESTART_CYCLES=${UPLOADER_SOAK_RESTARTS:-2}
KEEP_STATE=${UPLOADER_SOAK_KEEP_STATE:-0}

require_command() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "$1 is required" >&2
    exit 69
  }
}

cleanup() {
  result=$?
  trap - 0 1 2 15
  if [ "$result" -ne 0 ]; then
    echo "Uploader negative/soak conformance failed; recent Node A/D logs follow" >&2
    for log in "$GONZBNET_STATE/node-a/stdout.log" "$GONZBNET_STATE/node-d/stdout.log"; do
      if [ -f "$log" ]; then
        echo "==> $log <==" >&2
        tail -n 80 "$log" >&2 || true
      fi
    done
  fi
  if [ "$KEEP_STATE" != "1" ]; then
    "$GONZBNET_SCRIPT" reset >/dev/null 2>&1 || true
    rm -rf "$STATE"
  else
    echo "state retained under $STATE and $GONZBNET_STATE" >&2
  fi
  exit "$result"
}

wait_http() {
  port_number=$1
  attempts=0
  until curl -fsS "http://127.0.0.1:$port_number/healthz" >/dev/null 2>&1; do
    attempts=$((attempts + 1))
    if [ "$attempts" -ge 100 ]; then
      echo "timed out waiting for GoNZB on port $port_number" >&2
      return 1
    fi
    sleep 0.1
  done
}

stop_node() {
  node_name=$1
  pid_file="$GONZBNET_STATE/$node_name/pid"
  if [ ! -f "$pid_file" ]; then
    return
  fi
  node_pid=$(cat "$pid_file")
  kill "$node_pid" 2>/dev/null || true
  attempts=0
  while kill -0 "$node_pid" 2>/dev/null; do
    attempts=$((attempts + 1))
    if [ "$attempts" -ge 100 ]; then
      echo "timed out stopping $node_name" >&2
      return 1
    fi
    sleep 0.1
  done
  rm -f "$pid_file"
}

start_node() {
  node_name=$1
  port_number=$2
  config_file=$3
  node_dir="$GONZBNET_STATE/$node_name"
  if command -v setsid >/dev/null 2>&1; then
    setsid env SSL_CERT_FILE="$GONZBNET_STATE/tls/ca.pem" \
      "$GONZBNET_STATE/gonzb" serve --config "$config_file" \
      </dev/null >>"$node_dir/stdout.log" 2>&1 &
  else
    nohup env SSL_CERT_FILE="$GONZBNET_STATE/tls/ca.pem" \
      "$GONZBNET_STATE/gonzb" serve --config "$config_file" \
      </dev/null >>"$node_dir/stdout.log" 2>&1 &
  fi
  echo "$!" >"$node_dir/pid"
  wait_http "$port_number"
  sleep 1
  kill -0 "$(cat "$node_dir/pid")" 2>/dev/null || {
    echo "$node_name exited after restart" >&2
    return 1
  }
}

restart_node() {
  node_name=$1
  port_number=$2
  config_file=$3
  stop_node "$node_name"
  start_node "$node_name" "$port_number" "$config_file"
}

admin_request() {
  node_name=$1
  port_number=$2
  endpoint=$3
  payload=$4
  csrf=$(cat "$GONZBNET_STATE/$node_name/csrf-token")
  curl --fail-with-body --silent --show-error \
    -b "$GONZBNET_STATE/$node_name/cookies.txt" \
    -H "X-CSRF-Token: $csrf" -H 'Content-Type: application/json' \
    -d "$payload" "http://127.0.0.1:$port_number$endpoint"
}

admin_patch() {
  node_name=$1
  port_number=$2
  endpoint=$3
  payload=$4
  csrf=$(cat "$GONZBNET_STATE/$node_name/csrf-token")
  curl --fail-with-body --silent --show-error -X PATCH \
    -b "$GONZBNET_STATE/$node_name/cookies.txt" \
    -H "X-CSRF-Token: $csrf" -H 'Content-Type: application/json' \
    -d "$payload" "http://127.0.0.1:$port_number$endpoint"
}

admin_get() {
  node_name=$1
  port_number=$2
  endpoint=$3
  curl --fail-with-body --silent --show-error \
    -b "$GONZBNET_STATE/$node_name/cookies.txt" \
    "http://127.0.0.1:$port_number$endpoint"
}

submit_bearer() {
  token_value=$1
  nzb_path=$2
  idempotency_key=$3
  destination=$4
  curl --silent --show-error -o "$destination" -w '%{http_code}' \
    -H "Authorization: Bearer $token_value" \
    -H "Idempotency-Key: $idempotency_key" \
    -F "nzb=@$nzb_path;type=application/x-nzb" \
    http://127.0.0.1:18081/api/v1/uploader/submissions
}

newznab_search() {
  port_number=$1
  token_value=$2
  query=$3
  destination=$4
  curl --fail-with-body --silent --show-error --get \
    --data-urlencode 't=search' \
    --data-urlencode "q=$query" \
    --data-urlencode "apikey=$token_value" \
    "http://127.0.0.1:$port_number/api" >"$destination"
}

newznab_get_code() {
  port_number=$1
  token_value=$2
  release_guid=$3
  destination=$4
  curl --silent --show-error -o "$destination" -w '%{http_code}' --get \
    --data-urlencode 't=get' \
    --data-urlencode "id=$release_guid" \
    --data-urlencode "apikey=$token_value" \
    "http://127.0.0.1:$port_number/api"
}

extract_guid() {
  sed -n 's:.*<guid isPermaLink="false">\([^<]*\)</guid>.*:\1:p' "$1" | head -n 1
}

sqlite_scalar() {
  sqlite3 "$GONZBNET_STATE/node-a/gonzb.db" "$1"
}

db_exec() {
  database_name=$1
  sql_query=$2
  docker compose -p "$COMPOSE_PROJECT" -f "$COMPOSE" exec -T postgres \
    psql -v ON_ERROR_STOP=1 -U gonzb -d "$database_name" -c "$sql_query" >/dev/null
}

case "$SOAK_ITERATIONS:$RESTART_CYCLES" in
  *[!0-9:]*|:*|*:)
    echo "UPLOADER_SOAK_ITERATIONS and UPLOADER_SOAK_RESTARTS must be non-negative integers" >&2
    exit 64
    ;;
esac

for command_name in curl dd docker go jq sed sha256sum sqlite3; do
  require_command "$command_name"
done

trap cleanup 0 1 2 15
"$GONZBNET_SCRIPT" reset >/dev/null 2>&1 || true
rm -rf "$STATE"
mkdir -p "$STATE/inbox" "$STATE/fixtures" "$STATE/responses"

sed \
  -e "s|  inbox: { enabled: false }|  inbox: { enabled: true, path: \"$STATE/inbox\", scan_interval_seconds: 1, settle_age_seconds: 3 }|" \
  -e 's|max_nzb_bytes: 67108864|max_nzb_bytes: 2048|' \
  -e 's|max_metadata_length: 16384|max_metadata_length: 1024|' \
  -e 's|max_artifact_bytes: 33554432|max_artifact_bytes: 2048|' \
  -e 's|max_submission_bytes: 134217728|max_submission_bytes: 8192|' \
  "$ROOT/test/e2e/gonzbnet/node-a.yaml" >"$NODE_A_CONFIG"
grep -Fq 'inbox: { enabled: true' "$NODE_A_CONFIG"
grep -Fq 'max_nzb_bytes: 2048' "$NODE_A_CONFIG"

write_nzb() {
  output_path=$1
  title_value=$2
  message_id=$3
  size_value=$4
  cat >"$output_path" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">
  <head><meta type="title">$title_value</meta></head>
  <file poster="synthetic-soak@example.invalid" date="1786122000" subject="$title_value [1/1] - &quot;synthetic.txt&quot; yEnc (1/1)">
    <groups><group>alt.binaries.gonzb.synthetic</group></groups>
    <segments><segment bytes="$size_value" number="1">$message_id</segment></segments>
  </file>
</nzb>
EOF
}

write_nzb "$STATE/fixtures/a.nzb" "Synthetic.Uploader.Soak.A.CC0" "synthetic-soak-a@example.invalid" 101
write_nzb "$STATE/fixtures/b.nzb" "Synthetic.Uploader.Soak.B.CC0" "synthetic-soak-b@example.invalid" 102
write_nzb "$STATE/fixtures/c.nzb" "Synthetic.Uploader.Soak.C.CC0" "synthetic-soak-c@example.invalid" 103
write_nzb "$STATE/fixtures/d.nzb" "Synthetic.Uploader.Soak.D.CC0" "synthetic-soak-d@example.invalid" 104
printf '%s\n' 'not an nzb' >"$STATE/fixtures/malformed.nzb"
dd if=/dev/zero of="$STATE/fixtures/oversized.nzb" bs=4096 count=1 2>/dev/null
for fixture_path in "$STATE"/fixtures/[abcd].nzb; do
  fixture_size=$(wc -c <"$fixture_path")
  [ "$fixture_size" -lt 2048 ] || {
    echo "$fixture_path exceeds the bounded live-test limit" >&2
    exit 1
  }
done
fixture_hashes_before=$(sha256sum "$STATE"/fixtures/[abcd].nzb)

echo "Starting a disposable four-node pool with bounded uploader limits and inbox intake"
GONZBNET_NODE_A_CONFIG="$NODE_A_CONFIG" "$GONZBNET_SCRIPT" start
"$GONZBNET_SCRIPT" bootstrap
"$GONZBNET_SCRIPT" configure-pool

echo "Creating submission-only and read-only identities"
admin_request node-a 18081 /api/v1/admin/auth/roles \
  '{"id":"soak-submit","name":"Uploader soak submit","permissions":["uploader.submissions.create"]}' >/dev/null
admin_request node-a 18081 /api/v1/admin/auth/roles \
  '{"id":"soak-read","name":"Uploader soak read","permissions":["uploader.submissions.read"]}' >/dev/null
admin_request node-a 18081 /api/v1/admin/auth/users \
  '{"id":"soak-submit","username":"soak-submit","password":"soak-submit-local-2026","enabled":true,"role_ids":["soak-submit"]}' >/dev/null
admin_request node-a 18081 /api/v1/admin/auth/users \
  '{"id":"soak-read","username":"soak-read","password":"soak-read-local-2026","enabled":true,"role_ids":["soak-read"]}' >/dev/null
SUBMIT_TOKEN=$(admin_request node-a 18081 /api/v1/admin/auth/tokens \
  '{"user_id":"soak-submit","name":"negative-soak-submit"}' | jq -r '.secret')
READ_TOKEN=$(admin_request node-a 18081 /api/v1/admin/auth/tokens \
  '{"user_id":"soak-read","name":"negative-soak-read"}' | jq -r '.secret')
test -n "$SUBMIT_TOKEN" && test "$SUBMIT_TOKEN" != "null"
test -n "$READ_TOKEN" && test "$READ_TOKEN" != "null"

echo "Checking authentication, permission, malformed input, and bounded-size failures"
status_code=$(curl --silent --show-error -o "$STATE/responses/no-auth.json" -w '%{http_code}' \
  -F "nzb=@$STATE/fixtures/a.nzb;type=application/x-nzb" \
  http://127.0.0.1:18081/api/v1/uploader/submissions)
[ "$status_code" = "401" ]
status_code=$(submit_bearer "$READ_TOKEN" "$STATE/fixtures/a.nzb" read-cannot-submit "$STATE/responses/read-forbidden.json")
[ "$status_code" = "403" ]
status_code=$(submit_bearer "$SUBMIT_TOKEN" "$STATE/fixtures/malformed.nzb" malformed-input "$STATE/responses/malformed.json")
[ "$status_code" = "400" ]
status_code=$(submit_bearer "$SUBMIT_TOKEN" "$STATE/fixtures/oversized.nzb" oversized-input "$STATE/responses/oversized.json")
[ "$status_code" = "413" ]
[ "$(sqlite_scalar 'SELECT count(*) FROM uploader_submissions;')" = "0" ]

echo "Checking idempotency, exact-byte deduplication, and conflict handling"
status_code=$(submit_bearer "$SUBMIT_TOKEN" "$STATE/fixtures/a.nzb" stable-idempotency-key "$STATE/responses/a-first.json")
[ "$status_code" = "201" ]
submission_a=$(jq -r '.submission.id' "$STATE/responses/a-first.json")
release_a=$(jq -r '.submission.release_id' "$STATE/responses/a-first.json")
test -n "$submission_a" && test "$submission_a" != "null"
status_code=$(submit_bearer "$SUBMIT_TOKEN" "$STATE/fixtures/a.nzb" stable-idempotency-key "$STATE/responses/a-duplicate.json")
[ "$status_code" = "200" ]
jq -e --arg id "$submission_a" '.created == false and .submission.id == $id' "$STATE/responses/a-duplicate.json" >/dev/null
status_code=$(submit_bearer "$SUBMIT_TOKEN" "$STATE/fixtures/b.nzb" stable-idempotency-key "$STATE/responses/idempotency-conflict.json")
[ "$status_code" = "409" ]
[ "$(sqlite_scalar 'SELECT count(*) FROM uploader_submissions;')" = "1" ]

echo "Exercising the browser session and CSRF mutation boundary"
csrf=$(cat "$GONZBNET_STATE/node-a/csrf-token")
status_code=$(curl --silent --show-error -o "$STATE/responses/browser-no-csrf.json" -w '%{http_code}' \
  -b "$GONZBNET_STATE/node-a/cookies.txt" \
  -F "nzb=@$STATE/fixtures/b.nzb;type=application/x-nzb" \
  http://127.0.0.1:18081/api/v1/uploader/submissions)
[ "$status_code" = "403" ]
status_code=$(curl --silent --show-error -o "$STATE/responses/browser-create.json" -w '%{http_code}' \
  -b "$GONZBNET_STATE/node-a/cookies.txt" -H "X-CSRF-Token: $csrf" \
  -F "nzb=@$STATE/fixtures/b.nzb;type=application/x-nzb" \
  http://127.0.0.1:18081/api/v1/uploader/submissions)
[ "$status_code" = "201" ]
submission_b=$(jq -r '.submission.id' "$STATE/responses/browser-create.json")
jq -e '.submission.intake_kind == "manual" and .submission.submitted_by == "admin"' \
  "$STATE/responses/browser-create.json" >/dev/null
admin_patch node-a 18081 "/api/v1/uploader/submissions/$submission_b" \
  '{"title":"Synthetic.Uploader.Soak.B.Edited.CC0","category_id":2040,"note":"browser transport smoke"}' | \
  jq -e '.submission.title == "Synthetic.Uploader.Soak.B.Edited.CC0" and .submission.category_id == 2040' >/dev/null
admin_request node-a 18081 "/api/v1/uploader/submissions/$submission_b/actions/approve" '{}' | \
  jq -e '.submission.state == "approved"' >/dev/null
SEARCH_TOKEN=$(admin_request node-a 18081 /api/v1/auth/tokens '{"name":"negative-soak-search"}' | jq -r '.secret')
newznab_search 18081 "$SEARCH_TOKEN" "Synthetic.Uploader.Soak.B.Edited.CC0" "$STATE/responses/browser-search.xml"
grep -Fq 'Synthetic.Uploader.Soak.B.Edited.CC0' "$STATE/responses/browser-search.xml"
browser_guid=$(extract_guid "$STATE/responses/browser-search.xml")
[ "$(newznab_get_code 18081 "$SEARCH_TOKEN" "$browser_guid" "$STATE/responses/browser-grab.nzb")" = "200" ]
admin_request node-a 18081 "/api/v1/uploader/submissions/$submission_b/actions/return-to-pending" '{}' | \
  jq -e '.submission.state == "pending_review"' >/dev/null
status_code=$(newznab_get_code 18081 "$SEARCH_TOKEN" "$browser_guid" "$STATE/responses/browser-grab-after-pending.xml")
[ "$status_code" != "200" ]

echo "Checking interrupted inbox delivery and durable recovery across a GoNZB outage"
printf '%s\n' '<nzb><file' >"$STATE/inbox/interrupted.nzb"
sleep 1
[ "$(sqlite_scalar 'SELECT count(*) FROM uploader_inbox_failures;')" = "0" ]
[ "$(sqlite_scalar 'SELECT count(*) FROM uploader_submissions;')" = "2" ]
stop_node node-a
if GONZB_URL=http://127.0.0.1:18081 GONZB_TOKEN="$SUBMIT_TOKEN" \
  "$ROOT/scripts/gonzb-submit-nzb.sh" "$STATE/fixtures/c.nzb" \
  >"$STATE/responses/outage-submit.json" 2>"$STATE/responses/outage-submit.err"; then
  echo "submission unexpectedly succeeded while Node A was stopped" >&2
  exit 1
fi
cp "$STATE/fixtures/c.nzb" "$STATE/inbox/interrupted.nzb"
touch -d '5 minutes ago' "$STATE/inbox/interrupted.nzb"
start_node node-a 18081 "$NODE_A_CONFIG"
"$GONZBNET_SCRIPT" bootstrap >/dev/null
attempts=0
while [ "$attempts" -lt 30 ]; do
  inbox_c=$(sqlite_scalar "SELECT count(*) FROM uploader_submissions WHERE title = 'Synthetic.Uploader.Soak.C.CC0' AND intake_kind = 'inbox';")
  [ "$inbox_c" = "1" ] && break
  attempts=$((attempts + 1))
  sleep 1
done
[ "$inbox_c" = "1" ]

echo "Running $SOAK_ITERATIONS duplicate deliveries across $RESTART_CYCLES restart cycles"
iteration=1
while [ "$iteration" -le "$SOAK_ITERATIONS" ]; do
  status_code=$(submit_bearer "$SUBMIT_TOKEN" "$STATE/fixtures/a.nzb" stable-idempotency-key "$STATE/responses/soak-$iteration.json")
  [ "$status_code" = "200" ]
  jq -e --arg id "$submission_a" '.created == false and .submission.id == $id' "$STATE/responses/soak-$iteration.json" >/dev/null
  iteration=$((iteration + 1))
done
restart_number=1
while [ "$restart_number" -le "$RESTART_CYCLES" ]; do
  restart_node node-a 18081 "$NODE_A_CONFIG"
  "$GONZBNET_SCRIPT" bootstrap >/dev/null
  status_code=$(submit_bearer "$SUBMIT_TOKEN" "$STATE/fixtures/a.nzb" stable-idempotency-key "$STATE/responses/restart-$restart_number.json")
  [ "$status_code" = "200" ]
  jq -e --arg id "$submission_a" '.created == false and .submission.id == $id' "$STATE/responses/restart-$restart_number.json" >/dev/null
  restart_number=$((restart_number + 1))
done
[ "$(sqlite_scalar "SELECT count(*) FROM uploader_submissions WHERE id = '$submission_a';")" = "1" ]

echo "Checking stable-file rejection, failure suppression, changed-file retry, and symlink rejection"
cp "$STATE/fixtures/malformed.nzb" "$STATE/inbox/recoverable.nzb"
cp "$STATE/fixtures/oversized.nzb" "$STATE/inbox/oversized.nzb"
touch -d '5 minutes ago' "$STATE/inbox/recoverable.nzb" "$STATE/inbox/oversized.nzb"
attempts=0
while [ "$attempts" -lt 20 ]; do
  failure_count=$(sqlite_scalar 'SELECT count(*) FROM uploader_inbox_failures;')
  [ "$failure_count" = "2" ] && break
  attempts=$((attempts + 1))
  sleep 1
done
[ "$failure_count" = "2" ]
failure_stamp_before=$(sqlite_scalar "SELECT updated_at FROM uploader_inbox_failures WHERE error_code = 'size_invalid';")
sleep 3
failure_stamp_after=$(sqlite_scalar "SELECT updated_at FROM uploader_inbox_failures WHERE error_code = 'size_invalid';")
[ "$failure_stamp_after" = "$failure_stamp_before" ]
cp "$STATE/fixtures/d.nzb" "$STATE/inbox/recoverable.nzb"
touch -d '4 minutes ago' "$STATE/inbox/recoverable.nzb"
attempts=0
while [ "$attempts" -lt 20 ]; do
  inbox_d=$(sqlite_scalar "SELECT count(*) FROM uploader_submissions WHERE title = 'Synthetic.Uploader.Soak.D.CC0' AND intake_kind = 'inbox';")
  [ "$inbox_d" = "1" ] && break
  attempts=$((attempts + 1))
  sleep 1
done
[ "$inbox_d" = "1" ]
[ "$(sqlite_scalar 'SELECT count(*) FROM uploader_inbox_failures;')" = "1" ]
submission_count_before_symlink=$(sqlite_scalar 'SELECT count(*) FROM uploader_submissions;')
ln -s "$STATE/fixtures/a.nzb" "$STATE/inbox/outside-link.nzb"
sleep 4
[ "$(sqlite_scalar 'SELECT count(*) FROM uploader_submissions;')" = "$submission_count_before_symlink" ]
[ "$(sqlite_scalar 'SELECT count(*) FROM uploader_inbox_failures;')" = "1" ]

echo "Approving, explicitly publishing, and resolving the soak release on Node D"
admin_request node-a 18081 "/api/v1/uploader/submissions/$submission_a/actions/approve" '{}' | \
  jq -e '.submission.state == "approved"' >/dev/null
publication=$(admin_request node-a 18081 "/api/v1/uploader/submissions/$submission_a/federation-publications" \
  '{"pool_ids":["pool.e2e"]}')
printf '%s' "$publication" | jq -e '.items[0].state == "published"' >/dev/null
federated_release_id=$(printf '%s' "$publication" | jq -r '.items[0].release_id')
D_TOKEN=$(admin_request node-d 18084 /api/v1/auth/tokens '{"name":"negative-soak-node-d"}' | jq -r '.secret')
attempts=0
remote_guid=""
while [ "$attempts" -lt 30 ]; do
  admin_request node-a 18081 /api/v1/admin/gonzbnet/sync/push '{}' >/dev/null
  admin_request node-d 18084 /api/v1/admin/gonzbnet/sync/pull '{}' >/dev/null
  newznab_search 18084 "$D_TOKEN" "Synthetic.Uploader.Soak.A.CC0" "$STATE/responses/node-d-search.xml"
  remote_guid=$(extract_guid "$STATE/responses/node-d-search.xml")
  [ -n "$remote_guid" ] && break
  attempts=$((attempts + 1))
  sleep 1
done
test -n "$remote_guid"
ledger=$(admin_get node-d 18084 "/api/v1/admin/gonzbnet/diagnostics/releases?pool_id=pool.e2e&q=Synthetic.Uploader.Soak.A.CC0&source_kind=local_uploader&state=active")
printf '%s' "$ledger" | jq -e --arg release_id "$federated_release_id" '
  .items | any(.release_id == $release_id and .source_kind == "local_uploader" and .effective_state == "active")' >/dev/null
[ "$(newznab_get_code 18084 "$D_TOKEN" "$remote_guid" "$STATE/responses/node-d-first.nzb")" = "200" ]
remote_sha=$(sha256sum "$STATE/responses/node-d-first.nzb" | awk '{print $1}')
cache_path="$GONZBNET_STATE/node-d/blobs/$remote_guid.nzb"
test -s "$cache_path"

echo "Restarting Node D and reusing the previously issued Newznab download URL"
restart_node node-d 18084 "$ROOT/test/e2e/gonzbnet/node-d.yaml"
"$GONZBNET_SCRIPT" bootstrap >/dev/null
status_code=$(newznab_get_code 18084 "$D_TOKEN" "$remote_guid" "$STATE/responses/node-d-after-restart.nzb")
[ "$status_code" = "200" ] || {
  echo "previously issued Node D download URL returned HTTP $status_code after restart" >&2
  exit 1
}
[ "$(sha256sum "$STATE/responses/node-d-after-restart.nzb" | awk '{print $1}')" = "$remote_sha" ]

echo "Tampering with Node D's filesystem cache and verifying hash-based repair"
printf '%s\n' 'tampered local aggregator cache' >"$cache_path"
status_code=$(newznab_get_code 18084 "$D_TOKEN" "$remote_guid" "$STATE/responses/node-d-repaired.nzb")
[ "$status_code" = "200" ]
[ "$(sha256sum "$STATE/responses/node-d-repaired.nzb" | awk '{print $1}')" = "$remote_sha" ]
[ "$(sha256sum "$cache_path" | awk '{print $1}')" = "$remote_sha" ]
grep -Fq "Rejected cached NZB for $remote_guid" "$GONZBNET_STATE/node-d/stdout.log"

echo "Tampering with Node D's projected title and verifying fail-closed search/get"
db_exec gonzbnet_d "UPDATE federated_release_cards SET title = 'Tampered.Local.Projection' WHERE release_id = '$federated_release_id';"
newznab_search 18084 "$D_TOKEN" "Synthetic.Uploader.Soak.A.CC0" "$STATE/responses/node-d-tampered-search.xml"
if grep -Fq 'Synthetic.Uploader.Soak.A.CC0' "$STATE/responses/node-d-tampered-search.xml"; then
  echo "tampered federated projection remained searchable" >&2
  exit 1
fi
status_code=$(newznab_get_code 18084 "$D_TOKEN" "$remote_guid" "$STATE/responses/node-d-tampered-get.xml")
[ "$status_code" != "200" ]
db_exec gonzbnet_d "
  UPDATE federated_release_cards card
  SET title = source_event.body_json->>'title'
  FROM federated_release_sources source
  JOIN federation_events source_event ON source_event.event_id = source.source_event_id
  WHERE card.release_id = '$federated_release_id'
    AND source.release_id = card.release_id
    AND source_event.event_type = 'ReleaseCard';"
newznab_search 18084 "$D_TOKEN" "Synthetic.Uploader.Soak.A.CC0" "$STATE/responses/node-d-restored-search.xml"
grep -Fq 'Synthetic.Uploader.Soak.A.CC0' "$STATE/responses/node-d-restored-search.xml"

echo "Tampering with Node D's projection and event-body copy together"
db_exec gonzbnet_d "
  UPDATE federation_events event
  SET body_json = jsonb_set(event.body_json, '{title}', to_jsonb('Tampered.Event.Copy'::text), false)
  FROM federated_release_sources source
  WHERE source.release_id = '$federated_release_id'
    AND source.source_event_id = event.event_id;
  UPDATE federated_release_cards
  SET title = 'Tampered.Event.Copy',
      body_json = jsonb_set(body_json, '{title}', to_jsonb('Tampered.Event.Copy'::text), false)
  WHERE release_id = '$federated_release_id';"
newznab_search 18084 "$D_TOKEN" "Synthetic.Uploader.Soak.A.CC0" "$STATE/responses/node-d-event-copy-tampered-search.xml"
if grep -Fq 'Synthetic.Uploader.Soak.A.CC0' "$STATE/responses/node-d-event-copy-tampered-search.xml"; then
  echo "projection plus event-body-copy tampering remained searchable" >&2
  exit 1
fi
status_code=$(newznab_get_code 18084 "$D_TOKEN" "$remote_guid" "$STATE/responses/node-d-event-copy-tampered-get.xml")
[ "$status_code" != "200" ]
db_exec gonzbnet_d "
  UPDATE federation_events event
  SET body_json = event.canonical_event_json::jsonb->'body'
  FROM federated_release_sources source
  WHERE source.release_id = '$federated_release_id'
    AND source.source_event_id = event.event_id;
  UPDATE federated_release_cards card
  SET title = source_event.body_json->>'title',
      body_json = source_event.body_json
  FROM federated_release_sources source
  JOIN federation_events source_event ON source_event.event_id = source.source_event_id
  WHERE card.release_id = '$federated_release_id'
    AND source.release_id = card.release_id
    AND source_event.event_type = 'ReleaseCard';"
newznab_search 18084 "$D_TOKEN" "Synthetic.Uploader.Soak.A.CC0" "$STATE/responses/node-d-event-copy-restored-search.xml"
grep -Fq 'Synthetic.Uploader.Soak.A.CC0' "$STATE/responses/node-d-event-copy-restored-search.xml"

echo "Returning the source submission to pending and waiting for signed withdrawal"
admin_request node-a 18081 "/api/v1/uploader/submissions/$submission_a/actions/return-to-pending" '{}' | \
  jq -e '.submission.state == "pending_review"' >/dev/null
attempts=0
while [ "$attempts" -lt 45 ]; do
  publication_state=$(admin_get node-a 18081 "/api/v1/uploader/submissions/$submission_a/federation-publications" | \
    jq -r '.items[0].state')
  [ "$publication_state" = "withdrawn" ] && break
  attempts=$((attempts + 1))
  sleep 1
done
[ "$publication_state" = "withdrawn" ]
admin_request node-a 18081 /api/v1/admin/gonzbnet/sync/push '{}' >/dev/null
admin_request node-d 18084 /api/v1/admin/gonzbnet/sync/pull '{}' >/dev/null
newznab_search 18084 "$D_TOKEN" "Synthetic.Uploader.Soak.A.CC0" "$STATE/responses/node-d-after-withdrawal.xml"
if grep -Fq 'Synthetic.Uploader.Soak.A.CC0' "$STATE/responses/node-d-after-withdrawal.xml"; then
  echo "withdrawn release remained searchable on Node D" >&2
  exit 1
fi
status_code=$(newznab_get_code 18084 "$D_TOKEN" "$remote_guid" "$STATE/responses/node-d-after-withdrawal-get.xml")
[ "$status_code" != "200" ]

echo "Publishing a corrected ReleaseCard with a fresh lifecycle"
corrected_title="Synthetic.Uploader.Soak.A.Corrected.CC0"
admin_patch node-a 18081 "/api/v1/uploader/submissions/$submission_a" \
  "{\"title\":\"$corrected_title\",\"note\":\"corrected ReleaseCard soak\"}" | \
  jq -e --arg title "$corrected_title" '.submission.title == $title' >/dev/null
admin_request node-a 18081 "/api/v1/uploader/submissions/$submission_a/actions/approve" '{}' | \
  jq -e '.submission.state == "approved"' >/dev/null
corrected_publication=$(admin_request node-a 18081 "/api/v1/uploader/submissions/$submission_a/federation-publications" \
  '{"pool_ids":["pool.e2e"]}')
printf '%s' "$corrected_publication" | jq -e '.items[0].state == "published"' >/dev/null
corrected_release_id=$(printf '%s' "$corrected_publication" | jq -r '.items[0].release_id')
test -n "$corrected_release_id" && test "$corrected_release_id" != "$federated_release_id"

attempts=0
corrected_guid=""
while [ "$attempts" -lt 30 ]; do
  admin_request node-a 18081 /api/v1/admin/gonzbnet/sync/push '{}' >/dev/null
  admin_request node-d 18084 /api/v1/admin/gonzbnet/sync/pull '{}' >/dev/null
  newznab_search 18084 "$D_TOKEN" "$corrected_title" "$STATE/responses/node-d-corrected-search.xml"
  corrected_guid=$(extract_guid "$STATE/responses/node-d-corrected-search.xml")
  [ -n "$corrected_guid" ] && break
  attempts=$((attempts + 1))
  sleep 1
done
test -n "$corrected_guid"
[ "$(newznab_get_code 18084 "$D_TOKEN" "$corrected_guid" "$STATE/responses/node-d-corrected-grab.nzb")" = "200" ]
ledger=$(admin_get node-d 18084 "/api/v1/admin/gonzbnet/diagnostics/releases?pool_id=pool.e2e&q=$corrected_title&source_kind=local_uploader&state=active")
printf '%s' "$ledger" | jq -e --arg release_id "$corrected_release_id" '
  .items | any(.release_id == $release_id and .source_kind == "local_uploader" and .publication_state == "active" and .effective_state == "active")' >/dev/null
ledger=$(admin_get node-d 18084 "/api/v1/admin/gonzbnet/diagnostics/releases?pool_id=pool.e2e&release_id=$federated_release_id&state=withdrawn")
printf '%s' "$ledger" | jq -e --arg release_id "$federated_release_id" '
  .items | any(.release_id == $release_id and .effective_state == "withdrawn")' >/dev/null

echo "Tombstoning the corrected ReleaseCard and checking pool convergence"
tombstone_payload=$(jq -n --arg target_id "$corrected_release_id" \
  '{target_type:"release",target_id:$target_id,pool_id:"pool.e2e",reason:"corrected ReleaseCard governance soak",severity:"hide"}')
admin_request node-a 18081 /api/v1/admin/gonzbnet/moderation/tombstones "$tombstone_payload" | \
  jq -e '.status == "ok" and (.event_id | length > 0)' >/dev/null
admin_request node-a 18081 /api/v1/admin/gonzbnet/sync/push '{}' >/dev/null
admin_request node-d 18084 /api/v1/admin/gonzbnet/sync/pull '{}' >/dev/null
ledger=$(admin_get node-d 18084 "/api/v1/admin/gonzbnet/diagnostics/releases?pool_id=pool.e2e&release_id=$corrected_release_id&state=tombstoned")
printf '%s' "$ledger" | jq -e --arg release_id "$corrected_release_id" '
  .items | any(.release_id == $release_id and .tombstone_severity == "hide" and .effective_state == "tombstoned")' >/dev/null
newznab_search 18084 "$D_TOKEN" "$corrected_title" "$STATE/responses/node-d-after-tombstone.xml"
if grep -Fq "$corrected_title" "$STATE/responses/node-d-after-tombstone.xml"; then
  echo "tombstoned corrected release remained searchable on Node D" >&2
  exit 1
fi

echo "Checking source immutability and secret redaction"
fixture_hashes_after=$(sha256sum "$STATE"/fixtures/[abcd].nzb)
[ "$fixture_hashes_after" = "$fixture_hashes_before" ]
if grep -Fq "$SUBMIT_TOKEN" "$GONZBNET_STATE/node-a/stdout.log" "$GONZBNET_STATE/node-a/gonzb.log" "$STATE"/responses/*.json "$STATE"/responses/*.err 2>/dev/null; then
  echo "submission token leaked into logs or error responses" >&2
  exit 1
fi
if grep -Fq "$READ_TOKEN" "$GONZBNET_STATE/node-a/stdout.log" "$GONZBNET_STATE/node-a/gonzb.log" "$STATE"/responses/*.json "$STATE"/responses/*.err 2>/dev/null; then
  echo "read token leaked into logs or error responses" >&2
  exit 1
fi
if grep -Fq "$D_TOKEN" "$GONZBNET_STATE/node-d/stdout.log" "$GONZBNET_STATE/node-d/gonzb.log" 2>/dev/null; then
  echo "Node D token leaked into logs" >&2
  exit 1
fi

echo "Uploader negative/restart soak passed"
echo "  duplicate deliveries: $SOAK_ITERATIONS plus $RESTART_CYCLES post-restart retries"
echo "  verified: 401, 403, 400, 409, 413, CSRF, outage recovery, interrupted/invalid/oversized inbox input, symlink rejection"
echo "  integrity: restart-stable download URL, cached-NZB hash repair, projection/event-copy tamper rejection, signed withdrawal"
echo "  governance: uploader provenance, corrected republish, fresh lifecycle, pool tombstone convergence"
echo "  synthetic-only: no provider, downloader, torrent, tracker, media payload, or copyrighted fixture"
