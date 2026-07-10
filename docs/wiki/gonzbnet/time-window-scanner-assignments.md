# Time-Window Scanner Assignments

Scanner nodes can consume existing `CoverageAssignment` records that specify
`window_start` and `window_end` instead of `range_start` and `range_end`.

The scrape loop asks the local GoNZBNet coordinator for assigned scanner work.
Range assignments are still fetched directly. Time-window assignments are first
resolved against the local NNTP provider by probing XOVER dates within the
group low/high article bounds. When a concrete article range is found, the
scanner publishes a signed `TimeWindowClaim`, fetches the resolved range through
the normal header insert path, and reports the resolved range with
`RangeComplete` or `RangeFailed`.

Assigned time-window work does not advance latest or backfill checkpoints. The
coverage assignment status is driven by the signed outcome event, not by local
scrape cursors.

If the local provider cannot resolve the window to a concrete article range, the
assignment is skipped for that run without claiming it. This avoids claiming
work that the node cannot prove it can execute.
