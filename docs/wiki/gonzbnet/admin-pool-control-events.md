# Admin: Pool Control Events

GoNZBNet exposes accepted pool-control events through the local admin API and
UI.

Endpoint:

- `GET /api/v1/admin/gonzbnet/pools/:pool_id/control-events?limit=100`

Included event types:

- `PoolJoinRequest`
- `PoolMemberApproved`
- `PoolMemberRevoked`

The endpoint reads from the accepted local federation event log and returns the
event body JSON for operator review. It is a local admin view only and does not
send user data to peers.
