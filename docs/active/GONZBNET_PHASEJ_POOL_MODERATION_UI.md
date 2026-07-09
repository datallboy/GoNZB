# GoNZBNet Phase J: Pool And Moderation Admin UI

## Spec Scope

Close the remaining admin UI gap for trust-pool and tombstone moderation
workflows that already have local admin APIs:

- list/create/update trust pools
- list/add/revoke pool members with allowed capabilities
- list active/all tombstones
- create local or pool tombstones

## Implementation Plan

1. Add TypeScript types and API helpers for existing pool, member, and
   moderation endpoints.
2. Extend `/admin/gonzbnet` with compact trust-pool and tombstone panels.
3. Keep all actions local-RBAC guarded through existing backend permissions.
4. Document the UI behavior in `docs/wiki/gonzbnet/`.
5. Run UI build and Go tests.

## Out Of Scope

- Join request workflow.
- Key rotation/export.
- Pool checkpoint publication.
- Force-sync or force-resolve actions.
