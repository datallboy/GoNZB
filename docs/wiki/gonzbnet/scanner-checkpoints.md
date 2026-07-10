# Scanner Checkpoints

The scrape range coordinator emits a `CoverageCheckpoint` after a claimed
range completes successfully. It uses the local node identity and event chain,
records provider scope and range progress, appends the signed event, and
projects it into the coverage tables. Failed ranges continue to emit their
existing terminal failure event without a successful checkpoint.
