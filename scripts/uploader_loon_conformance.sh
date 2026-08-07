#!/bin/sh
# Optional live conformance test. It runs Loon's real offline watch-folder
# pipeline against loopback fixtures and feeds the nested completed NZB tree to
# GoNZB's read-only inbox. No torrent, tracker, provider, or external HTTP
# endpoint is contacted.
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
STATE="$ROOT/.e2e/uploader-loon"
GONZBNET_STATE="$ROOT/.e2e/gonzbnet"
GONZBNET_SCRIPT="$ROOT/scripts/gonzbnet_e2e.sh"
LOON_COMMIT="2c8982dc6371d0e3cf817bb78c07396db77a4b03"
LOON_SOURCE=${LOON_SOURCE:-}
KEEP_STATE=${UPLOADER_LOON_KEEP_STATE:-0}

LOON_PID=""
NNTP_PID=""

usage() {
  echo "usage: LOON_SOURCE=/path/to/loon-agent $0" >&2
  echo "loon-agent must be checked out cleanly at $LOON_COMMIT" >&2
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
    wait "$pid" 2>/dev/null || true
  fi
}

cleanup() {
  result=$?
  trap - 0 1 2 15
  stop_pid "$LOON_PID"
  stop_pid "$NNTP_PID"
  if [ "$result" -ne 0 ]; then
    echo "Loon conformance failed; recent fixture logs follow" >&2
    for log in "$STATE/loon.log" "$STATE/postingnntp.log" "$GONZBNET_STATE/node-a/stdout.log"; do
      if [ -f "$log" ]; then
        echo "==> $log <==" >&2
        tail -n 60 "$log" >&2 || true
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
    if [ "$attempts" -ge 100 ]; then
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

newznab_search() {
  token=$1
  query=$2
  destination=$3
  curl --fail-with-body --silent --show-error --get \
    --data-urlencode 't=search' \
    --data-urlencode "q=$query" \
    --data-urlencode "apikey=$token" \
    "http://127.0.0.1:18081/api" >"$destination"
}

newznab_get() {
  token=$1
  release_id=$2
  destination=$3
  curl --fail-with-body --silent --show-error --get \
    --data-urlencode 't=get' \
    --data-urlencode "id=$release_id" \
    --data-urlencode "apikey=$token" \
    "http://127.0.0.1:18081/api" >"$destination"
}

extract_guid() {
  sed -n 's:.*<guid isPermaLink="false">\([^<]*\)</guid>.*:\1:p' "$1" | head -n 1
}

start_loon() {
  env \
    SITE_URL=http://127.0.0.1:19092 \
    AGENT_TOKEN=synthetic-loopback-token \
    POLL_INTERVAL=1 \
    TEMP_DIR="$STATE/loon-temp" \
    CONFIG_DIR="$STATE/loon-config" \
    OFFLINE_OUTPUT_DIR="$STATE/offline-output" \
    NNTP_SERVER=127.0.0.1:11121 \
    NNTP_SSL=false \
    NNTP_CONNECTIONS=2 \
    NNTP_USER=synthetic-loon \
    NNTP_PASS=synthetic-loopback-nntp-password \
    NNTP_GROUP=alt.binaries.gonzb.synthetic \
    NNTP_POSTER=synthetic-loon \
    NNTP_FROM=synthetic-loon@example.invalid \
    NNTP_DOMAIN=example.invalid \
    PAR2_REDUNDANCY=1 \
    OBFUSCATE=false \
    ENCRYPT=false \
    OFFER_ENABLED=false \
    HTTP_PROXY=http://127.0.0.1:9 \
    HTTPS_PROXY=http://127.0.0.1:9 \
    NO_PROXY=127.0.0.1,localhost \
    "$STATE/bin/loon-agent" >>"$STATE/loon.log" 2>&1 &
  LOON_PID=$!
}

if [ -z "$LOON_SOURCE" ]; then
  usage
  exit 64
fi

for command in curl docker git go jq sed sha256sum sqlite3; do
  require_command "$command"
done

if [ ! -d "$LOON_SOURCE/.git" ]; then
  echo "LOON_SOURCE is not a loon-agent git checkout: $LOON_SOURCE" >&2
  exit 66
fi
actual_commit=$(git -C "$LOON_SOURCE" rev-parse HEAD)
if [ "$actual_commit" != "$LOON_COMMIT" ]; then
  echo "loon-agent checkout is $actual_commit; expected $LOON_COMMIT" >&2
  exit 65
fi
if [ -n "$(git -C "$LOON_SOURCE" status --porcelain)" ]; then
  echo "loon-agent checkout must be clean for a reproducible conformance run" >&2
  exit 65
fi

trap cleanup 0 1 2 15
"$GONZBNET_SCRIPT" reset >/dev/null 2>&1 || true
rm -rf "$STATE"
mkdir -p "$STATE/bin" "$STATE/input" "$STATE/loon-temp" "$STATE/loon-config" "$STATE/offline-output"

# The standard four-node fixture keeps its inbox disabled. Generate a
# disposable Node A variant so this test exercises the real runtime scanner
# without changing the default E2E topology.
sed "s|  inbox: { enabled: false }|  inbox: { enabled: true, path: \"$STATE/offline-output\", scan_interval_seconds: 1, settle_age_seconds: 1 }|" \
  "$ROOT/test/e2e/gonzbnet/node-a.yaml" >"$STATE/node-a.yaml"
grep -Fq 'inbox: { enabled: true' "$STATE/node-a.yaml"

echo "Starting GoNZB with Loon's completed-output tree as its recursive inbox"
GONZBNET_NODE_A_CONFIG="$STATE/node-a.yaml" "$GONZBNET_SCRIPT" start
"$GONZBNET_SCRIPT" bootstrap
curl -fsS http://127.0.0.1:18081/readyz | jq -e \
  '.modules.uploader.enabled == true and .modules.uploader.ready == true' >/dev/null

echo "Building pinned Loon and the loopback write-capable NNTP fixture"
(cd "$ROOT" && GOCACHE="${GOCACHE:-/tmp/gocache}" go build -o "$STATE/bin/postingnntpfixture" ./test/e2e/uploader/postienntpfixture)
(cd "$LOON_SOURCE" && GOCACHE="${GOCACHE:-/tmp/gocache}" go build -mod=readonly -o "$STATE/bin/loon-agent" ./cmd/agent)

"$STATE/bin/postingnntpfixture" \
  -listen 127.0.0.1:11121 \
  -capture "$STATE/articles.jsonl" \
  -ready-file "$STATE/postingnntp.ready" \
  >"$STATE/postingnntp.log" 2>&1 &
NNTP_PID=$!
wait_file "$STATE/postingnntp.ready"

echo "Initializing Loon's service database and configuring an offline watch"
start_loon
attempts=0
until [ "$(sqlite3 "$STATE/loon-temp/agent.db" 'SELECT COUNT(*) FROM schema_migrations;' 2>/dev/null || true)" = "6" ]; do
  attempts=$((attempts + 1))
  if [ "$attempts" -ge 100 ]; then
    echo "timed out waiting for Loon database migrations" >&2
    exit 1
  fi
  sleep 0.1
done
stop_pid "$LOON_PID"
LOON_PID=""

sqlite3 "$STATE/loon-temp/agent.db" <<EOF
INSERT INTO groups (
  name, newsgroups_json, screenshots, par2_redundancy, obfuscate, source,
  watermark_text, type, banned_extensions_json, sample_seconds
) VALUES (
  'GoNZB Synthetic', '["alt.binaries.gonzb.synthetic"]', 0, 1, 0, 'local',
  '', 'other', '[]', 0
);
INSERT INTO watch_folders (path, group_id, enabled)
VALUES ('$STATE/input', (SELECT id FROM groups WHERE name = 'GoNZB Synthetic'), 1);
EOF

cat >"$STATE/input/Synthetic.Loon.Conformance.CC0.txt" <<'EOF'
GoNZB and Loon local conformance payload.

This synthetic text was created solely for protocol integration testing.
To the extent possible under law, its author dedicates it to the public domain
under CC0 1.0. It contains no third-party media or copyrighted sample data.

Loon reads this stable local file, posts it only to a loopback NNTP fixture,
and writes the completed NZB into its nested offline output directory.
GoNZB reads only that completed output tree; it never receives this source path.
EOF
touch -d '2 minutes ago' "$STATE/input/Synthetic.Loon.Conformance.CC0.txt"
source_sha_before=$(sha256sum "$STATE/input/Synthetic.Loon.Conformance.CC0.txt" | awk '{print $1}')

echo "Running Loon's real service-mode watcher and offline posting pipeline"
start_loon
attempts=0
while [ "$attempts" -lt 150 ]; do
  job_status=$(sqlite3 "$STATE/loon-temp/agent.db" "SELECT status FROM offline_jobs ORDER BY id DESC LIMIT 1;" 2>/dev/null || true)
  case "$job_status" in
    completed)
      break
      ;;
    failed)
      job_error=$(sqlite3 "$STATE/loon-temp/agent.db" "SELECT COALESCE(error, '') FROM offline_jobs ORDER BY id DESC LIMIT 1;")
      echo "Loon offline job failed: $job_error" >&2
      exit 1
      ;;
  esac
  attempts=$((attempts + 1))
  sleep 1
