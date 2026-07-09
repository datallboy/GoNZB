# Phase J Pool And Moderation UI

Phase J extends the local GoNZBNet admin page with trust-pool, pool-member, and
tombstone moderation workflows.

## Trust Pools

The WebUI now calls the existing local pool admin APIs:

- `GET /api/v1/admin/gonzbnet/pools`
- `POST /api/v1/admin/gonzbnet/pools`
- `GET /api/v1/admin/gonzbnet/pools/:pool_id/members`
- `POST /api/v1/admin/gonzbnet/pools/:pool_id/members`
- `POST /api/v1/admin/gonzbnet/pools/:pool_id/members/:node_id/revoke`

The page can save pool policy basics, list pools, add/update members, revoke
members, and show allowed contribution capabilities for the selected pool.

## Tombstones

The WebUI also exposes existing tombstone moderation APIs:

- `GET /api/v1/admin/gonzbnet/moderation/tombstones`
- `POST /api/v1/admin/gonzbnet/moderation/tombstones`

Creating a tombstone signs a local GoNZBNet `Tombstone` event, appends it to the
local event log, and updates local tombstone projections. Local-only tombstones
remain local; pool tombstones use the existing pool visibility rules.

## RBAC Boundary

The page route remains guarded by `gonzbnet.admin.pools`. Tombstone reads and
writes continue to require `gonzbnet.admin.moderation` at the backend. If a user
has pool administration but not moderation permission, the rest of the page
continues to load and the tombstone table is empty.

The UI does not expose local usernames, API keys, search history, grab history,
or download history.
