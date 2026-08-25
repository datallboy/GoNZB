#!/bin/sh
# Optional live conformance test. It posts only locally-authored synthetic text
# to loopback fixtures; it never contacts a Usenet provider or torrent network.
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
STATE="$ROOT/.e2e/uploader-pesto"
GONZBNET_STATE="$ROOT/.e2e/gonzbnet"
GONZBNET_SCRIPT="$ROOT/scripts/gonzbnet_e2e.sh"
PESTO_COMMIT="b9e2d8a41ddfddb2dd0d0954a5984114b3553636"
PESTO_SOURCE=${PESTO_SOURCE:-}
PESTO_RUST_TOOLCHAIN=${PESTO_RUST_TOOLCHAIN:-1.96.0}
KEEP_STATE=${UPLOADER_PESTO_KEEP_STATE:-0}

NNTP_PID=""
PROXY_PID=""

usage() {
  echo "usage: PESTO_SOURCE=/path/to/pesto $0" >&2
  echo "pesto must be checked out cleanly at $PESTO_COMMIT" >&2
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
  stop_pid "$PROXY_PID"
  stop_pid "$NNTP_PID"
  if [ "$result" -ne 0 ]; then
    echo "pesto conformance failed; recent fixture logs follow" >&2
    for log in "$STATE/pesto.log" "$STATE/postingnntp.log" "$STATE/hookproxy.log" "$GONZBNET_STATE/node-a/stdout.log"; do
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

if [ -z "$PESTO_SOURCE" ]; then
  usage
  exit 64
fi

for command in curl docker git go jq rustup sed sha256sum; do
  require_command "$command"
done

if [ ! -d "$PESTO_SOURCE/.git" ]; then
  echo "PESTO_SOURCE is not a pesto git checkout: $PESTO_SOURCE" >&2
  exit 66
fi
actual_commit=$(git -C "$PESTO_SOURCE" rev-parse HEAD)
if [ "$actual_commit" != "$PESTO_COMMIT" ]; then
  echo "pesto checkout is $actual_commit; expected $PESTO_COMMIT" >&2
  exit 65
fi
if [ -n "$(git -C "$PESTO_SOURCE" status --porcelain)" ]; then
  echo "pesto checkout must be clean for a reproducible conformance run" >&2
  exit 65
fi
if ! rustup run "$PESTO_RUST_TOOLCHAIN" rustc --version >/dev/null 2>&1; then
  echo "Rust toolchain $PESTO_RUST_TOOLCHAIN is required; install it with rustup" >&2
  exit 69
fi

trap cleanup 0 1 2 15
"$GONZBNET_SCRIPT" reset >/dev/null 2>&1 || true
rm -rf "$STATE"
mkdir -p "$STATE/input" "$STATE/output" "$STATE/bin" "$STATE/pesto-config/pesto"

echo "Starting the GoNZB fixture with uploader intake on Node A"
"$GONZBNET_SCRIPT" start
"$GONZBNET_SCRIPT" bootstrap
curl -fsS http://127.0.0.1:18081/readyz | jq -e \
  '.modules.uploader.enabled == true and .modules.uploader.ready == true' >/dev/null

echo "Creating a least-privilege pesto submission identity"
admin_request node-a 18081 /api/v1/admin/auth/roles \
  '{"id":"pesto-submit","name":"pesto submit only","permissions":["uploader.submissions.create"]}' >/dev/null
admin_request node-a 18081 /api/v1/admin/auth/users \
  '{"id":"pesto-conformance","username":"pesto-conformance","password":"pesto-conformance-local-2026","enabled":true,"role_ids":["pesto-submit"]}' >/dev/null
PESTO_TOKEN=$(admin_request node-a 18081 /api/v1/admin/auth/tokens \
  '{"user_id":"pesto-conformance","name":"pesto-conformance"}' | jq -r '.secret')
test -n "$PESTO_TOKEN" && test "$PESTO_TOKEN" != "null"

forbidden_code=$(curl --silent --show-error -o "$STATE/least-privilege-response.json" -w '%{http_code}' \
  -H "Authorization: Bearer $PESTO_TOKEN" \
  http://127.0.0.1:18081/api/v1/uploader/submissions)
[ "$forbidden_code" = "403" ] || {
  echo "pesto token unexpectedly gained uploader read access (HTTP $forbidden_code)" >&2
  exit 1
}

echo "Building pinned pesto and loopback-only protocol fixtures"
(cd "$ROOT" && GOCACHE="${GOCACHE:-/tmp/gocache}" go build -o "$STATE/bin/postingnntpfixture" ./test/e2e/uploader/postienntpfixture)
(cd "$ROOT" && GOCACHE="${GOCACHE:-/tmp/gocache}" go build -o "$STATE/bin/hookproxy" ./test/e2e/uploader/hookproxy)
(cd "$PESTO_SOURCE" && rustup run "$PESTO_RUST_TOOLCHAIN" cargo build --release --locked -p pesto-poster --bin pesto)
cp "$PESTO_SOURCE/target/release/pesto" "$STATE/bin/pesto"

"$STATE/bin/postingnntpfixture" \
  -listen 127.0.0.1:11120 \
  -capture "$STATE/articles.jsonl" \
  -ready-file "$STATE/postingnntp.ready" \
  >"$STATE/postingnntp.log" 2>&1 &
NNTP_PID=$!
wait_file "$STATE/postingnntp.ready"

"$STATE/bin/hookproxy" \
  -listen 127.0.0.1:18091 \
  -target http://127.0.0.1:18081 \
  -failures 2 \
  -ready-file "$STATE/hookproxy.ready" \
  >"$STATE/hookproxy.log" 2>&1 &
PROXY_PID=$!
wait_file "$STATE/hookproxy.ready"

cat >"$STATE/bin/submit-hook" <<EOF
#!/bin/sh
set -eu
: "\${PESTO_NZB:?pesto did not provide the generated NZB path}"
exec "$ROOT/scripts/gonzb-submit-nzb.sh" "\$PESTO_NZB"
EOF
chmod 700 "$STATE/bin/submit-hook"

# Prevent pesto's courtesy version check from making an external request. The
# pinned version is deliberately recorded as current inside this disposable
# config root; the upload itself can then reach loopback only.
cat >"$STATE/pesto-config/pesto/update_check.json" <<'EOF'
{"checked_at":4102444800,"latest_version":"0.8.6"}
EOF

cat >"$STATE/input/Synthetic.Pesto.Conformance.CC0.txt" <<'EOF'
GoNZB and pesto local conformance payload.

This synthetic text was created solely for protocol integration testing.
To the extent possible under law, its author dedicates it to the public domain
under CC0 1.0. It contains no third-party media or copyrighted sample data.

Segment one: pesto posts this content only to a loopback NNTP fixture.
Segment two: the post-upload hook sends only the completed NZB to GoNZB.
Segment three: no source path or NNTP credential crosses the uploader boundary.
EOF

source_sha_before=$(sha256sum "$STATE/input/Synthetic.Pesto.Conformance.CC0.txt" | awk '{print $1}')
source_size=$(wc -c <"$STATE/input/Synthetic.Pesto.Conformance.CC0.txt" | tr -d ' ')
nzb_path="$STATE/output/Synthetic.Pesto.Conformance.CC0.nzb"

echo "Running pinned pesto through two injected HTTP 503 responses"
env XDG_CONFIG_HOME="$STATE/pesto-config" \
  GONZB_URL=http://127.0.0.1:18091 GONZB_TOKEN="$PESTO_TOKEN" \
  "$STATE/bin/pesto" \
  --host 127.0.0.1 --port 11120 --no-ssl --connections 2 \
  --groups alt.binaries.gonzb.synthetic \
  --from synthetic-pesto@example.invalid \
  --article-size 256 --par2 0 --pipeline-depth 1 --retries 1 \
  --check --check-delay 0 --check-retries 1 --check-connections 1 --check-post-retries 0 \
  --message-id-domain example.invalid --date now \
  --out "$nzb_path" --nzb-title Synthetic.Pesto.Conformance.CC0 \
  --no-history --no-notify --no-hooks \
  --post-hook "$STATE/bin/submit-hook" \
  --output-format json \
  "$STATE/input/Synthetic.Pesto.Conformance.CC0.txt" \
  >"$STATE/pesto.log" 2>&1

test -s "$nzb_path"
source_sha_after=$(sha256sum "$STATE/input/Synthetic.Pesto.Conformance.CC0.txt" | awk '{print $1}')
[ "$source_sha_after" = "$source_sha_before" ] || {
  echo "pesto modified its source input" >&2
  exit 1
}
if grep -Fq "$PESTO_TOKEN" "$STATE/pesto.log" "$STATE/hookproxy.log" "$STATE/postingnntp.log"; then
  echo "pesto uploader token leaked into fixture logs" >&2
  exit 1
fi

submission_json=$(admin_get node-a 18081 '/api/v1/uploader/submissions?state=pending_review&limit=10')
submission_id=$(printf '%s' "$submission_json" | jq -r '.items[0].id // empty')
[ -n "$submission_id" ] || {
  echo "pesto post-hook did not create a pending submission" >&2
  exit 1
}
submission_json=$(admin_get node-a 18081 "/api/v1/uploader/submissions/$submission_id")
segment_count=$(printf '%s' "$submission_json" | jq -r '.submission.segment_count')
printf '%s' "$submission_json" | jq -e \
  '.submission.state == "pending_review"
   and .submission.intake_kind == "http"
   and .submission.submitted_by == "pesto-conformance"
   and .submission.title == "Synthetic.Pesto.Conformance.CC0"
   and .submission.file_count == 1
   and (.submission.groups | index("alt.binaries.gonzb.synthetic") != null)' >/dev/null

captured_count=$(jq -s 'length' "$STATE/articles.jsonl")
[ "$captured_count" = "$segment_count" ] || {
  echo "captured article count $captured_count differs from NZB segment count $segment_count" >&2
  exit 1
}
captured_article_bytes=$(jq -s 'map(.article_bytes) | add' "$STATE/articles.jsonl")
submission_size=$(printf '%s' "$submission_json" | jq -r '.submission.size_bytes')
[ "$captured_article_bytes" = "$submission_size" ] || {
  echo "captured wire article bytes $captured_article_bytes differ from NZB-derived size $submission_size" >&2
  exit 1
}
jq -e -s \
  'length > 0
   and all(.[]; .article_bytes > .body_bytes
     and .body_bytes > 0
     and .yenc_name == "Synthetic.Pesto.Conformance.CC0.txt"
     and (.newsgroups | index("alt.binaries.gonzb.synthetic") != null)
     and (.article_sha256 | length) == 64
     and (.body_sha256 | length) == 64)' \
  "$STATE/articles.jsonl" >/dev/null
while IFS= read -r message_id; do
  plain_id=${message_id#<}
  plain_id=${plain_id%>}
  grep -Fq "$plain_id" "$nzb_path" || {
    echo "pesto NZB is missing captured message ID $message_id" >&2
    exit 1
  }
done <<EOF
$(jq -r '.message_id' "$STATE/articles.jsonl")
EOF

proxy_stats=$(curl -fsS http://127.0.0.1:18091/stats)
printf '%s' "$proxy_stats" | jq -e \
  '.requests >= 3 and .injected_failures == 2 and .forwarded == 1' >/dev/null

echo "Verifying exact-content callback deduplication"
dedupe_json=$(GONZB_URL=http://127.0.0.1:18081 GONZB_TOKEN="$PESTO_TOKEN" \
  "$ROOT/scripts/gonzb-submit-nzb.sh" "$nzb_path")
printf '%s' "$dedupe_json" | jq -e \
  --arg id "$submission_id" '.created == false and .submission.id == $id' >/dev/null

unauthorized_code=$(curl --silent --show-error -o "$STATE/unauthorized-response.json" -w '%{http_code}' \
  -F "nzb=@$nzb_path;type=application/x-nzb" \
  http://127.0.0.1:18081/api/v1/uploader/submissions)
[ "$unauthorized_code" = "401" ] || {
  echo "unauthenticated pesto submission returned HTTP $unauthorized_code, expected 401" >&2
  exit 1
}

echo "Approving the pesto submission and checking local Newznab search/get"
approved_json=$(admin_request node-a 18081 "/api/v1/uploader/submissions/$submission_id/actions/approve" '{}')
printf '%s' "$approved_json" | jq -e '.submission.state == "approved"' >/dev/null
search_token=$(admin_request node-a 18081 /api/v1/auth/tokens '{"name":"pesto-conformance-search"}' | jq -r '.secret')
test -n "$search_token" && test "$search_token" != "null"

newznab_search "$search_token" "Synthetic.Pesto.Conformance.CC0" "$STATE/search.xml"
grep -Fq "Synthetic.Pesto.Conformance.CC0" "$STATE/search.xml"
release_id=$(extract_guid "$STATE/search.xml")
test -n "$release_id"
newznab_get "$search_token" "$release_id" "$STATE/grab.nzb"
expected_sha=$(printf '%s' "$submission_json" | jq -r '.submission.nzb_sha256')
grab_sha=$(sha256sum "$STATE/grab.nzb" | awk '{print $1}')
[ "$grab_sha" = "$expected_sha" ] || {
  echo "Newznab returned NZB hash $grab_sha; expected $expected_sha" >&2
  exit 1
}

echo "Returning the pesto submission to pending and checking search withdrawal"
pending_json=$(admin_request node-a 18081 "/api/v1/uploader/submissions/$submission_id/actions/return-to-pending" '{}')
printf '%s' "$pending_json" | jq -e '.submission.state == "pending_review"' >/dev/null
newznab_search "$search_token" "Synthetic.Pesto.Conformance.CC0" "$STATE/search-after-withdrawal.xml"
if grep -Fq "Synthetic.Pesto.Conformance.CC0" "$STATE/search-after-withdrawal.xml"; then
  echo "returned-to-pending pesto submission remained searchable" >&2
  exit 1
fi

echo "pesto conformance passed"
echo "  pinned commit: $PESTO_COMMIT"
echo "  synthetic source bytes: $source_size"
echo "  synthetic articles: $captured_count"
echo "  uploader submission: $submission_id"
echo "  verified: real POST/STAT, post-hook 503 retry, least privilege, metadata, dedupe, approval, Newznab search/get, withdrawal"