done
[ "$job_status" = "completed" ] || {
  echo "timed out waiting for Loon's offline job" >&2
  exit 1
}
nzb_path=$(sqlite3 "$STATE/loon-temp/agent.db" "SELECT nzb_path FROM offline_jobs ORDER BY id DESC LIMIT 1;")
test -s "$nzb_path"
case "$nzb_path" in
  "$STATE/offline-output"/*/*/*.nzb) ;;
  *)
    echo "Loon wrote its NZB outside the expected nested offline-output layout: $nzb_path" >&2
    exit 1
    ;;
esac
stop_pid "$LOON_PID"
LOON_PID=""

source_sha_after=$(sha256sum "$STATE/input/Synthetic.Loon.Conformance.CC0.txt" | awk '{print $1}')
[ "$source_sha_after" = "$source_sha_before" ] || {
  echo "Loon modified its watched source file" >&2
  exit 1
}
[ "$(sqlite3 "$STATE/loon-temp/agent.db" 'SELECT COUNT(*) FROM offline_jobs;')" = "1" ] || {
  echo "Loon's watcher created duplicate jobs for one source path" >&2
  exit 1
}
if find "$STATE/offline-output" -type f \( -name '*.torrent' -o -name '*.magnet' \) | grep -q .; then
  echo "Loon's offline output unexpectedly contains torrent handoff data" >&2
  exit 1
