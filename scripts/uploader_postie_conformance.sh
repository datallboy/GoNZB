#!/bin/sh
# Optional live conformance test. It posts only locally-authored synthetic text
# to loopback fixtures; it never contacts a Usenet provider or torrent network.
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
STATE="$ROOT/.e2e/uploader-postie"
GONZBNET_STATE="$ROOT/.e2e/gonzbnet"
GONZBNET_SCRIPT="$ROOT/scripts/gonzbnet_e2e.sh"
COMPOSE="$ROOT/docker-compose.gonzbnet-e2e.yml"
COMPOSE_PROJECT="gonzbnet-e2e"
POSTIE_COMMIT="e4da026405f3e6853b60d5907d42a2e8daaf6557"
POSTIE_SOURCE=${POSTIE_SOURCE:-}
KEEP_STATE=${UPLOADER_POSTIE_KEEP_STATE:-0}

POSTIE_PID=""
NNTP_PID=""
PROXY_PID=""

usage() {
  echo "usage: POSTIE_SOURCE=/path/to/postie $0" >&2
  echo "Postie must be checked out cleanly at $POSTIE_COMMIT" >&2
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "$1 is required" >&2
    exit 69
  }
}

stop_pid() {
  pid=$1
  if [ -n "$pid" ]; then
    kill "$pid" 2>/dev/null || true
  fi
}

