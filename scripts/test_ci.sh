#!/usr/bin/env bash
set -euo pipefail

if ! command -v jq >/dev/null 2>&1; then
    echo "jq is required to enforce the no-skipped-tests CI policy" >&2
    exit 1
fi

args=("$@")
if [ "${#args[@]}" -eq 0 ]; then
    args=(./... -count=1 -timeout=20m)
fi

summary_file="$(mktemp)"
cleanup() {
    rm -f "${summary_file}"
}
trap cleanup EXIT

set +e
go test -json "${args[@]}" |
    jq --unbuffered --join-output '
        if .Action == "skip" and .Test != null then
            "CI_TEST_SKIP\t\(.Package)\t\(.Test)\n"
        elif .Action == "output" then
            .Output
        else
            empty
        end
    ' |
    tee "${summary_file}"
pipeline_status=("${PIPESTATUS[@]}")
set -e

if [ "${pipeline_status[0]}" -ne 0 ]; then
    exit "${pipeline_status[0]}"
fi
if [ "${pipeline_status[1]}" -ne 0 ]; then
    echo "failed to inspect Go test events" >&2
    exit "${pipeline_status[1]}"
fi
if [ "${pipeline_status[2]}" -ne 0 ]; then
    echo "failed to write Go test output" >&2
    exit "${pipeline_status[2]}"
fi
if grep -q '^CI_TEST_SKIP' "${summary_file}"; then
    echo "CI does not permit skipped tests" >&2
    exit 1
fi