fi

nzb_sha_before=$(sha256sum "$nzb_path" | awk '{print $1}')
nzb_mtime_before=$(stat -c '%Y' "$nzb_path")

echo "Waiting for GoNZB's recursive inbox to ingest Loon's nested NZB"
attempts=0
submission_id=""
while [ "$attempts" -lt 60 ]; do
  submission_list=$(admin_get node-a 18081 '/api/v1/uploader/submissions?state=pending_review&limit=10')
  submission_id=$(printf '%s' "$submission_list" | jq -r '.items[0].id // empty')
  [ -n "$submission_id" ] && break
  attempts=$((attempts + 1))
  sleep 1
done
[ -n "$submission_id" ] || {
  echo "GoNZB did not ingest Loon's completed NZB" >&2
  exit 1
}
sleep 2
submission_list=$(admin_get node-a 18081 '/api/v1/uploader/submissions?state=pending_review&limit=10')
printf '%s' "$submission_list" | jq -e '.count == 1 and (.items | length) == 1' >/dev/null
submission_json=$(admin_get node-a 18081 "/api/v1/uploader/submissions/$submission_id")
printf '%s' "$submission_json" | jq -e \
  '.submission.state == "pending_review"
   and .submission.intake_kind == "inbox"
   and .submission.submitted_by == "system:inbox"
   and .submission.title == "Synthetic.Loon.Conformance.CC0"
   and .submission.file_count >= 1
   and (.submission.groups | index("alt.binaries.gonzb.synthetic") != null)' >/dev/null

captured_count=$(jq -s 'length' "$STATE/articles.jsonl")
segment_count=$(printf '%s' "$submission_json" | jq -r '.submission.segment_count')
[ "$captured_count" = "$segment_count" ] || {
  echo "captured article count $captured_count differs from NZB segment count $segment_count" >&2
  exit 1
}
captured_payload_bytes=$(jq -s 'map(.yenc_part_bytes) | add' "$STATE/articles.jsonl")
submission_size=$(printf '%s' "$submission_json" | jq -r '.submission.size_bytes')
[ "$captured_payload_bytes" = "$submission_size" ] || {
  echo "captured yEnc payload bytes $captured_payload_bytes differ from NZB-derived size $submission_size" >&2
  exit 1
}
jq -e -s \
  'length > 0
   and all(.[]; .yenc_part_bytes > 0
     and .body_bytes > .yenc_part_bytes
     and (.newsgroups | index("alt.binaries.gonzb.synthetic") != null)
     and (.article_sha256 | length) == 64
     and (.body_sha256 | length) == 64)' \
  "$STATE/articles.jsonl" >/dev/null
