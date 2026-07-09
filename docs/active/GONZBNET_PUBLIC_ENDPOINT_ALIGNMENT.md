# GoNZBNet Public Endpoint Alignment

This cleanup aligns two small public federation endpoints with the implementation
spec without starting a new phase branch.

Implemented:

- `POST /gonzbnet/v1/events/batch` is registered as a rate-limited alias for
  the existing inbox batch handler.
- `GET /gonzbnet/v1/pools/:pool_id/members` returns the local trust-pool member
  projection as a `PoolMembers` response.

Behavior:

- Both routes remain behind `modules.gonzbnet.enabled` and
  `gonzbnet.http_enabled`.
- The member endpoint exposes node-level pool membership only. It does not
  expose local usernames, API keys, searches, grabs, downloads, or local RBAC
  assignments.
- Pool checkpoint publication remains deferred with the existing trust-pool
  checkpoint work.
