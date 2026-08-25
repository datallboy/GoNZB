#!/bin/sh
# Optional live Newznab consumer conformance. It publishes two synthetic NZBs
# through a disposable GoNZBNet pool, then exercises pinned Prowlarr, Radarr,
# and Sonarr containers against the remote aggregator node.
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
STATE="$ROOT/.e2e/arr-consumers"
GONZBNET_STATE="$ROOT/.e2e/gonzbnet"
GONZBNET_SCRIPT="$ROOT/scripts/gonzbnet_e2e.sh"
COMPOSE="$ROOT/docker-compose.gonzbnet-e2e.yml"
COMPOSE_PROJECT="gonzbnet-e2e"
KEEP_STATE=${NEWZNAB_ARR_KEEP_STATE:-0}

PROWLARR_IMAGE=${PROWLARR_IMAGE:-lscr.io/linuxserver/prowlarr@sha256:2489c6dbaf11e3a6d71aeb2e6980d04193d4af611aa7064a974851222fd41722}
RADARR_IMAGE=${RADARR_IMAGE:-lscr.io/linuxserver/radarr@sha256:a45b5ab0f850f39edb4cc9c95bbd967b52ddc3d4574a4dfb45561177db6c88f4}
SONARR_IMAGE=${SONARR_IMAGE:-lscr.io/linuxserver/sonarr@sha256:24acea2956a0ccb11f103877d9f4f8576600fb34bff34820ed749c2256dab89f}

PROWLARR_CONTAINER="gonzb-prowlarr"
RADARR_CONTAINER="gonzb-radarr"
SONARR_CONTAINER="gonzb-sonarr"

PROWLARR_VERSION="2.3.5.5327"
RADARR_VERSION="6.3.0.10514"
SONARR_VERSION="4.0.19.2979"

require_command() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "$1 is required" >&2
    exit 69
  }
}

remove_containers() {
  docker rm -f "$PROWLARR_CONTAINER" "$RADARR_CONTAINER" "$SONARR_CONTAINER" >/dev/null 2>&1 || true
}

cleanup() {
  result=$?
  trap - 0 1 2 15
  remove_containers
  if [ "$result" -ne 0 ]; then
    echo "ARR conformance failed; recent logs follow" >&2
    for log in \
      "$STATE/prowlarr/logs/prowlarr.txt" \
      "$STATE/radarr/logs/radarr.txt" \
      "$STATE/sonarr/logs/sonarr.txt" \
      "$GONZBNET_STATE/node-d/stdout.log"; do
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

ensure_image() {
  image_ref=$1
  if ! docker image inspect "$image_ref" >/dev/null 2>&1; then
    docker pull "$image_ref"
  fi
}

wait_http() {
  port_number=$1
  attempts=0
  until curl -fsS "http://127.0.0.1:$port_number/ping" >/dev/null 2>&1; do
    attempts=$((attempts + 1))
    if [ "$attempts" -ge 120 ]; then
      echo "timed out waiting for consumer on port $port_number" >&2
      return 1
    fi
    sleep 1
  done
}

assert_port_free() {
  port_number=$1
  if curl -fsS --connect-timeout 1 "http://127.0.0.1:$port_number/ping" >/dev/null 2>&1; then
    echo "port $port_number already serves an ARR application; refusing to reuse it" >&2
    exit 1
  fi
}

api_key_from_config() {
  config_file=$1
  sed -n 's:.*<ApiKey>\([^<]*\)</ApiKey>.*:\1:p' "$config_file"
}

admin_request() {
  node=$1
  port_number=$2
  endpoint=$3
  payload=$4
  csrf=$(cat "$GONZBNET_STATE/$node/csrf-token")
  curl --fail-with-body --silent --show-error \
    -b "$GONZBNET_STATE/$node/cookies.txt" \
    -H "X-CSRF-Token: $csrf" -H 'Content-Type: application/json' \
    -d "$payload" "http://127.0.0.1:$port_number$endpoint"
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

newznab_get() {
  port_number=$1
  token_value=$2
  release_guid=$3
  destination=$4
  curl --fail-with-body --silent --show-error --get \
    --data-urlencode 't=get' \
    --data-urlencode "id=$release_guid" \
    --data-urlencode "apikey=$token_value" \
    "http://127.0.0.1:$port_number/api" >"$destination"
}

extract_guid() {
  sed -n 's:.*<guid isPermaLink="false">\([^<]*\)</guid>.*:\1:p' "$1" | head -n 1
}

db_scalar() {
  database_name=$1
  sql_query=$2
  docker compose -p "$COMPOSE_PROJECT" -f "$COMPOSE" exec -T postgres \
    psql -U gonzb -d "$database_name" -Atc "$sql_query"
}

start_arr_container() {
  container_name=$1
  image_ref=$2
  config_dir=$3
  shift 3
  docker run -d --name "$container_name" --network host \
    -e PUID="$(id -u)" -e PGID="$(id -g)" -e TZ=Etc/UTC \
    -e HTTP_PROXY=http://127.0.0.1:9 \
    -e HTTPS_PROXY=http://127.0.0.1:9 \
    -e NO_PROXY=127.0.0.1,localhost \
    -v "$config_dir:/config" "$@" "$image_ref" >/dev/null
}

run_arr_command() {
  port_number=$1
  api_key=$2
  command_name=$3
  command_id=$(curl -fsS \
    -H "X-Api-Key: $api_key" -H 'Content-Type: application/json' \
    -d "{\"name\":\"$command_name\"}" \
    "http://127.0.0.1:$port_number/api/v3/command" | jq -r '.id')
  test -n "$command_id" && test "$command_id" != "null"
  attempts=0
  command_status=""
  while [ "$attempts" -lt 90 ]; do
    command_status=$(curl -fsS -H "X-Api-Key: $api_key" \
      "http://127.0.0.1:$port_number/api/v3/command/$command_id" | jq -r '.status')
    case "$command_status" in
      completed) return 0 ;;
      failed)
        echo "$command_name failed on port $port_number" >&2
        return 1
        ;;
    esac
    attempts=$((attempts + 1))
    sleep 1
  done
  echo "timed out waiting for $command_name on port $port_number" >&2
  return 1
}

