# Phase K Peer Management

Phase K adds local peer-management actions for GoNZBNet admins.

## Permission

`gonzbnet.admin.peers` was added and included in the built-in admin role. Peer
management routes use this permission separately from pool and moderation
administration.

## Admin API

The following local endpoints were added:

- `POST /api/v1/admin/gonzbnet/peers`
- `POST /api/v1/admin/gonzbnet/peers/:peer_id/enable`
- `POST /api/v1/admin/gonzbnet/peers/:peer_id/disable`
- `POST /api/v1/admin/gonzbnet/sync/pull`
- `POST /api/v1/admin/gonzbnet/sync/push`
- `POST /api/v1/admin/gonzbnet/sync/gossip`

Peer upsert stores a manual peer URL in `federation_peers`. Enable/disable
toggles the local peer row and does not delete cursor or delivery history.

The sync actions instantiate the existing GoNZBNet sync service and run a single
pull, push, or gossip pass against enabled peers.

## WebUI

The GoNZBNet admin page now includes:

- manual peer URL add form
- pull, push, and gossip action buttons
- enable/disable buttons in the peer diagnostics table

These actions are local admin operations only. They do not expose local user
credentials, search history, grab history, or download history to peers.
