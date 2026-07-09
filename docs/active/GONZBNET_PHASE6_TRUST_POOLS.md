# GoNZBNet Phase 6 Trust Pools

Scope:

- Add trust pool and pool member projection tables.
- Define typed pool event bodies for `PoolGenesis`, `PoolJoinRequest`,
  `PoolMemberApproved`, and `PoolMemberRevoked`.
- Validate M-of-N approvals for member approval and revocation events.
- Project accepted pool events into `trust_pools` and `pool_members`.
- Add a pool authorizer used before storing protected pool events.
- Reject events from non-members or revoked members for protected pools.
- Add minimal local admin APIs to list pools, view members, create a local
  pool, and grant/revoke local pool member state.

Implementation plan:

1. Add `internal/gonzbnet/pools` for event bodies, approval validation, and
   authorization helpers.
2. Add PostgreSQL migration `007_gonzbnet_trust_pools.up.sql`.
3. Add `pgindex` methods for pool projection, member lookup, and
   authorization.
4. Call pool authorization from inbox acceptance and pull sync before appending
   non-pool-control events.
5. Add local admin routes under `/api/v1/admin/gonzbnet/pools`.
6. Add tests for 2-of-3 approvals, non-member rejection, and revoked-member
   rejection.

Out of scope:

- Pool checkpoint Merkle roots.
- Pool invitation UX.
- Admin UI.
- Tombstone/moderation events.