while IFS= read -r message_id; do
  plain_id=${message_id#<}
  plain_id=${plain_id%>}
  grep -Fq "$plain_id" "$nzb_path" || {
    echo "Loon NZB is missing captured message ID $message_id" >&2
    exit 1
  }
done <<EOF
$(jq -r '.message_id' "$STATE/articles.jsonl")
EOF

nzb_sha_after=$(sha256sum "$nzb_path" | awk '{print $1}')
nzb_mtime_after=$(stat -c '%Y' "$nzb_path")
[ "$nzb_sha_after" = "$nzb_sha_before" ] && [ "$nzb_mtime_after" = "$nzb_mtime_before" ] || {
  echo "GoNZB mutated Loon's completed NZB" >&2
  exit 1
}
if grep -Fq "$STATE/input" "$GONZBNET_STATE/node-a/stdout.log"; then
  echo "Loon's private source path leaked into GoNZB logs" >&2
  exit 1
fi
if grep -Fq 'synthetic-loopback-nntp-password' "$STATE/loon.log" "$STATE/postingnntp.log" "$GONZBNET_STATE/node-a/stdout.log"; then
  echo "Loon's NNTP password leaked into logs" >&2
  exit 1
fi

echo "Approving the Loon submission and checking aggregator/Newznab search and get"
approved_json=$(admin_request node-a 18081 "/api/v1/uploader/submissions/$submission_id/actions/approve" '{}')
printf '%s' "$approved_json" | jq -e '.submission.state == "approved"' >/dev/null
search_token=$(admin_request node-a 18081 /api/v1/auth/tokens '{"name":"loon-conformance-search"}' | jq -r '.secret')
test -n "$search_token" && test "$search_token" != "null"

newznab_search "$search_token" "Synthetic.Loon.Conformance.CC0" "$STATE/search.xml"
grep -Fq "Synthetic.Loon.Conformance.CC0" "$STATE/search.xml"
release_id=$(extract_guid "$STATE/search.xml")
test -n "$release_id"
newznab_get "$search_token" "$release_id" "$STATE/grab.nzb"
expected_sha=$(printf '%s' "$submission_json" | jq -r '.submission.nzb_sha256')
grab_sha=$(sha256sum "$STATE/grab.nzb" | awk '{print $1}')
[ "$grab_sha" = "$expected_sha" ] || {
  echo "Newznab returned NZB hash $grab_sha; expected $expected_sha" >&2
  exit 1
}

echo "Returning the Loon submission to pending and checking search withdrawal"
pending_json=$(admin_request node-a 18081 "/api/v1/uploader/submissions/$submission_id/actions/return-to-pending" '{}')
printf '%s' "$pending_json" | jq -e '.submission.state == "pending_review"' >/dev/null
newznab_search "$search_token" "Synthetic.Loon.Conformance.CC0" "$STATE/search-after-withdrawal.xml"
if grep -Fq "Synthetic.Loon.Conformance.CC0" "$STATE/search-after-withdrawal.xml"; then
  echo "returned-to-pending Loon submission remained searchable" >&2
  exit 1
fi

if [ -n "$(git -C "$LOON_SOURCE" status --porcelain)" ]; then
  echo "the conformance run modified the pinned Loon checkout" >&2
  exit 1
fi

echo "Loon conformance passed"
echo "  pinned commit: $LOON_COMMIT"
echo "  synthetic articles: $captured_count"
echo "  uploader submission: $submission_id"
echo "  handoff: nested OFFLINE_OUTPUT_DIR through GoNZB's read-only recursive inbox"
echo "  verified: service watcher, POST, NZB metadata, source/output immutability, dedupe, approval, Newznab search/get, withdrawal"
echo "  topology note: separate servers still require a shared read-only mount or the deferred gonzb-nzb-forwarder"