for command in curl docker git go jq sed sha256sum; do
  require_command "$command"
done

trap cleanup 0 1 2 15
remove_containers
"$GONZBNET_SCRIPT" reset >/dev/null 2>&1 || true
rm -rf "$STATE"
mkdir -p "$STATE/prowlarr" "$STATE/radarr" "$STATE/sonarr" "$STATE/movies" "$STATE/tv"

for port_number in 9696 7878 8989; do
  assert_port_free "$port_number"
done

echo "Resolving immutable ARR consumer images"
ensure_image "$PROWLARR_IMAGE"
ensure_image "$RADARR_IMAGE"
ensure_image "$SONARR_IMAGE"

echo "Starting and configuring the disposable four-node GoNZBNet pool"
"$GONZBNET_SCRIPT" start
"$GONZBNET_SCRIPT" bootstrap
"$GONZBNET_SCRIPT" configure-pool

cat >"$STATE/movie.nzb" <<'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">
  <head><meta type="title">Synthetic.CC0.Movie.2026.1080p.WEB-DL.x264-GONZB</meta></head>
  <file poster="synthetic@example.invalid" date="1786122000" subject="Synthetic.CC0.Movie.2026.1080p.WEB-DL.x264-GONZB [1/1] - &quot;synthetic-movie.txt&quot; yEnc (1/1)">
    <groups><group>alt.binaries.gonzb.synthetic</group></groups>
    <segments><segment bytes="1024" number="1">synthetic-arr-movie@example.invalid</segment></segments>
  </file>
</nzb>
EOF
cat >"$STATE/tv.nzb" <<'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">
  <head><meta type="title">Synthetic.CC0.Show.S01E01.1080p.WEB-DL.x264-GONZB</meta></head>
  <file poster="synthetic@example.invalid" date="1786122000" subject="Synthetic.CC0.Show.S01E01.1080p.WEB-DL.x264-GONZB [1/1] - &quot;synthetic-show.txt&quot; yEnc (1/1)">
    <groups><group>alt.binaries.gonzb.synthetic</group></groups>
    <segments><segment bytes="2048" number="1">synthetic-arr-tv@example.invalid</segment></segments>
  </file>
