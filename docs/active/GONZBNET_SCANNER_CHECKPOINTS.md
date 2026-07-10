# GoNZBNet Scanner Checkpoints

Status: in progress

The GoNZBNet scrape range coordinator now emits a signed
`CoverageCheckpoint` after each successfully completed claimed range. The
checkpoint records the claim, provider scope, group, range start/current/end,
and the scan timestamp, then projects through the same append-only event path
as claims and terminal outcomes.

This establishes checkpoint production at the scanner lifecycle boundary.
Periodic heartbeats, capacity publication, group observations, and richer
release/manifest counters remain to be connected to scanner runtime metrics.
