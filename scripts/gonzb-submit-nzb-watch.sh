#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 || $# -gt 2 ]]; then
  echo "usage: gonzb-submit-nzb-watch.sh OUTPUT_DIR [STATE_DIR]" >&2
  exit 64
fi

output_dir=$1
state_dir=${2:-./data/gonzb-nzb-forwarder}
script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
submit_helper=${GONZB_SUBMIT_HELPER:-$script_dir/gonzb-submit-nzb.sh}
scan_interval=${GONZB_WATCH_INTERVAL_SECONDS:-30}
settle_seconds=${GONZB_WATCH_SETTLE_SECONDS:-60}
retry_base=${GONZB_WATCH_RETRY_BASE_SECONDS:-60}
retry_max=${GONZB_WATCH_RETRY_MAX_SECONDS:-3600}
max_nzb_bytes=${GONZB_WATCH_MAX_NZB_BYTES:-67108864}
run_once=${GONZB_WATCH_ONCE:-0}

require_uint() {
  local name=$1 value=$2
  if [[ ! $value =~ ^[0-9]+$ ]]; then
    echo "$name must be a non-negative integer" >&2
    exit 64
  fi
}

require_uint GONZB_WATCH_INTERVAL_SECONDS "$scan_interval"
require_uint GONZB_WATCH_SETTLE_SECONDS "$settle_seconds"
require_uint GONZB_WATCH_RETRY_BASE_SECONDS "$retry_base"
require_uint GONZB_WATCH_RETRY_MAX_SECONDS "$retry_max"
require_uint GONZB_WATCH_MAX_NZB_BYTES "$max_nzb_bytes"
if (( scan_interval == 0 || retry_base == 0 || retry_max < retry_base || max_nzb_bytes == 0 )); then
  echo "interval, retry base, and maximum NZB bytes must be positive; retry max must be at least retry base" >&2
  exit 64
fi
if [[ $run_once != 0 && $run_once != 1 ]]; then
  echo "GONZB_WATCH_ONCE must be 0 or 1" >&2
  exit 64
fi
if [[ ! -d $output_dir || -L $output_dir ]]; then
  echo "output directory must be a real directory: $output_dir" >&2
  exit 66
fi
if [[ ! -x $submit_helper ]]; then
  echo "submission helper is not executable: $submit_helper" >&2
  exit 69
fi
if [[ -e $state_dir && ( ! -d $state_dir || -L $state_dir ) ]]; then
  echo "state directory must be a real directory: $state_dir" >&2
  exit 66
fi

umask 077
mkdir -p -- "$state_dir/delivered" "$state_dir/retry"

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum -- "$1" | awk '{print $1}'
    return
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 -- "$1" | awk '{print $1}'
    return
  fi
  echo "sha256sum or shasum is required" >&2
  return 69
}

file_signature() {
  if stat -c '%d:%i:%s:%Y' -- "$1" >/dev/null 2>&1; then
    stat -c '%d:%i:%s:%Y' -- "$1"
    return
  fi
  stat -f '%d:%i:%z:%m' -- "$1"
}

file_size() {
  if stat -c '%s' -- "$1" >/dev/null 2>&1; then
    stat -c '%s' -- "$1"
    return
  fi
  stat -f '%z' -- "$1"
}

file_mtime() {
  if stat -c '%Y' -- "$1" >/dev/null 2>&1; then
    stat -c '%Y' -- "$1"
    return
  fi
  stat -f '%m' -- "$1"
}

atomic_state_write() {
  local target=$1 value=$2 temp
  temp=$(mktemp "$state_dir/.state.XXXXXX")
  printf '%s\n' "$value" >"$temp"
  mv -f -- "$temp" "$target"
}

retry_delay() {
  local attempt=$1 delay=$retry_base step=1
  while (( step < attempt && delay < retry_max )); do
    if (( delay > retry_max / 2 )); then
      delay=$retry_max
    else
      delay=$((delay * 2))
    fi
    ((step += 1))
  done
  printf '%s\n' "$delay"
}

scan_once() {
  local path now mtime size before after digest receipt retry_file
  local attempts next_attempt next_epoch delay

  while IFS= read -r -d '' path; do
    [[ -f $path && ! -L $path ]] || continue
    now=$(date +%s)
    mtime=$(file_mtime "$path") || continue
    (( now - mtime >= settle_seconds )) || continue
    size=$(file_size "$path") || continue
    if (( size <= 0 || size > max_nzb_bytes )); then
      echo "skipping NZB outside size limit: $path" >&2
      continue
    fi

    before=$(file_signature "$path") || continue
    digest=$(sha256_file "$path") || continue
    after=$(file_signature "$path") || continue
    if [[ $before != "$after" ]]; then
      echo "NZB changed while hashing; deferring: $path" >&2
      continue
    fi

    receipt=$state_dir/delivered/$digest
    [[ ! -f $receipt ]] || continue
    retry_file=$state_dir/retry/$digest
    attempts=0
    next_epoch=0
    if [[ -f $retry_file ]]; then
      read -r attempts next_epoch <"$retry_file" || true
      [[ $attempts =~ ^[0-9]+$ ]] || attempts=0
      [[ $next_epoch =~ ^[0-9]+$ ]] || next_epoch=0
    fi
    (( now >= next_epoch )) || continue

    echo "submitting completed NZB: $path"
    # Keep successful API response bodies out of service logs; curl errors are
    # still emitted on stderr by the maintained helper.
    if "$submit_helper" "$path" >/dev/null; then
      after=$(file_signature "$path") || continue
      if [[ $before != "$after" || $(sha256_file "$path") != "$digest" ]]; then
        echo "NZB changed during submission; leaving it eligible for the next scan: $path" >&2
        continue
      fi
      atomic_state_write "$receipt" "delivered_at=$(date -u +%Y-%m-%dT%H:%M:%SZ) path=$path"
      rm -f -- "$retry_file"
      echo "delivered completed NZB: $path"
      continue
    fi

    next_attempt=$((attempts + 1))
    delay=$(retry_delay "$next_attempt")
    next_epoch=$((now + delay))
    atomic_state_write "$retry_file" "$next_attempt $next_epoch"
    echo "delivery failed; retrying in ${delay}s: $path" >&2
  done < <(find "$output_dir" -type f \( -iname '*.nzb' \) -print0)
}

while :; do
  scan_once
  [[ $run_once == 0 ]] || exit 0
  sleep "$scan_interval"
done