</nzb>
EOF
cat >"$STATE/movie.json" <<'EOF'
{"category_id":2040,"provenance":{"tool":"arr-consumer-conformance","version":"1","external_id":"synthetic-movie"}}
EOF
cat >"$STATE/tv.json" <<'EOF'
{"category_id":5030,"provenance":{"tool":"arr-consumer-conformance","version":"1","external_id":"synthetic-tv"}}
EOF

echo "Submitting and explicitly publishing two locally generated CC0 NZBs"
admin_request node-a 18081 /api/v1/admin/auth/roles \
  '{"id":"arr-seed","name":"ARR seed submit only","permissions":["uploader.submissions.create"]}' >/dev/null
admin_request node-a 18081 /api/v1/admin/auth/users \
  '{"id":"arr-seed","username":"arr-seed","password":"arr-seed-local-2026","enabled":true,"role_ids":["arr-seed"]}' >/dev/null
SEED_TOKEN=$(admin_request node-a 18081 /api/v1/admin/auth/tokens \
  '{"user_id":"arr-seed","name":"arr-consumer-seed"}' | jq -r '.secret')
test -n "$SEED_TOKEN" && test "$SEED_TOKEN" != "null"

GONZB_URL=http://127.0.0.1:18081 GONZB_TOKEN="$SEED_TOKEN" \
  "$ROOT/scripts/gonzb-submit-nzb.sh" "$STATE/movie.nzb" "$STATE/movie.json" >"$STATE/movie-submit.json"
GONZB_URL=http://127.0.0.1:18081 GONZB_TOKEN="$SEED_TOKEN" \
  "$ROOT/scripts/gonzb-submit-nzb.sh" "$STATE/tv.nzb" "$STATE/tv.json" >"$STATE/tv-submit.json"

for submission_name in movie tv; do
  submission_id=$(jq -r '.submission.id' "$STATE/$submission_name-submit.json")
  admin_request node-a 18081 "/api/v1/uploader/submissions/$submission_id/actions/approve" '{}' >/dev/null
  publication=$(admin_request node-a 18081 "/api/v1/uploader/submissions/$submission_id/federation-publications" \
    '{"pool_ids":["pool.e2e"]}')
  printf '%s' "$publication" | jq -e '.items[0].state == "published"' >/dev/null
done

admin_request node-d 18084 /api/v1/admin/auth/roles \
  '{"id":"arr-read","name":"ARR Newznab read only","permissions":["aggregator.releases.read","gonzbnet.search","gonzbnet.get","gonzbnet.resolve_manifest"]}' >/dev/null
admin_request node-d 18084 /api/v1/admin/gonzbnet/pools/pool.e2e/role-access \
  '{"role_id":"arr-read","can_search":true,"can_get":true,"can_resolve_manifest":true}' >/dev/null
admin_request node-d 18084 /api/v1/admin/auth/users \
  '{"id":"arr-read","username":"arr-read","password":"arr-read-local-2026","enabled":true,"role_ids":["arr-read"]}' >/dev/null
ARR_TOKEN=$(admin_request node-d 18084 /api/v1/admin/auth/tokens \
  '{"user_id":"arr-read","name":"arr-consumers"}' | jq -r '.secret')
test -n "$ARR_TOKEN" && test "$ARR_TOKEN" != "null"

