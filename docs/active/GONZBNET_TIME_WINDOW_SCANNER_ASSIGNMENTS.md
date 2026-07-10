# GoNZBNet Time-Window Scanner Assignments

This cleanup lets scanner nodes execute `CoverageAssignment` work that is
expressed as a time window instead of a concrete article-number range.

## Scope

- Let the scrape coordinator return both article-range and time-window
  assignments for the local node.
- Resolve time windows locally against the configured NNTP provider using
  bounded XOVER date probes.
- Convert the resolved window into a concrete article range before fetching
  headers through the existing scrape insert path.
- Publish signed `TimeWindowClaim` events for time-window assignments.
- Publish existing `RangeComplete` or `RangeFailed` events for the resolved
  concrete article range.
- Keep assigned work separate from latest/backfill checkpoints.

## Boundary

- This cleanup does not create new assignments.
- Time-window resolution assumes article numbers are broadly chronological for
  the target group/provider.
- If a window cannot be resolved to a concrete range, the scrape loop skips it
  without publishing a claim or mutating scrape cursors.
- Stale time-window reassignment is handled by the stale-claim reassigner in
  automatic coverage mode.
