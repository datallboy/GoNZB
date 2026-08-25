#!/bin/sh
# Full gonzb-worker conformance test. Every payload and credential is synthetic
# and disposable. Pesto posts only to a loopback NNTP fixture; no torrent or
# external Usenet endpoint is contacted.
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
STATE="$ROOT/.e2e/uploader-worker"
GONZBNET_STATE="$ROOT/.e2e/gonzbnet"
GONZBNET_SCRIPT="$ROOT/scripts/gonzbnet_e2e.sh"
PESTO_COMMIT="b9e2d8a41ddfddb2dd0d0954a5984114b3553636"
PESTO_SOURCE=${PESTO_SOURCE:-}
PESTO_RUST_TOOLCHAIN=${PESTO_RUST_TOOLCHAIN:-1.96.0}
KEEP_STATE=${UPLOADER_WORKER_KEEP_STATE:-0}
TORRENT_HASH="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
RELEASE_NAME="Synthetic.Worker.Conformance.CC0"
QBIT_USERNAME="worker-conformance"
QBIT_PASSWORD="worker-conformance-local"

usage() {
  cat >&2 <<EOF
usage: PESTO_SOURCE=/path/to/pesto $0 {test|start|worker|approve|federate|aggregator|stop|reset|status}

  test        run every stage and clean disposable state
  start       start four GoNZB nodes and loopback qBittorrent/NNTP fixtures
  worker      run gonzb-worker once and validate uploader metadata
  approve     approve the submission and validate Node A aggregator output
  federate    publish to pool.e2e and synchronize Node D
  aggregator  validate Node D aggregator search, grab, and manifest cache
  stop        stop processes and containers, retaining state
  reset       stop and remove all disposable harness state
  status      show fixture and GoNZB process status
EOF
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "$1 is required" >&2
    exit 69
  }
}

