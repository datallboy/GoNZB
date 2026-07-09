# Admin Requirements Cleanup

This page tracks GoNZBNet admin requirements from the implementation spec that
are not part of a named implementation phase.

## Block And Unblock Node

The admin API supports local node blocking:

- `POST /api/v1/admin/gonzbnet/nodes/:node_id/block`
- `POST /api/v1/admin/gonzbnet/nodes/:node_id/unblock`

These routes use the existing GoNZBNet peer-management permission group. A
blocked node remains recorded locally, but `GetFederationNodePublicKey` already
excludes nodes with `status = 'blocked'`, so future signed events from that node
cannot be verified through the normal trusted-node path.

The local node cannot be blocked through this API. Blocking does not remove
federation node identity rows, event logs, peer records, trust-pool membership,
or user/API-key data.

`/admin/gonzbnet` exposes Block/Unblock actions in the node capabilities table.
