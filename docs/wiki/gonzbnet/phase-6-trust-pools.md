# Phase 6 Trust Pools

Phase 6 introduces protected trust pools. It keeps authentication local and
continues to authenticate federation traffic by node identity, not by user.

Pool events:

- `PoolGenesis` creates or updates the local trust-pool projection.
- `PoolJoinRequest` is validated and stored but does not grant membership by
  itself.
- `PoolMemberApproved` activates a member role after enough active admin
  approval signatures are verified.
- `PoolMemberRevoked` revokes all roles for a node after the pool moderation
  threshold is met.

Projection tables:

- `trust_pools` stores pool policy, thresholds, accepted event types, and
  minimum node trust.
- `pool_members` stores active and revoked roles for nodes in each pool.

Authorization:

- Pool control events are validated before they are appended as accepted
  events.
- Non-control events for a known protected pool are accepted only when the
  author is an active pool member.
- Revoked members fail the same active-member check.
- Pool policy can restrict accepted event types and enforce minimum local node
  trust score.
- Unknown pool IDs are not treated as protected yet, preserving existing
  single-node/local-pool behavior.

Local admin API:

- `GET /api/v1/admin/gonzbnet/pools`
- `POST /api/v1/admin/gonzbnet/pools`
- `GET /api/v1/admin/gonzbnet/pools/:pool_id/members`
- `POST /api/v1/admin/gonzbnet/pools/:pool_id/members`
- `POST /api/v1/admin/gonzbnet/pools/:pool_id/members/:node_id/revoke`

The admin routes require `gonzbnet.admin.pools`.

Public federation API:

- `GET /gonzbnet/v1/pools/:pool_id/members` returns the local node-level
  member projection for federation peer discovery.

Later cleanup:

- `PoolCheckpoint` validation and projection now recomputes Merkle roots over
  the accepted append-only pool event log and stores the latest checkpoint on
  `trust_pools`.

Out of scope:

- Tombstones and moderation records.
- Pool invitation UI.
- Automatic trust scoring.
