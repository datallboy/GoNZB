#!/bin/sh
set -eu

if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
  echo "usage: gonzb-submit-nzb.sh NZB_PATH [METADATA_JSON_PATH]" >&2
  exit 64
fi

nzb_path=$1
metadata_path=${2:-}
: "${GONZB_URL:?set GONZB_URL to the GoNZB base URL}"
: "${GONZB_TOKEN:?set GONZB_TOKEN to a least-privilege API token}"

if [ ! -f "$nzb_path" ]; then
  echo "NZB path is not a regular file: $nzb_path" >&2
  exit 66
fi

endpoint=${GONZB_URL%/}/api/v1/uploader/submissions
if [ -n "$metadata_path" ]; then
  if [ ! -f "$metadata_path" ]; then
    echo "metadata path is not a regular file: $metadata_path" >&2
    exit 66
  fi
  exec curl --fail-with-body --silent --show-error \
    --retry 3 --retry-all-errors --connect-timeout 10 --max-time 120 \
    -H "Authorization: Bearer ${GONZB_TOKEN}" \
    -F "nzb=@${nzb_path};type=application/x-nzb" \
    -F "metadata=<${metadata_path};type=application/json" \
    "$endpoint"
fi

exec curl --fail-with-body --silent --show-error \
  --retry 3 --retry-all-errors --connect-timeout 10 --max-time 120 \
  -H "Authorization: Bearer ${GONZB_TOKEN}" \
  -F "nzb=@${nzb_path};type=application/x-nzb" \
  "$endpoint"
