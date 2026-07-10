# Public Endpoint Alignment

GoNZBNet now registers two spec-listed public federation endpoints that were
previously covered by other paths or local admin routes only:

- `POST /gonzbnet/v1/events/batch`
- `GET /gonzbnet/v1/pools/:pool_id/checkpoint`
- `GET /gonzbnet/v1/pools/:pool_id/members`
- `GET /gonzbnet/v1/peers`

`events/batch` uses the same authenticated, rate-limited inbox handler as
`/gonzbnet/v1/inbox`.

`pools/:pool_id/members` reads the local `pool_members` projection and returns a
`PoolMembers` response containing node IDs, pool roles, status, allowed
capabilities, limits, and membership timestamps.

`pools/:pool_id/checkpoint` returns the latest accepted signed
`PoolCheckpoint` event for the pool, read from the append-only event log via
`trust_pools.latest_checkpoint_event_id`.

`peers` returns enabled peer URLs through the same fanout filter used by
WebSocket gossip. When `gonzbnet.peer_exchange_enabled` is false, it returns an
empty list.

These routes are federation HTTP routes, so they are only registered when
`modules.gonzbnet.enabled` and `gonzbnet.http_enabled` are true. The read routes
require node-signed requests. Pool members and checkpoints require active
membership in the named pool; enabled peer discovery requires active membership
in at least one local pool. They do not expose local user identity, API keys,
search history, grab history, download history, or role-to-pool RBAC mappings.

Checkpoint creation remains event-driven; this endpoint exposes the latest
accepted checkpoint rather than inventing a checkpoint from local JSONB state.
