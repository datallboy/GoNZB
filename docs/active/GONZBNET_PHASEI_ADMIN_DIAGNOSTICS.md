# GoNZBNet Phase I: Admin Diagnostics

## Spec Scope

Close the remaining v1 diagnostics gap from the GoNZBNet implementation spec:

- peer list and sync cursor/status
- event log and validation status
- rejected event/dead-letter visibility
- validation task state
- pool and member state already exist through Phase 6 admin APIs and remain wired
  through existing endpoints

## Implementation Plan

1. Add read-only PostgreSQL store methods for federation peers, event summaries,
   rejected events, peer deliveries, and validation tasks.
2. Add local admin API endpoints under `/api/v1/admin/gonzbnet/diagnostics/*`,
   guarded by existing local GoNZBNet admin permissions.
3. Extend the GoNZBNet admin WebUI with diagnostics panels for peers, events,
   rejected events, deliveries, and validation tasks.
4. Document the diagnostics behavior in `docs/wiki/gonzbnet/`.
5. Run UI build and Go tests.

## Out Of Scope

- Mutating peer configuration.
- Force sync actions.
- Key rotation/export.
- New federation wire endpoints.
