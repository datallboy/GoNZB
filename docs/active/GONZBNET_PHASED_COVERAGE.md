# GoNZBNet Addendum Phase D: Coverage Events And Manual Assignments

## Spec Scope

Add coverage coordination primitives:

- `ScannerCapacity`
- `GroupObservation`
- `CoveragePlan`
- `CoverageAssignment`
- `RangeClaim` / `TimeWindowClaim`
- `CoverageCheckpoint`
- `RangeComplete` / `RangeFailed`
- admin API/UI for manual assignment

## Implementation Plan

1. Add `internal/gonzbnet/coverage` event schemas, validation, and body hashing.
2. Add migration `014_gonzbnet_coverage.up.sql` for scanner capacity, group
   observations, coverage plans, assignments, claims, checkpoints, and range
   outcomes.
3. Add projection methods for coverage events and dashboard reads:
   active assignments, active claims, stale claims, and completed/failed ranges.
4. Add admin APIs under `/api/v1/admin/gonzbnet/coverage` to create manual
   assignments, record local claims/outcomes, and list dashboard state.
5. Extend pool authorization and advertised caps for coverage event types.
6. Project incoming coverage events from inbox and pull sync.

## Assumptions

- This phase implements API-level manual assignment, not WebUI screens. The API
  response shapes are intended for a later UI pass.
- Claim expiration is projection-based. Active claims are claims whose
  `expires_at` is in the future; stale claims are expired claims without a
  matching completion/failure.
- Actual scanner execution is out of scope; scanner modules can consume active
  assignments and emit claim/outcome events later.

## Out Of Scope

- Automated scheduler decisions.
- NNTP scan execution.
- Visual dashboard components.
