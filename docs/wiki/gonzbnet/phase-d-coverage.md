# Phase D Coverage Events And Manual Assignments

Phase D adds signed coverage coordination events and local admin APIs for manual
assignment of scanner work.

## Event Types

`internal/gonzbnet/coverage` defines:

- `ScannerCapacity`
- `GroupObservation`
- `CoveragePlan`
- `CoverageAssignment`
- `RangeClaim`
- `TimeWindowClaim`
- `CoverageCheckpoint`
- `RangeComplete`
- `RangeFailed`

Pool authorization requires scanner/coverage capability for scanner-originated
claims and outcomes, and admin/coverage-coordinator capability for plans,
assignments, and checkpoints.

## Projections

Migration `014_gonzbnet_coverage.up.sql` adds coverage tables for capacities,
observations, plans, assignments, claims, checkpoints, and outcomes. Projections
retain the signed event body as JSON and expose dashboard-oriented rows:

- active assignments
- active claims
- stale claims
- completed and failed ranges

Claims become stale when `expires_at <= now()` and no matching range outcome has
been projected.

## Admin API

The following endpoints are protected by the existing GoNZBNet pool-admin RBAC
permission:

- `GET /api/v1/admin/gonzbnet/coverage?pool_id=...`
- `POST /api/v1/admin/gonzbnet/coverage/assignments`
- `POST /api/v1/admin/gonzbnet/coverage/claims`
- `POST /api/v1/admin/gonzbnet/coverage/complete`
- `POST /api/v1/admin/gonzbnet/coverage/failed`

The write endpoints create signed local coverage events, append them to the
federation event log, and project them locally.

## Public Read API

Later cleanup adds signed node-to-node read endpoints:

- `GET /gonzbnet/v1/coverage/groups`
- `GET /gonzbnet/v1/coverage/plan`
- `GET /gonzbnet/v1/coverage/work`
- `GET /gonzbnet/v1/capabilities/nodes`

These require signed node authentication and active membership in the requested
pool.

## Public Write API

Later cleanup adds signed node-to-node write convenience endpoints:

- `POST /gonzbnet/v1/coverage/claim`
- `POST /gonzbnet/v1/coverage/checkpoint`

These accept constrained coverage event types and reuse the normal inbox
verification, pool authorization, append-only log, and projection path.

## Current Boundary

This phase does not execute scanner work. Scanner modules can read assignments,
publish claims, and report outcomes through the event schemas added here.
