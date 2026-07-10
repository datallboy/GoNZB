# GoNZBNet Assignment-Driven Scanner Cleanup

This cleanup lets scanner nodes consume existing `CoverageAssignment` range
work through the existing usenet-indexer scrape loop.

## Scope

- Extend the optional scrape range coordinator with an assigned-range provider.
- When `gonzbnet.coverage_mode` is `scheduler` or `automatic`, request
  dedup-aware scanner work suggestions for the local node.
- Require executable article ranges when requesting suggestions for the scrape
  loop so time-window-only assignments remain visible to admin/scheduler views
  but do not consume the assigned-range fetch limit.
- Fetch suggested range assignments explicitly through XOVER.
- Publish signed local `RangeClaim`, `RangeComplete`, and `RangeFailed` events
  with the source `assignment_id`.
- Keep explicit assignment fetches separate from latest/backfill cursors so
  assigned coverage work does not mutate scrape progress.

## Boundary

- Only article range assignments are consumed by this cleanup.
- Time-window assignment execution remains future work.
- Automatic stale-claim reassignment is handled separately for article ranges
  when automatic coverage mode is enabled.
