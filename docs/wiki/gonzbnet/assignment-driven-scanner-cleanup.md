# Assignment-Driven Scanner Cleanup

Scanner nodes can now consume existing `CoverageAssignment` range work through
the existing usenet-indexer scrape loop.

When GoNZBNet scanner coordination is enabled and
`gonzbnet.coverage_mode` is `scheduler` or `automatic`, the scrape service asks
the local GoNZBNet coordinator for dedup-aware scanner suggestions assigned to
the local node. Range assignments are fetched explicitly with XOVER, inserted
through the normal indexer header path, and reported with signed
`RangeClaim`/`RangeComplete`/`RangeFailed` events that include the
`assignment_id`.

Assigned range fetches do not update latest or backfill scrape cursors. They are
tracked through GoNZBNet coverage outcomes instead.

This cleanup consumes existing range assignments. It does not create new
assignments, execute time-window assignments, or implement automatic stale-claim
failover.
