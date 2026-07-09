# GoNZBNet Phase P: Remove Peer Admin Action

## Spec Scope

Add the local admin action:

- remove peer

## Implementation Plan

1. Add a pgindex store method to delete manual federation peers by ID.
2. Add an admin API endpoint under `/api/v1/admin/gonzbnet/peers/:peer_id`.
3. Add TypeScript API helper and a remove button in the peer diagnostics table.
4. Document behavior under `docs/wiki/gonzbnet/`.
5. Run UI build and Go tests.

## Out Of Scope

- Removing trusted nodes or pool members.
- Emitting federation events for local peer-list changes.
- Changing existing enable/disable peer behavior.