cleanup() {
  result=$?
  trap - 0 1 2 15
  stop_pid "$POSTIE_PID"
  stop_pid "$PROXY_PID"
  stop_pid "$NNTP_PID"
  if [ "$result" -ne 0 ]; then
    echo "Postie conformance failed; recent fixture logs follow" >&2
    for log in "$STATE/postie.log" "$STATE/postienntp.log" "$STATE/hookproxy.log" "$GONZBNET_STATE/node-a/stdout.log" "$GONZBNET_STATE/node-d/stdout.log"; do
      if [ -f "$log" ]; then
        echo "==> $log <==" >&2
        tail -n 40 "$log" >&2 || true
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

wait_file() {
  path=$1
  attempts=0
  until [ -s "$path" ]; do
    attempts=$((attempts + 1))
    if [ "$attempts" -ge 50 ]; then
      echo "timed out waiting for $path" >&2
      return 1
    fi
    sleep 0.1
  done
}

admin_request() {
  node=$1
  port=$2
  path=$3
  payload=$4
  csrf=$(cat "$GONZBNET_STATE/$node/csrf-token")
  curl --fail-with-body --silent --show-error \
    -b "$GONZBNET_STATE/$node/cookies.txt" \
    -H "X-CSRF-Token: $csrf" -H 'Content-Type: application/json' \
    -d "$payload" "http://127.0.0.1:$port$path"
}

admin_get() {
  node=$1
  port=$2
  path=$3
  curl --fail-with-body --silent --show-error \
    -b "$GONZBNET_STATE/$node/cookies.txt" \
    "http://127.0.0.1:$port$path"
}

db_scalar() {
  database=$1
  query=$2
  docker compose -p "$COMPOSE_PROJECT" -f "$COMPOSE" exec -T postgres \
    psql -U gonzb -d "$database" -Atc "$query"
}

newznab_search() {
  port=$1
  token=$2
  query=$3
  destination=$4
  curl --fail-with-body --silent --show-error --get \
    --data-urlencode 't=search' \
    --data-urlencode "q=$query" \
    --data-urlencode "apikey=$token" \
    "http://127.0.0.1:$port/api" >"$destination"
}

newznab_get() {
  port=$1
  token=$2
  release_id=$3
  destination=$4
  curl --fail-with-body --silent --show-error --get \
    --data-urlencode 't=get' \
    --data-urlencode "id=$release_id" \
    --data-urlencode "apikey=$token" \
    "http://127.0.0.1:$port/api" >"$destination"
}

extract_guid() {
  sed -n 's:.*<guid isPermaLink="false">\([^<]*\)</guid>.*:\1:p' "$1" | head -n 1
}

if [ -z "$POSTIE_SOURCE" ]; then
  usage
  exit 64
fi

for command in curl docker git go jq sed sha256sum; do
  require_command "$command"
done

if [ ! -d "$POSTIE_SOURCE/.git" ]; then
  echo "POSTIE_SOURCE is not a Postie git checkout: $POSTIE_SOURCE" >&2
  exit 66
fi
actual_commit=$(git -C "$POSTIE_SOURCE" rev-parse HEAD)
if [ "$actual_commit" != "$POSTIE_COMMIT" ]; then
  echo "Postie checkout is $actual_commit; expected $POSTIE_COMMIT" >&2
  exit 65
fi
if [ -n "$(git -C "$POSTIE_SOURCE" status --porcelain)" ]; then
  echo "Postie checkout must be clean for a reproducible conformance run" >&2
  exit 65
fi

trap cleanup 0 1 2 15
"$GONZBNET_SCRIPT" reset >/dev/null 2>&1 || true
rm -rf "$STATE"
mkdir -p "$STATE/watch" "$STATE/output" "$STATE/bin" "$STATE/postie-data"

echo "Starting the four-node GoNZBNet fixture with uploader intake on Node A"
"$GONZBNET_SCRIPT" start
"$GONZBNET_SCRIPT" bootstrap
"$GONZBNET_SCRIPT" configure-pool
curl -fsS http://127.0.0.1:18081/readyz | jq -e \
  '.modules.uploader.enabled == true and .modules.uploader.ready == true' >/dev/null

echo "Creating a least-privilege Postie submission identity"
admin_request node-a 18081 /api/v1/admin/auth/roles \
  '{"id":"postie-submit","name":"Postie submit only","permissions":["uploader.submissions.create"]}' >/dev/null
admin_request node-a 18081 /api/v1/admin/auth/users \
  '{"id":"postie-conformance","username":"postie-conformance","password":"postie-conformance-local-2026","enabled":true,"role_ids":["postie-submit"]}' >/dev/null
POSTIE_TOKEN=$(admin_request node-a 18081 /api/v1/admin/auth/tokens \
  '{"user_id":"postie-conformance","name":"postie-conformance"}' | jq -r '.secret')
test -n "$POSTIE_TOKEN" && test "$POSTIE_TOKEN" != "null"

forbidden_code=$(curl --silent --show-error -o "$STATE/least-privilege-response.json" -w '%{http_code}' \
  -H "Authorization: Bearer $POSTIE_TOKEN" \
  http://127.0.0.1:18081/api/v1/uploader/submissions)
[ "$forbidden_code" = "403" ] || {
  echo "Postie token unexpectedly gained uploader read access (HTTP $forbidden_code)" >&2
  exit 1
}

echo "Building pinned Postie and loopback-only protocol fixtures"
(cd "$ROOT" && GOCACHE="${GOCACHE:-/tmp/gocache}" go build -o "$STATE/bin/postienntpfixture" ./test/e2e/uploader/postienntpfixture)
(cd "$ROOT" && GOCACHE="${GOCACHE:-/tmp/gocache}" go build -o "$STATE/bin/hookproxy" ./test/e2e/uploader/hookproxy)
(cd "$POSTIE_SOURCE" && GOCACHE="${GOCACHE:-/tmp/gocache}" go build -o "$STATE/bin/postie" ./cmd/postie)

"$STATE/bin/postienntpfixture" \
  -listen 127.0.0.1:11120 \
  -capture "$STATE/articles.jsonl" \
  -ready-file "$STATE/postienntp.ready" \
  >"$STATE/postienntp.log" 2>&1 &
NNTP_PID=$!
wait_file "$STATE/postienntp.ready"

"$STATE/bin/hookproxy" \
  -listen 127.0.0.1:18091 \
  -target http://127.0.0.1:18081 \
  -failures 2 \
  -ready-file "$STATE/hookproxy.ready" \
  >"$STATE/hookproxy.log" 2>&1 &
PROXY_PID=$!
wait_file "$STATE/hookproxy.ready"

cat >"$STATE/postie.yaml" <<EOF
version: 2
servers:
  - name: gonzb-loopback-conformance
    host: 127.0.0.1
    port: 11120
    username: ""
    password: ""
    ssl: false
    max_connections: 2
    enabled: true
    role: upload
    inflight: 1
connection_pool:
  min_connections: 0
  health_check_interval: 1m
posting:
  wait_for_par2: true
  max_retries: 1
  retry_delay: 100ms
  article_size_in_bytes: 256
  upload_buffer_memory_limit: 1048576
  groups:
    - name: alt.binaries.gonzb.synthetic
      enabled: true
  throttle_rate: 0
  message_id_format: random
  obfuscation_policy: none
  par2_obfuscation_policy: none
  group_policy: all
  post_headers:
    add_nxg_header: false
    default_from: synthetic-conformance@example.invalid
post_check:
  enabled: true
  delay: 100ms
  max_reposts: 1
  deferred_check_delay: 100ms
  deferred_max_retries: 2
  deferred_max_backoff: 1s
  deferred_check_interval: 1s
  deferred_batch_size: 100
  stat_batch_size: 20
  max_concurrent_checks: 1
par2:
  enabled: false
  redundancy: 5%
  temp_dir: "$STATE/postie-data"
  maintain_par2_files: false
  skip_if_par2_exists: true
  max_concurrent_jobs: 1
watchers:
  - name: gonzb-conformance
    enabled: true
    watch_directory: "$STATE/watch"
    size_threshold: 1
    schedule: { start_time: "00:00", end_time: "23:59" }
    ignore_patterns: ["*.tmp", "*.part"]
    min_file_size: 1
    check_interval: 250ms
    delete_original_file: false
    single_nzb_per_folder: false
    follow_symlinks: false
    min_file_age: 1ms
    min_file_age_to_delete: 0s
nzb_compression: { enabled: false, type: none, level: 0 }
database:
  database_type: sqlite
  database_path: "$STATE/postie-data/postie.db"
queue: { max_concurrent_uploads: 1, min_size_to_start: 0 }
output_dir: "$STATE/output"
maintain_original_extension: true
post_upload_script:
  enabled: true
  command: '$ROOT/scripts/gonzb-submit-nzb.sh "{nzb_path}"'
  timeout: 30s
  max_retries: 3
  retry_delay: 1s
  max_backoff: 2s
  max_retry_duration: 1m
  retry_check_interval: 1s
EOF

cat >"$STATE/watch/Synthetic.Postie.Conformance.CC0.txt" <<'EOF'
GoNZB and Postie local conformance payload.

This synthetic text was created solely for protocol integration testing.
To the extent possible under law, its author dedicates it to the public domain
under CC0 1.0. It contains no third-party media or copyrighted sample data.

Segment one: integrity is checked by signed manifests and content hashes.
Segment two: search and grab are exercised across disposable local nodes.
Segment three: all NNTP and HTTP endpoints in this test bind to loopback.
EOF

# Postie's watcher requires two stable-size observations.
sleep 1
echo "Running Postie watch mode through two injected HTTP 503 responses"
env GONZB_URL=http://127.0.0.1:18091 GONZB_TOKEN="$POSTIE_TOKEN" \
  "$STATE/bin/postie" watch \
  --config "$STATE/postie.yaml" \
  --dir "$STATE/watch" \
  --output-dir "$STATE/output" \
  >"$STATE/postie.log" 2>&1 &
POSTIE_PID=$!

attempts=0
submission_id=""
while [ "$attempts" -lt 120 ]; do
  submission_json=$(admin_get node-a 18081 '/api/v1/uploader/submissions?state=pending_review&limit=10')
  submission_id=$(printf '%s' "$submission_json" | jq -r '.items[0].id // empty')
  [ -n "$submission_id" ] && break
  kill -0 "$POSTIE_PID" 2>/dev/null || {
    echo "Postie exited before submitting its generated NZB" >&2
    exit 1
  }
  attempts=$((attempts + 1))
  sleep 1
done
[ -n "$submission_id" ] || {
  echo "timed out waiting for Postie's uploader submission" >&2
  exit 1
}

nzb_path=$(find "$STATE/output" -type f -name '*.nzb' -print | head -n 1)
test -n "$nzb_path" && test -s "$nzb_path"
submission_json=$(admin_get node-a 18081 "/api/v1/uploader/submissions/$submission_id")
title=$(printf '%s' "$submission_json" | jq -r '.submission.title')
segment_count=$(printf '%s' "$submission_json" | jq -r '.submission.segment_count')
printf '%s' "$submission_json" | jq -e \
  '.submission.state == "pending_review"
   and .submission.intake_kind == "http"
   and .submission.submitted_by == "postie-conformance"
   and .submission.file_count == 1
   and (.submission.groups | index("alt.binaries.gonzb.synthetic") != null)' >/dev/null

captured_count=$(jq -s 'length' "$STATE/articles.jsonl")
[ "$captured_count" = "$segment_count" ] || {
  echo "captured article count $captured_count differs from NZB segment count $segment_count" >&2
  exit 1
}
jq -e -s \
  'length > 0
   and all(.[]; .body_bytes > 0
     and .yenc_name == "Synthetic.Postie.Conformance.CC0.txt"
     and (.newsgroups | index("alt.binaries.gonzb.synthetic") != null)
     and (.article_sha256 | length) == 64
     and (.body_sha256 | length) == 64)' \
  "$STATE/articles.jsonl" >/dev/null
while IFS= read -r message_id; do
  plain_id=${message_id#<}
  plain_id=${plain_id%>}
  grep -Fq "$plain_id" "$nzb_path" || {
    echo "Postie NZB is missing captured message ID $message_id" >&2
    exit 1
  }
done <<EOF
$(jq -r '.message_id' "$STATE/articles.jsonl")
EOF

proxy_stats=$(curl -fsS http://127.0.0.1:18091/stats)
printf '%s' "$proxy_stats" | jq -e \
  '.requests >= 3 and .injected_failures == 2 and .forwarded == 1' >/dev/null

echo "Verifying exact-content deduplication through the same helper"
dedupe_json=$(GONZB_URL=http://127.0.0.1:18081 GONZB_TOKEN="$POSTIE_TOKEN" \
  "$ROOT/scripts/gonzb-submit-nzb.sh" "$nzb_path")
printf '%s' "$dedupe_json" | jq -e \
  --arg id "$submission_id" '.created == false and .submission.id == $id' >/dev/null

echo "Approving the Postie submission and checking Node A search/get"
approved_json=$(admin_request node-a 18081 "/api/v1/uploader/submissions/$submission_id/actions/approve" '{}')
printf '%s' "$approved_json" | jq -e '.submission.state == "approved"' >/dev/null
A_TOKEN=$(admin_request node-a 18081 /api/v1/auth/tokens '{"name":"postie-conformance-search-a"}' | jq -r '.secret')
test -n "$A_TOKEN" && test "$A_TOKEN" != "null"

newznab_search 18081 "$A_TOKEN" "$title" "$STATE/node-a-search.xml"
grep -Fq "$title" "$STATE/node-a-search.xml"
local_guid=$(extract_guid "$STATE/node-a-search.xml")
test -n "$local_guid"
newznab_get 18081 "$A_TOKEN" "$local_guid" "$STATE/node-a-grab.nzb"
expected_sha=$(printf '%s' "$submission_json" | jq -r '.submission.nzb_sha256')
local_sha=$(sha256sum "$STATE/node-a-grab.nzb" | awk '{print $1}')
[ "$local_sha" = "$expected_sha" ] || {
  echo "Node A returned NZB hash $local_sha; expected $expected_sha" >&2
  exit 1
}

echo "Publishing explicitly to pool.e2e and checking Node D search/grab"
admin_get node-a 18081 /api/v1/uploader/federation-pools | \
  jq -e '.items | index("pool.e2e") != null' >/dev/null
publication_json=$(admin_request node-a 18081 "/api/v1/uploader/submissions/$submission_id/federation-publications" \
  '{"pool_ids":["pool.e2e"]}')
printf '%s' "$publication_json" | jq -e '.items[0].state == "published"' >/dev/null
release_id=$(printf '%s' "$publication_json" | jq -r '.items[0].release_id')
manifest_id=$(printf '%s' "$publication_json" | jq -r '.items[0].manifest_id')
test -n "$release_id" && test "$release_id" != "null"
test -n "$manifest_id" && test "$manifest_id" != "null"

event_count=$(db_scalar gonzbnet_a "
  SELECT count(*) FROM federation_events
  WHERE pool_ids @> '[\"pool.e2e\"]'::jsonb
    AND event_type = 'ReleaseCard'
    AND body_json->>'release_id' = '$release_id'
    AND validation_status = 'accepted'")
[ "$event_count" = "1" ] || {
  echo "expected one accepted uploader ReleaseCard on Node A, got $event_count" >&2
  exit 1
}

D_TOKEN=$(admin_request node-d 18084 /api/v1/auth/tokens '{"name":"postie-conformance-search-d"}' | jq -r '.secret')
test -n "$D_TOKEN" && test "$D_TOKEN" != "null"
attempts=0
remote_guid=""
while [ "$attempts" -lt 60 ]; do
  admin_request node-a 18081 /api/v1/admin/gonzbnet/sync/push '{}' >/dev/null
  admin_request node-d 18084 /api/v1/admin/gonzbnet/sync/pull '{}' >/dev/null
  newznab_search 18084 "$D_TOKEN" "$title" "$STATE/node-d-search.xml"
  if grep -Fq "$title" "$STATE/node-d-search.xml"; then
    remote_guid=$(extract_guid "$STATE/node-d-search.xml")
    [ -n "$remote_guid" ] && break
  fi
  attempts=$((attempts + 1))
  sleep 1
done
[ -n "$remote_guid" ] || {
  echo "Node D did not return the Postie uploader release" >&2
  exit 1
}

newznab_get 18084 "$D_TOKEN" "$remote_guid" "$STATE/node-d-first-grab.nzb"
while IFS= read -r message_id; do
  plain_id=${message_id#<}
  plain_id=${plain_id%>}
  grep -Fq "$plain_id" "$STATE/node-d-first-grab.nzb" || {
    echo "Node D NZB is missing captured message ID $message_id" >&2
    exit 1
  }
done <<EOF
$(jq -r '.message_id' "$STATE/articles.jsonl")
EOF
newznab_get 18084 "$D_TOKEN" "$remote_guid" "$STATE/node-d-second-grab.nzb"
cmp "$STATE/node-d-first-grab.nzb" "$STATE/node-d-second-grab.nzb"

cached=$(db_scalar gonzbnet_d "
  SELECT count(*)
  FROM resolution_manifests cached
  JOIN federation_events source ON source.event_id = cached.source_event_id
  WHERE cached.manifest_id = '$manifest_id'
    AND cached.validation_status = 'accepted'
    AND source.event_type = 'ResolutionManifest'
    AND source.pool_ids @> '[\"pool.e2e\"]'::jsonb")
[ "$cached" = "1" ] || {
  echo "Node D did not retain one accepted signed manifest for $manifest_id" >&2
  exit 1
}

echo "Postie conformance passed"
echo "  pinned commit: $POSTIE_COMMIT"
echo "  synthetic articles: $captured_count"
echo "  uploader submission: $submission_id"
echo "  federated release: $release_id"
echo "  manifest: $manifest_id"
echo "  verified: transient hook retry, dedupe, approval, Node A search/get, pool publication, Node D search/grab/cache"
