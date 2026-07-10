# GoNZBNet Scanner Coordination Cleanup

This cleanup narrows the remaining scanner-loop boundary without adding a new
microservice or changing default indexer behavior.

## Scope

- Add an optional scrape range coordinator hook to the existing indexer scrape
  service.
- Wire a GoNZBNet coordinator only when the usenet-indexer module, GoNZBNet
  module, scanner mode, coverage mode, and unassigned scanner work are enabled.
- Publish local signed `RangeClaim`, `RangeComplete`, and `RangeFailed` events
  around indexer scrape ranges.
- Honor trusted remote active claims when `scanner_respect_remote_claims` is
  enabled.
- Honor trusted completed ranges for scanner-mode duplicate suppression.

## Boundary

- Do not add a standalone scanner service.
- Do not expose local usernames, API keys, search history, grab history,
  download history, or NNTP credentials.
- Do not write scanner progress into scrape-owned source fact tables.
- Keep validation overlap behavior unchanged; this integration is for primary
  scanner work only.
