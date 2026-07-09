# GoNZBNet Config Validation Cleanup

## Scope

This cleanup aligns bootstrap validation with the GoNZBNet modular-monolith shape.

## Implementation

- Treat `modules.gonzbnet.enabled` as a valid enabled module in `ValidateEffective`.
- Require `store.pg_dsn` when `modules.gonzbnet.enabled` is true, matching the PostgreSQL-backed event, node, pool, release, manifest, health, and coverage stores.

## Out Of Scope

- Runtime start/stop of a boot-disabled GoNZBNet module from the admin API.
- Changing the existing project convention that uses `modules.gonzbnet.enabled` as the hard module gate.
