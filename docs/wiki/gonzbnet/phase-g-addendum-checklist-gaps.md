# Phase G Addendum Checklist Gaps

Phase G closes remaining addendum checklist items that were not fully covered by
the A-F phase sequence.

## ScannerHeartbeat

`internal/gonzbnet/coverage` now defines `ScannerHeartbeat`, a signed liveness
event for scanner nodes. It records:

- node and pool IDs
- scanner status
- active claim IDs
- currently covered groups

Projection stores the latest heartbeat per node/pool in `scanner_heartbeats`.

## Admin Reads

The GoNZBNet admin API now exposes:

- `GET /api/v1/admin/gonzbnet/nodes/capabilities`
- `GET /api/v1/admin/gonzbnet/coverage/groups`
- `GET /api/v1/admin/gonzbnet/coverage/validation-gaps`
- `POST /api/v1/admin/gonzbnet/coverage/stale-penalties`

These cover capability inspection, pool group catalog views, missing validation
coverage, and explicit stale-claim penalty materialization.

## Stale Penalties

Stale claim penalties are recorded in `coverage_stale_claim_penalties`. They are
not automatically applied to node trust scores yet; the table provides auditable
evidence for later reputation policy.
