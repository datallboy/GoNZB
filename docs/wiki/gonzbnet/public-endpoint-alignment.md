# Public Endpoint Alignment

GoNZBNet now registers two spec-listed public federation endpoints that were
previously covered by other paths or local admin routes only:

- `POST /gonzbnet/v1/events/batch`
- `GET /gonzbnet/v1/pools/:pool_id/members`

`events/batch` uses the same authenticated, rate-limited inbox handler as
`/gonzbnet/v1/inbox`.

`pools/:pool_id/members` reads the local `pool_members` projection and returns a
`PoolMembers` response containing node IDs, pool roles, status, allowed
capabilities, limits, and membership timestamps.

Both routes are public federation routes, so they are only registered when
`modules.gonzbnet.enabled` and `gonzbnet.http_enabled` are true. They do not
expose local user identity, API keys, search history, grab history, download
history, or role-to-pool RBAC mappings.

Pool checkpoint publication is still deferred.
