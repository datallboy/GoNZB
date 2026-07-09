# GoNZBNet Admin Pool Tombstone Authorization

## Scope

This cleanup aligns local pool tombstone creation with the GoNZBNet trust-pool moderation model. Pool-scoped tombstones are votes by active pool admins, not arbitrary local events.

## Implementation

- Add an `IsActivePoolAdmin(ctx, poolID, nodeID)` store query against `pool_members`.
- Before signing a pool-scoped tombstone, load the local node identity and require that node to be an active admin for the requested pool.
- Return `403 Forbidden` when the local node is not authorized.
- Keep local-only tombstones unchanged; they do not require pool membership and remain private to the instance.

## Validation

- Unauthorized pool tombstone requests must not append federation events.
- Authorized active pool admins still sign, append, and project pool-visible tombstone events.
