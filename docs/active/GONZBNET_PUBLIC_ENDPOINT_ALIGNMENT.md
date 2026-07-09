# GoNZBNet Public Endpoint Alignment

This cleanup aligns two small public federation endpoints with the implementation
spec without starting a new phase branch.

Implemented:

- `POST /gonzbnet/v1/events/batch` is registered as a rate-limited alias for
  the existing inbox batch handler.
- `GET /gonzbnet/v1/pools/:pool_id/checkpoint` returns the latest accepted
  signed `PoolCheckpoint` event for the pool.
- `GET /gonzbnet/v1/pools/:pool_id/members` returns the local trust-pool member
  projection as a `PoolMembers` response.
- `GET /gonzbnet/v1/peers` returns a filtered list of enabled peer URLs when
  peer exchange is enabled.

Behavior:

- These routes remain behind `modules.gonzbnet.enabled` and
  `gonzbnet.http_enabled`.
- The member endpoint exposes node-level pool membership only. It does not
  expose local usernames, API keys, searches, grabs, downloads, or local RBAC
  assignments.
- The peer endpoint returns an empty peer list when
  `gonzbnet.peer_exchange_enabled` is false.
- The checkpoint endpoint returns the original accepted signed event from the
  append-only federation log.
