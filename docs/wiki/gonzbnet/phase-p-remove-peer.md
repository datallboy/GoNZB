# Phase P: Remove Peer

Phase P adds local peer removal for GoNZBNet operators.

## Admin API

The peer-management API adds:

- `DELETE /api/v1/admin/gonzbnet/peers/:peer_id`

The route uses the existing `gonzbnet.admin.peers` permission group. Removing a
peer deletes only the local `federation_peers` row. Existing schema constraints
cascade local peer cursor and delivery rows.

This does not remove:

- federation node identity records
- trust-pool membership records
- accepted or rejected event-log records
- local user or API-key records

## Admin UI

`/admin/gonzbnet` now includes a Remove button in the peer diagnostics table.
Enable/disable remains available for reversible local pause behavior; Remove is
for deleting the local peer record.