require_file() {
  test -s "$1" || {
    echo "required harness state is missing: $1" >&2
    exit 66
  }
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

stop_pidfile() {
  pidfile=$1
  if [ -f "$pidfile" ]; then
    pid=$(cat "$pidfile")
    kill "$pid" 2>/dev/null || true
    rm -f "$pidfile"
  fi
}

stop_local_fixtures() {
  stop_pidfile "$STATE/qbittorrent.pid"
  stop_pidfile "$STATE/postingnntp.pid"
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
  docker compose -p gonzbnet-e2e -f "$ROOT/docker-compose.gonzbnet-e2e.yml" exec -T postgres \
    psql -U gonzb -d "$database" -Atc "$query"
}

newznab_search() {
  port=$1
  token=$2
  query=$3
  destination=$4
  curl --fail-with-body --silent --show-error --get \
    --data-urlencode 't=search' --data-urlencode "q=$query" --data-urlencode "apikey=$token" \
    "http://127.0.0.1:$port/api" >"$destination"
}

newznab_get() {
  port=$1
  token=$2
  requested_guid=$3
  destination=$4
  curl --fail-with-body --silent --show-error --get \
    --data-urlencode 't=get' --data-urlencode "id=$requested_guid" --data-urlencode "apikey=$token" \
    "http://127.0.0.1:$port/api" >"$destination"
}

extract_guid() {
  sed -n 's:.*<guid isPermaLink="false">\([^<]*\)</guid>.*:\1:p' "$1" | head -n 1
}

validate_pesto_source() {
  if [ -z "$PESTO_SOURCE" ]; then
    echo "PESTO_SOURCE is required for start/test" >&2
    usage
    exit 64
  fi
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
    echo "Rust toolchain $PESTO_RUST_TOOLCHAIN is required" >&2
    exit 69
  fi
}

build_fixtures() {
  echo "Building gonzb-worker, pinned pesto, and loopback fixtures"
  (cd "$ROOT" && GOCACHE="${GOCACHE:-/tmp/gocache}" go build -o "$STATE/bin/gonzb-worker" ./cmd/gonzb-worker)
  (cd "$ROOT" && GOCACHE="${GOCACHE:-/tmp/gocache}" go build -o "$STATE/bin/qbittorrentfixture" ./test/e2e/worker/qbittorrentfixture)
  (cd "$ROOT" && GOCACHE="${GOCACHE:-/tmp/gocache}" go build -o "$STATE/bin/rsyncfixture" ./test/e2e/worker/rsyncfixture)
  (cd "$ROOT" && GOCACHE="${GOCACHE:-/tmp/gocache}" go build -o "$STATE/bin/postingnntpfixture" ./test/e2e/uploader/postienntpfixture)
  (cd "$PESTO_SOURCE" && rustup run "$PESTO_RUST_TOOLCHAIN" cargo build --release --locked -p pesto-poster --bin pesto)
  cp "$PESTO_SOURCE/target/release/pesto" "$STATE/bin/pesto"
}

create_source() {
  mkdir -p "$STATE/source/$RELEASE_NAME"
  cat >"$STATE/source/$RELEASE_NAME/Worker.Conformance.CC0.txt" <<'EOF'
GoNZB worker and pesto local conformance payload.

This synthetic text was created solely for protocol integration testing.
To the extent possible under law, its author dedicates it to the public domain
under CC0 1.0. It contains no third-party media or copyrighted sample data.

The worker discovers this completed fixture through a loopback qBittorrent API,
copies it through the confined rsync fixture, and asks pesto to encrypt,
obfuscate, generate PAR2 recovery data, and post to loopback NNTP only.
EOF
  sha256sum "$STATE/source/$RELEASE_NAME/Worker.Conformance.CC0.txt" | awk '{print $1}' >"$STATE/source.sha256"
  wc -c <"$STATE/source/$RELEASE_NAME/Worker.Conformance.CC0.txt" | tr -d ' ' >"$STATE/source.size"
}

write_configs() {
  cat >"$STATE/pesto.toml" <<'EOF'
[server]
host = "127.0.0.1"
port = 11121
ssl = false
connections = 2
timeout = 30

[posting]
groups = ["alt.binaries.gonzb.synthetic"]
retries = 1
check = true
check_delay = 0
check_retries = 1
check_connections = 1
check_post_retries = 0
EOF

  mkdir -p "$STATE/pesto-config/pesto"
  cat >"$STATE/pesto-config/pesto/update_check.json" <<'EOF'
{"checked_at":4102444800,"latest_version":"0.8.6"}
EOF

  cat >"$STATE/gonzb-worker.yaml" <<EOF
worker:
  data_dir: $STATE/worker-data
  node_id: worker-conformance-vps
  poll_interval_seconds: 1
  min_free_space_gb: 0
  workspace_multiplier: 1
  cleanup_on_success: false

qbittorrent:
  url: http://127.0.0.1:18092
  username: ""
  password: ""
  candidate_tag: gonzb-candidate
  timeout_seconds: 5

transfer:
  type: rsync
  rsync_binary: $STATE/bin/rsyncfixture
  ssh_host: fixture.invalid
  ssh_user: fixture
  ssh_port: 22
  ssh_key: ""
  source_root: $STATE/source
  extra_args: []

pesto:
  binary: $STATE/bin/pesto
  config_path: $STATE/pesto.toml
  compression: 7z
  encryption: true
  obfuscation: full
  par2_percent: 10
  extra_args:
    - --from=synthetic-worker@example.invalid
    - --article-size=256
    - --pipeline-depth=1
    - --message-id-domain=example.invalid
    - --date=now

gonzb:
  url: http://127.0.0.1:18081
  api_token: ""
  timeout_seconds: 10
EOF
  chmod 600 "$STATE/pesto.toml" "$STATE/gonzb-worker.yaml"
}

start_fixtures() {
  "$STATE/bin/postingnntpfixture" \
    -listen 127.0.0.1:11121 -capture "$STATE/articles.jsonl" -ready-file "$STATE/postingnntp.ready" \
    >"$STATE/postingnntp.log" 2>&1 &
  echo "$!" >"$STATE/postingnntp.pid"
  wait_file "$STATE/postingnntp.ready"

  "$STATE/bin/qbittorrentfixture" \
    -listen 127.0.0.1:18092 -ready-file "$STATE/qbittorrent.ready" \
    -username "$QBIT_USERNAME" -password "$QBIT_PASSWORD" -tag gonzb-candidate \
    -hash "$TORRENT_HASH" -name "$RELEASE_NAME" \
    -content-path "$STATE/source/$RELEASE_NAME" -size "$(cat "$STATE/source.size")" \
    -tracker 'https://fixture-user:fixture-pass@tracker.example.invalid/announce/private?token=fixture-secret' \
    >"$STATE/qbittorrent.log" 2>&1 &
  echo "$!" >"$STATE/qbittorrent.pid"
  wait_file "$STATE/qbittorrent.ready"
}

create_worker_identity() {
  echo "Creating a least-privilege worker uploader identity"
  admin_request node-a 18081 /api/v1/admin/auth/roles \
    '{"id":"worker-submit","name":"worker submit only","permissions":["uploader.submissions.create"]}' >/dev/null
  admin_request node-a 18081 /api/v1/admin/auth/users \
    '{"id":"worker-conformance","username":"worker-conformance","password":"worker-conformance-local-2026","enabled":true,"role_ids":["worker-submit"]}' >/dev/null
  admin_request node-a 18081 /api/v1/admin/auth/tokens \
    '{"user_id":"worker-conformance","name":"worker-conformance"}' | jq -r '.secret' >"$STATE/worker-token"
  chmod 600 "$STATE/worker-token"
  token=$(cat "$STATE/worker-token")
  test -n "$token" && test "$token" != "null"
  forbidden_code=$(curl --silent --show-error -o "$STATE/least-privilege-response.json" -w '%{http_code}' \
    -H "Authorization: Bearer $token" http://127.0.0.1:18081/api/v1/uploader/submissions)
  [ "$forbidden_code" = "403" ] || {
    echo "worker token unexpectedly gained uploader read access (HTTP $forbidden_code)" >&2
    exit 1
  }
}

start_stage() {
  validate_pesto_source
  for command in curl docker git go jq rustup sed sha256sum 7z; do
    require_command "$command"
  done
  "$0" reset >/dev/null 2>&1 || true
  umask 077
  mkdir -p "$STATE/bin" "$STATE/worker-data" "$STATE/pesto-config/pesto"
  "$GONZBNET_SCRIPT" start
  "$GONZBNET_SCRIPT" bootstrap
  "$GONZBNET_SCRIPT" configure-pool
  curl -fsS http://127.0.0.1:18081/readyz | jq -e \
    '.modules.uploader.enabled == true and .modules.uploader.ready == true' >/dev/null
  build_fixtures
  create_source
  write_configs
  start_fixtures
  create_worker_identity
  echo "Worker conformance fixtures are ready"
}

worker_stage() {
  for required in "$STATE/bin/gonzb-worker" "$STATE/gonzb-worker.yaml" "$STATE/worker-token" "$STATE/source.sha256"; do
    require_file "$required"
  done
  echo "Running gonzb-worker through qBittorrent, rsync, pesto, and uploader intake"
  env XDG_CONFIG_HOME="$STATE/pesto-config" \
    GONZB_WORKER_QBITTORRENT_USERNAME="$QBIT_USERNAME" \
    GONZB_WORKER_QBITTORRENT_PASSWORD="$QBIT_PASSWORD" \
    GONZB_WORKER_GONZB_API_TOKEN="$(cat "$STATE/worker-token")" \
    GONZB_RSYNC_FIXTURE_SOURCE_ROOT="$STATE/source" \
    GONZB_RSYNC_FIXTURE_DEST_ROOT="$STATE/worker-data" \
    "$STATE/bin/gonzb-worker" -config "$STATE/gonzb-worker.yaml" -once -torrent-hash "$TORRENT_HASH" \
    >"$STATE/worker.log" 2>&1

  current_source_sha=$(sha256sum "$STATE/source/$RELEASE_NAME/Worker.Conformance.CC0.txt" | awk '{print $1}')
  [ "$current_source_sha" = "$(cat "$STATE/source.sha256")" ] || {
    echo "gonzb-worker modified the fixture source" >&2
    exit 1
  }
  nzb_path=$(find "$STATE/worker-data/jobs" -type f -path '*/output/release.nzb' -print | head -n 1)
  test -n "$nzb_path" && test -s "$nzb_path" || {
    echo "worker did not retain a generated NZB" >&2
    exit 1
  }
  printf '%s\n' "$nzb_path" >"$STATE/nzb-path"
  if grep -Fq "$RELEASE_NAME" "$nzb_path" || grep -Fq 'obfuscated:' "$nzb_path"; then
    echo "worker NZB retained title or obfuscation metadata" >&2
    exit 1
  fi
  meta_types=$(sed -n 's/.*<meta type="\([^"]*\)">.*/\1/p' "$nzb_path" | sort -u)
  [ "$meta_types" = "password" ] || {
    echo "worker NZB contains unexpected metadata types: $meta_types" >&2
    exit 1
  }

  submission_list=$(admin_get node-a 18081 '/api/v1/uploader/submissions?state=pending_review&limit=20')
  submission_id=$(printf '%s' "$submission_list" | jq -r --arg hash "$TORRENT_HASH" \
    '.items[] | select(.provenance_external_id == $hash) | .id' | head -n 1)
  test -n "$submission_id" || {
    echo "worker submission did not reach Node A uploader" >&2
    exit 1
  }
  printf '%s\n' "$submission_id" >"$STATE/submission-id"
  submission_json=$(admin_get node-a 18081 "/api/v1/uploader/submissions/$submission_id")
  printf '%s' "$submission_json" >"$STATE/submission.json"
  printf '%s' "$submission_json" | jq -e \
    --arg title "$RELEASE_NAME" --arg hash "$TORRENT_HASH" \
    '.submission.state == "pending_review"
     and .submission.title == $title
     and .submission.intake_kind == "http"
     and .submission.submitted_by == "worker-conformance"
     and .submission.provenance_tool == "gonzb-worker/pesto"
     and .submission.provenance_version == "pesto 0.8.6"
     and .submission.provenance_external_id == $hash
     and .submission.has_password == true
     and .submission.has_par2 == true
     and .submission.obfuscated_subjects == true
     and .submission.encrypted_names == true
     and .submission.file_count > 0
     and .submission.segment_count > 0
     and (.submission.groups | index("alt.binaries.gonzb.synthetic") != null)
     and (.submission.artifacts | length) == 1
     and .submission.artifacts[0].kind == "metadata"
     and .submission.artifacts[0].original_filename == "gonzb-worker.json"' >/dev/null

  release_id=$(printf '%s' "$submission_json" | jq -r '.submission.release_id')
  printf '%s\n' "$release_id" >"$STATE/local-release-id"
  artifact_id=$(printf '%s' "$submission_json" | jq -r '.submission.artifacts[0].id')
  admin_get node-a 18081 "/api/v1/uploader/submissions/$submission_id/artifacts/$artifact_id" >"$STATE/gonzb-worker.json"
  jq -e --arg hash "$TORRENT_HASH" --argjson size "$(cat "$STATE/source.size")" \
    '.torrent_hash == $hash
     and .source_tracker == "tracker.example.invalid"
     and .original_size_bytes == $size
     and .worker_node_id == "worker-conformance-vps"
     and (.job_id | length) > 0' "$STATE/gonzb-worker.json" >/dev/null

  captured_count=$(jq -s 'length' "$STATE/articles.jsonl")
  segment_count=$(printf '%s' "$submission_json" | jq -r '.submission.segment_count')
  [ "$captured_count" = "$segment_count" ] || {
    echo "captured article count $captured_count differs from uploader segment count $segment_count" >&2
    exit 1
  }
  while IFS= read -r message_id; do
    plain_id=${message_id#<}
    plain_id=${plain_id%>}
    grep -Fq "$plain_id" "$nzb_path" || {
      echo "worker NZB is missing captured message ID $message_id" >&2
      exit 1
    }
  done <<EOF
$(jq -r '.message_id' "$STATE/articles.jsonl")
EOF
  if grep -Fq "$(cat "$STATE/worker-token")" "$STATE/worker.log" "$STATE/postingnntp.log" "$STATE/qbittorrent.log"; then
    echo "worker uploader token leaked into fixture logs" >&2
    exit 1
  fi
  if grep -Fq 'fixture-secret' "$STATE/worker.log" "$STATE/gonzb-worker.json"; then
    echo "private tracker material leaked into worker output" >&2
    exit 1
  fi
  echo "Worker intake passed: submission=$submission_id release=$release_id articles=$captured_count"
}

approve_stage() {
  require_file "$STATE/submission-id"
  require_file "$STATE/nzb-path"
  submission_id=$(cat "$STATE/submission-id")
  echo "Approving worker submission and checking Node A aggregator"
  approved=$(admin_request node-a 18081 "/api/v1/uploader/submissions/$submission_id/actions/approve" '{}')
  printf '%s' "$approved" | jq -e '.submission.state == "approved"' >/dev/null
  source_token=$(admin_request node-a 18081 /api/v1/auth/tokens '{"name":"worker-conformance-search-a"}' | jq -r '.secret')
  printf '%s\n' "$source_token" >"$STATE/source-search-token"
  chmod 600 "$STATE/source-search-token"
  newznab_search 18081 "$source_token" "$RELEASE_NAME" "$STATE/node-a-search.xml"
  grep -Fq "$RELEASE_NAME" "$STATE/node-a-search.xml"
  source_guid=$(extract_guid "$STATE/node-a-search.xml")
  test -n "$source_guid"
  newznab_get 18081 "$source_token" "$source_guid" "$STATE/node-a-grab.nzb"
  cmp "$(cat "$STATE/nzb-path")" "$STATE/node-a-grab.nzb"
  echo "Node A uploader release is available through its aggregator"
}

federate_stage() {
  require_file "$STATE/submission-id"
  submission_id=$(cat "$STATE/submission-id")
  echo "Publishing worker release to pool.e2e"
  admin_get node-a 18081 /api/v1/uploader/federation-pools | jq -e '.items | index("pool.e2e") != null' >/dev/null
  publication=$(admin_request node-a 18081 "/api/v1/uploader/submissions/$submission_id/federation-publications" \
    '{"pool_ids":["pool.e2e"]}')
  printf '%s' "$publication" | jq -e '.items[0].state == "published"' >/dev/null
  release_id=$(printf '%s' "$publication" | jq -r '.items[0].release_id')
  manifest_id=$(printf '%s' "$publication" | jq -r '.items[0].manifest_id')
  printf '%s\n' "$release_id" >"$STATE/federated-release-id"
  printf '%s\n' "$manifest_id" >"$STATE/manifest-id"
  event_count=$(db_scalar gonzbnet_a "
    SELECT count(*) FROM federation_events
    WHERE pool_ids @> '[\"pool.e2e\"]'::jsonb
      AND event_type = 'ReleaseCard'
      AND body_json->>'release_id' = '$release_id'
      AND body_json->'source'->>'kind' = 'local_uploader'
      AND validation_status = 'accepted'")
  [ "$event_count" = "1" ] || {
    echo "expected one accepted local_uploader ReleaseCard on Node A, got $event_count" >&2
    exit 1
  }

  attempts=0
  projected=0
  while [ "$attempts" -lt 60 ]; do
    admin_request node-a 18081 /api/v1/admin/gonzbnet/sync/push '{}' >/dev/null
    admin_request node-d 18084 /api/v1/admin/gonzbnet/sync/pull '{}' >/dev/null
    projected=$(db_scalar gonzbnet_d "
      SELECT count(*) FROM federated_release_sources
      WHERE release_id = '$release_id' AND pool_id = 'pool.e2e' AND resolvable")
    [ "$projected" = "1" ] && break
    attempts=$((attempts + 1))
    sleep 1
  done
  [ "$projected" = "1" ] || {
    echo "Node D did not project the worker release" >&2
    exit 1
  }
  echo "Federation passed: release=$release_id manifest=$manifest_id"
}

aggregator_stage() {
  require_file "$STATE/federated-release-id"
  require_file "$STATE/manifest-id"
  require_file "$STATE/nzb-path"
  release_id=$(cat "$STATE/federated-release-id")
  manifest_id=$(cat "$STATE/manifest-id")
  echo "Checking the GoNZBNet-backed aggregator on Node D"
  remote_token=$(admin_request node-d 18084 /api/v1/auth/tokens '{"name":"worker-conformance-search-d"}' | jq -r '.secret')
  printf '%s\n' "$remote_token" >"$STATE/remote-search-token"
  chmod 600 "$STATE/remote-search-token"

  attempts=0
  remote_guid=""
  while [ "$attempts" -lt 60 ]; do
    newznab_search 18084 "$remote_token" "$RELEASE_NAME" "$STATE/node-d-search.xml"
    if grep -Fq "$RELEASE_NAME" "$STATE/node-d-search.xml"; then
      remote_guid=$(extract_guid "$STATE/node-d-search.xml")
      test -n "$remote_guid" && break
    fi
    admin_request node-a 18081 /api/v1/admin/gonzbnet/sync/push '{}' >/dev/null
    admin_request node-d 18084 /api/v1/admin/gonzbnet/sync/pull '{}' >/dev/null
    attempts=$((attempts + 1))
    sleep 1
  done
  [ -n "$remote_guid" ] || {
    echo "Node D aggregator did not return the worker release" >&2
    exit 1
  }
  newznab_get 18084 "$remote_token" "$remote_guid" "$STATE/node-d-first-grab.nzb"
  cmp "$(cat "$STATE/nzb-path")" "$STATE/node-d-first-grab.nzb"
  newznab_get 18084 "$remote_token" "$remote_guid" "$STATE/node-d-second-grab.nzb"
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
    echo "Node D did not cache the signed worker manifest" >&2
    exit 1
  }
  projected=$(db_scalar gonzbnet_d "
    SELECT count(*) FROM federated_release_sources
    WHERE release_id = '$release_id' AND pool_id = 'pool.e2e' AND resolvable")
  [ "$projected" = "1" ] || {
    echo "Node D aggregator result is not backed by the expected federated projection" >&2
    exit 1
  }
  echo "Remote aggregator passed: Node D search/grab/cache resolved Node A worker release"
}

reset_stage() {
  stop_local_fixtures
  "$GONZBNET_SCRIPT" reset >/dev/null 2>&1 || true
  rm -rf "$STATE"
}

cleanup_test() {
  result=$?
  trap - 0 1 2 15
  if [ "$result" -ne 0 ]; then
    echo "worker conformance failed; recent fixture logs follow" >&2
    for log in "$STATE/worker.log" "$STATE/postingnntp.log" "$STATE/qbittorrent.log" "$GONZBNET_STATE/node-a/stdout.log" "$GONZBNET_STATE/node-d/stdout.log"; do
      if [ -f "$log" ]; then
        echo "==> $log <==" >&2
        tail -n 50 "$log" >&2 || true
      fi
    done
  fi
  if [ "$KEEP_STATE" = "1" ]; then
    stop_local_fixtures
    "$GONZBNET_SCRIPT" stop >/dev/null 2>&1 || true
    echo "state retained under $STATE and $GONZBNET_STATE" >&2
  else
    reset_stage
  fi
  exit "$result"
}

case "${1:-}" in
  test)
    trap cleanup_test 0 1 2 15
    start_stage
    worker_stage
    approve_stage
    federate_stage
    aggregator_stage
    trap - 0 1 2 15
    if [ "$KEEP_STATE" = "1" ]; then
      stop_local_fixtures
      "$GONZBNET_SCRIPT" stop >/dev/null 2>&1 || true
      echo "state retained under $STATE and $GONZBNET_STATE"
    else
      reset_stage
    fi
    echo "gonzb-worker full conformance passed"
    ;;
  start) start_stage ;;
  worker) worker_stage ;;
  approve) approve_stage ;;
  federate) federate_stage ;;
  aggregator) aggregator_stage ;;
  stop)
    stop_local_fixtures
    "$GONZBNET_SCRIPT" stop
    ;;
  reset) reset_stage ;;
  status)
    for fixture in qbittorrent postingnntp; do
      if [ -s "$STATE/$fixture.pid" ] && kill -0 "$(cat "$STATE/$fixture.pid")" 2>/dev/null; then
        echo "$fixture: running"
      else
        echo "$fixture: stopped"
      fi
    done
    "$GONZBNET_SCRIPT" status
    ;;
  *) usage; exit 2 ;;
esac
