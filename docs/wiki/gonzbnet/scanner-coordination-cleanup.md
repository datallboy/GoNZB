# Scanner Coordination Cleanup

GoNZBNet scanner coordination is integrated into the existing usenet-indexer
scrape loop as an optional modular-monolith hook.

The hook is enabled only when all of these are true:

- `modules.usenet_indexer.enabled`
- `modules.gonzbnet.enabled`
- `gonzbnet.scanner_enabled`
- `gonzbnet.coverage_enabled`
- `gonzbnet.scanner_allow_unassigned_work`

When enabled, each configured scrape range asks the local GoNZBNet coordinator
whether the range should be skipped. Trusted active remote `RangeClaim` events
can temporarily block duplicate primary work when
`gonzbnet.scanner_respect_remote_claims` is true. Trusted completed ranges can
advance the local scrape cursor without fetching the same range again.

If the range is not blocked, the coordinator publishes a signed local
`RangeClaim` before XOVER. Successful ranges publish `RangeComplete`; failed
ranges publish `RangeFailed`. Events use the normal local node identity,
append-only event log, and coverage projection path.

This integration does not expose local users, API keys, searches, grabs,
downloads, or NNTP credentials. It also does not add a standalone scanner
process or assignment-driven automatic scanner work selection.
