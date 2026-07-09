# GoNZBNet Addendum Phase A: Capability Registry And Module Switches

## Spec Scope

Add node capability fields, a capability registry table, module enable/disable
config, pool-approved allowed capabilities, and event acceptance checks based on
approved node capabilities.

## Compatibility Plan

- Keep all optional contribution modules disabled by default except consumer,
  index projection, and manifest cache.
- Existing pool admins are treated as capability-unrestricted for moderation
  and bootstrap compatibility.
- Non-admin pool members with no `allowed_capabilities` are treated as consumer
  only.
- Admin APIs can set `allowed_capabilities` and `limits` on pool members.

## Implementation Plan

1. Add config flags under `gonzbnet`:
   `consumer_enabled`, `scanner_enabled`, `index_projection_enabled`,
   `manifest_builder_enabled`, `manifest_cache_enabled`, `validator_enabled`,
   `health_checker_enabled`, `coverage_enabled`, and `scheduler_enabled`.
2. Extend `NodeProfile.capabilities` with participation capability booleans.
3. Add migration `011_gonzbnet_capabilities.up.sql` for
   `federation_node_capabilities`, plus `allowed_capabilities` and
   `limits_json` on `pool_members`.
4. Store node capability snapshots when node profiles are upserted.
5. Extend pool member records/admin APIs with allowed capabilities and limits.
6. Enforce capability requirements in `CanAcceptFederationEventForPools` for
   contribution events:
   - `ReleaseCard`: `scanner` or `indexer`
   - `ResolutionManifest`: `manifest_builder` or `manifest_cache`
   - `HealthAttestation`: `validator` or `health_checker`
   - `Tombstone`: admin role
7. Document no-op behavior for disabled modules.

## Out Of Scope

- Scanner, validator, coverage, and scheduler implementation beyond capability
  advertisement and authorization gates.
- UI components for editing capability policy.
