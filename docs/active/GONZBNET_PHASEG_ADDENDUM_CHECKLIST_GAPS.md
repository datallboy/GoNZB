# GoNZBNet Addendum Phase G: Checklist Gap Closure

## Spec Scope

Close addendum checklist items that were not fully covered by Phases A-F:

- `ScannerHeartbeat` event and projection.
- Admin API for viewing node capabilities.
- Admin API for viewing pool group observations as a group catalog.
- Admin API/dashboard data for validation gaps.
- Stale claim penalty projection.

## Implementation Plan

1. Extend `internal/gonzbnet/coverage` with `ScannerHeartbeat`.
2. Add migration `015_gonzbnet_addendum_gaps.up.sql` for scanner heartbeats and
   stale-claim penalty tracking.
3. Project `ScannerHeartbeat` from inbox and pull sync through the existing
   coverage projection path.
4. Add store/admin APIs for:
   - node capability list
   - coverage group catalog
   - validation gaps
   - stale claim penalty materialization
5. Update capability requirements, pool default event types, caps, and wiki.

## Out Of Scope

- WebUI screens for the new read APIs.
- Automatic reputation changes for every stale claim. This phase records stale
  penalties in a table so later reputation policy can decide how to apply them.