forbidden_code=$(curl --silent --show-error -o "$STATE/least-privilege-response.json" -w '%{http_code}' \
  -H "Authorization: Bearer $ARR_TOKEN" \
  http://127.0.0.1:18084/api/v1/uploader/submissions)
[ "$forbidden_code" = "401" ] || [ "$forbidden_code" = "403" ] || {
  echo "ARR viewer token unexpectedly gained uploader queue access (HTTP $forbidden_code)" >&2
  exit 1
}

attempts=0
while [ "$attempts" -lt 12 ]; do
  admin_request node-a 18081 /api/v1/admin/gonzbnet/sync/push '{}' >/dev/null
  sleep 1
  admin_request node-d 18084 /api/v1/admin/gonzbnet/sync/pull '{}' >/dev/null
  federated_count=$(db_scalar gonzbnet_d \
    "SELECT count(*) FROM federated_release_sources source JOIN federated_release_cards card USING (release_id) WHERE source.pool_id = 'pool.e2e' AND source.resolvable AND card.title LIKE 'Synthetic.CC0.%';")
  if [ "$federated_count" = "2" ]; then
    break
  fi
  attempts=$((attempts + 1))
  sleep 5
done
[ "$attempts" -lt 12 ] || {
  echo "Node D did not receive both explicitly published synthetic releases" >&2
  exit 1
}
newznab_search 18084 "$ARR_TOKEN" "Synthetic.CC0.Movie" "$STATE/node-d-movie-search.xml"
newznab_search 18084 "$ARR_TOKEN" "Synthetic.CC0.Show" "$STATE/node-d-tv-search.xml"
grep -Fq 'Synthetic.CC0.Movie.2026.1080p.WEB-DL.x264-GONZB' "$STATE/node-d-movie-search.xml"
grep -Fq 'Synthetic.CC0.Show.S01E01.1080p.WEB-DL.x264-GONZB' "$STATE/node-d-tv-search.xml"

echo "Starting pinned Prowlarr and configuring Generic Newznab against Node D"
start_arr_container "$PROWLARR_CONTAINER" "$PROWLARR_IMAGE" "$STATE/prowlarr"
wait_http 9696
PROWLARR_KEY=$(api_key_from_config "$STATE/prowlarr/config.xml")
test -n "$PROWLARR_KEY"
curl -fsS -H "X-Api-Key: $PROWLARR_KEY" http://127.0.0.1:9696/api/v1/system/status | \
  jq -e --arg version "$PROWLARR_VERSION" '.version == $version' >/dev/null
curl -fsS -H "X-Api-Key: $PROWLARR_KEY" http://127.0.0.1:9696/api/v1/indexer/schema >"$STATE/prowlarr-schema.json"
app_profile_id=$(curl -fsS -H "X-Api-Key: $PROWLARR_KEY" http://127.0.0.1:9696/api/v1/appprofile | jq -r '.[0].id')
jq --arg token "$ARR_TOKEN" --argjson profile "$app_profile_id" \
  '[.[] | select(.name == "Generic Newznab")][0]
   | .name="GoNZB Node D"
   | .appProfileId=$profile
   | (.fields[] | select(.name == "baseUrl").value)="http://127.0.0.1:18084"
   | (.fields[] | select(.name == "apiPath").value)="/api"
   | (.fields[] | select(.name == "apiKey").value)=$token' \
  "$STATE/prowlarr-schema.json" >"$STATE/prowlarr-indexer.json"

prowlarr_test_code=$(curl -sS -o "$STATE/prowlarr-test-response.json" -w '%{http_code}' \
  -H "X-Api-Key: $PROWLARR_KEY" -H 'Content-Type: application/json' \
  -d @"$STATE/prowlarr-indexer.json" http://127.0.0.1:9696/api/v1/indexer/test)
[ "$prowlarr_test_code" = "200" ] || {
  echo "Prowlarr Generic Newznab test returned HTTP $prowlarr_test_code" >&2
  exit 1
}
prowlarr_create_code=$(curl -sS -o "$STATE/prowlarr-create-response.json" -w '%{http_code}' \
  -H "X-Api-Key: $PROWLARR_KEY" -H 'Content-Type: application/json' \
  -d @"$STATE/prowlarr-indexer.json" http://127.0.0.1:9696/api/v1/indexer)
[ "$prowlarr_create_code" = "201" ] || {
  echo "Prowlarr Generic Newznab create returned HTTP $prowlarr_create_code" >&2
  exit 1
}
prowlarr_indexer_id=$(jq -r '.id' "$STATE/prowlarr-create-response.json")

curl -fsS --get -H "X-Api-Key: $PROWLARR_KEY" \
  --data-urlencode 'query=Synthetic.CC0.Movie' \
  --data-urlencode "indexerIds=$prowlarr_indexer_id" \
  --data-urlencode 'type=movie' \
  http://127.0.0.1:9696/api/v1/search >"$STATE/prowlarr-movie-search.json"
curl -fsS --get -H "X-Api-Key: $PROWLARR_KEY" \
  --data-urlencode 'query=Synthetic.CC0.Show' \
  --data-urlencode "indexerIds=$prowlarr_indexer_id" \
  --data-urlencode 'type=tvsearch' \
  http://127.0.0.1:9696/api/v1/search >"$STATE/prowlarr-tv-search.json"
jq -e \
  'length == 1
   and .[0].title == "Synthetic.CC0.Movie.2026.1080p.WEB-DL.x264-GONZB"
   and .[0].size == 1024
   and any(.[0].categories[]; .id == 2040)' \
  "$STATE/prowlarr-movie-search.json" >/dev/null
jq -e \
  'length == 1
   and .[0].title == "Synthetic.CC0.Show.S01E01.1080p.WEB-DL.x264-GONZB"
   and .[0].size == 2048
   and any(.[0].categories[]; .id == 5030)' \
  "$STATE/prowlarr-tv-search.json" >/dev/null

for release_kind in movie tv; do
  download_url=$(jq -r '.[0].downloadUrl' "$STATE/prowlarr-$release_kind-search.json")
  release_guid=$(jq -r '.[0].guid' "$STATE/prowlarr-$release_kind-search.json")
  curl -fsSL "$download_url" >"$STATE/prowlarr-$release_kind-grab.nzb"
  newznab_get 18084 "$ARR_TOKEN" "$release_guid" "$STATE/node-d-$release_kind-grab.nzb"
  cmp "$STATE/prowlarr-$release_kind-grab.nzb" "$STATE/node-d-$release_kind-grab.nzb"
done
grep -Fq 'synthetic-arr-movie@example.invalid' "$STATE/prowlarr-movie-grab.nzb"
grep -Fq 'synthetic-arr-tv@example.invalid' "$STATE/prowlarr-tv-grab.nzb"
curl -fsS -H "X-Api-Key: $PROWLARR_KEY" \
  'http://127.0.0.1:9696/api/v1/history?page=1&pageSize=20&sortKey=date&sortDirection=descending' \
  >"$STATE/prowlarr-history.json"
jq -e \
  '([.records[] | select(.eventType == "indexerQuery" and .successful == true and .data.queryResults == "1")] | length) >= 2
   and ([.records[] | select(.eventType == "releaseGrabbed" and .successful == true)] | length) >= 2' \
  "$STATE/prowlarr-history.json" >/dev/null

echo "Starting pinned Radarr and Sonarr against the same federated Node D endpoint"
start_arr_container "$RADARR_CONTAINER" "$RADARR_IMAGE" "$STATE/radarr" \
  -v "$STATE/movies:/movies"
start_arr_container "$SONARR_CONTAINER" "$SONARR_IMAGE" "$STATE/sonarr" \
  -v "$STATE/tv:/tv"
wait_http 7878
wait_http 8989
RADARR_KEY=$(api_key_from_config "$STATE/radarr/config.xml")
SONARR_KEY=$(api_key_from_config "$STATE/sonarr/config.xml")
test -n "$RADARR_KEY" && test -n "$SONARR_KEY"
curl -fsS -H "X-Api-Key: $RADARR_KEY" http://127.0.0.1:7878/api/v3/system/status | \
  jq -e --arg version "$RADARR_VERSION" '.appName == "Radarr" and .version == $version' >/dev/null
curl -fsS -H "X-Api-Key: $SONARR_KEY" http://127.0.0.1:8989/api/v3/system/status | \
  jq -e --arg version "$SONARR_VERSION" '.appName == "Sonarr" and .version == $version' >/dev/null

for app_name in radarr sonarr; do
  if [ "$app_name" = "radarr" ]; then
    app_port=7878
    app_key=$RADARR_KEY
  else
    app_port=8989
    app_key=$SONARR_KEY
  fi
  curl -fsS -H "X-Api-Key: $app_key" \
    "http://127.0.0.1:$app_port/api/v3/indexer/schema" >"$STATE/$app_name-schema.json"
  jq --arg token "$ARR_TOKEN" \
    '[.[] | select(.implementation == "Newznab")][0]
     | .name="GoNZB Node D"
     | .enableRss=true
     | .enableAutomaticSearch=true
     | .enableInteractiveSearch=true
     | (.fields[] | select(.name == "baseUrl").value)="http://127.0.0.1:18084"
     | (.fields[] | select(.name == "apiPath").value)="/api"
     | (.fields[] | select(.name == "apiKey").value)=$token' \
    "$STATE/$app_name-schema.json" >"$STATE/$app_name-indexer.json"
  test_code=$(curl -sS -o "$STATE/$app_name-test-response.json" -w '%{http_code}' \
    -H "X-Api-Key: $app_key" -H 'Content-Type: application/json' \
    -d @"$STATE/$app_name-indexer.json" \
    "http://127.0.0.1:$app_port/api/v3/indexer/test")
  [ "$test_code" = "200" ] || {
    echo "$app_name Newznab test returned HTTP $test_code" >&2
    exit 1
  }
  create_code=$(curl -sS -o "$STATE/$app_name-create-response.json" -w '%{http_code}' \
    -H "X-Api-Key: $app_key" -H 'Content-Type: application/json' \
    -d @"$STATE/$app_name-indexer.json" \
    "http://127.0.0.1:$app_port/api/v3/indexer")
  [ "$create_code" = "201" ] || {
    echo "$app_name Newznab create returned HTTP $create_code" >&2
    exit 1
  }
done

movie_requests_before=$(grep -F -c 't=movie' "$GONZBNET_STATE/node-d/stdout.log" || true)
tv_requests_before=$(grep -F -c 't=tvsearch' "$GONZBNET_STATE/node-d/stdout.log" || true)
radarr_log_before=$(wc -l <"$STATE/radarr/logs/radarr.txt")
sonarr_log_before=$(wc -l <"$STATE/sonarr/logs/sonarr.txt")

run_arr_command 7878 "$RADARR_KEY" RssSync
run_arr_command 8989 "$SONARR_KEY" RssSync

movie_requests_after=$(grep -F -c 't=movie' "$GONZBNET_STATE/node-d/stdout.log" || true)
tv_requests_after=$(grep -F -c 't=tvsearch' "$GONZBNET_STATE/node-d/stdout.log" || true)
[ "$movie_requests_after" -gt "$movie_requests_before" ] || {
  echo "Radarr did not issue a movie Newznab query to Node D" >&2
  exit 1
}
[ "$tv_requests_after" -gt "$tv_requests_before" ] || {
  echo "Sonarr did not issue a TV Newznab query to Node D" >&2
  exit 1
}
tail -n "+$((radarr_log_before + 1))" "$STATE/radarr/logs/radarr.txt" | grep -Fq 'Processing 1 releases'
tail -n "+$((radarr_log_before + 1))" "$STATE/radarr/logs/radarr.txt" | grep -Fq 'Reports found: 1, Reports grabbed: 0'
tail -n "+$((sonarr_log_before + 1))" "$STATE/sonarr/logs/sonarr.txt" | grep -Fq 'Processing 1 releases'
tail -n "+$((sonarr_log_before + 1))" "$STATE/sonarr/logs/sonarr.txt" | grep -Fq 'Reports found: 1, Reports grabbed: 0'

if grep -Fq "$ARR_TOKEN" \
  "$STATE/prowlarr/logs/prowlarr.txt" \
  "$STATE/radarr/logs/radarr.txt" \
  "$STATE/sonarr/logs/sonarr.txt" \
  "$GONZBNET_STATE/node-d/stdout.log"; then
  echo "GoNZB Newznab token leaked into application logs" >&2
  exit 1
fi

echo "ARR Newznab conformance passed"
echo "  Prowlarr: $PROWLARR_VERSION ($PROWLARR_IMAGE)"
echo "  Radarr: $RADARR_VERSION ($RADARR_IMAGE)"
echo "  Sonarr: $SONARR_VERSION ($SONARR_IMAGE)"
echo "  verified: least-privilege Node D token, signed pool search, Prowlarr caps/search/exact grab, Radarr movie RSS parse, Sonarr TV RSS parse"
echo "  synthetic-only: no media library, downloader, provider, torrent, tracker, or copyrighted payload"
