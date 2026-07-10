# GoNZBNet Scanner Checkpoints

Status: in progress

The GoNZBNet scrape range coordinator now emits a signed
`CoverageCheckpoint` after each successfully completed claimed range. The
checkpoint records the claim, provider scope, group, range start/current/end,
and the scan timestamp, then projects through the same append-only event path
as claims and terminal outcomes.

This establishes checkpoint production at the scanner lifecycle boundary.
The scrape service also exposes an optional run observer. When GoNZBNet is
enabled, the coordinator consumes completed scrape metrics to publish signed
`ScannerCapacity` and `ScannerHeartbeat` events through the same event chain.
Successful range completion also emits a provider-scoped `GroupObservation`
using the observed article range and processed-header count. Richer in-progress
checkpoint counters remain open.
