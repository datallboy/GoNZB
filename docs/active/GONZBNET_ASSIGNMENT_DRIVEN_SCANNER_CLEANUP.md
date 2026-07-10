# GoNZBNet Assignment-Driven Scanner Cleanup

This cleanup lets scanner nodes consume existing `CoverageAssignment` range
work through the existing usenet-indexer scrape loop.

## Scope

- Extend the optional scrape range coordinator with an assigned-range provider.
- When `gonzbnet.coverage_mode` is `scheduler` or `automatic`, request
  dedup-aware scanner work suggestions for the local node.
- Initially consumed executable article ranges; time-window execution is now
  handled by the follow-up time-window scanner assignment cleanup.
- Fetch suggested range assignments explicitly through XOVER.
- Publish signed local `RangeClaim`, `RangeComplete`, and `RangeFailed` events
  with the source `assignment_id`.
- Keep explicit assignment fetches separate from latest/backfill cursors so
  assigned coverage work does not mutate scrape progress.

## Boundary

- Article range assignments are consumed directly by this cleanup.
- Time-window assignments are consumed by resolving them to article ranges in
  the time-window scanner assignment cleanup.
- Automatic stale-claim reassignment is handled separately for article ranges
  when automatic coverage mode is enabled.
