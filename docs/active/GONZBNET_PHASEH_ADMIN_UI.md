# GoNZBNet Phase H: Admin UI

## Spec Scope

Add WebUI coverage for the GoNZBNet admin API surfaces:

- node capability view
- coverage dashboard
- group catalog
- active/stale claims and completed ranges
- coverage score
- duplicate work
- validation gaps
- manual assignment and claim/outcome actions

## Implementation Plan

1. Add TypeScript response types and API helpers for existing
   `/api/v1/admin/gonzbnet/*` endpoints.
2. Add an `AdminGoNZBNetPage` with dense operational panels rather than a
   marketing page.
3. Register `/admin/gonzbnet` route guarded by `gonzbnet.admin.pools`.
4. Add sidebar navigation when the user has GoNZBNet admin permissions.
5. Run UI build and Go tests.

## Out Of Scope

- New backend endpoints.
- WebSocket/live updates.
- Full editor UI for trust pool membership policies.
