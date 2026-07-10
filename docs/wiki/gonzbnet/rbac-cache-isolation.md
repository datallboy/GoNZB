# Pool RBAC And Cache Isolation

GoNZBNet applies local authorization before any federated NZB is returned,
including a hit in the aggregator's generic blob cache.

The caller must have:

- `gonzbnet.get`;
- `gonzbnet.resolve_manifest`;
- `can_get=true` and `can_resolve_manifest=true` for at least one pool that is
  a source of the requested release.

Pool access may come from a direct user grant or one of the user's roles. A
direct user row for a pool overrides role rows for that pool, including an
explicit denial.

The shared `aggregator_release_cache` does not store federation pool identity.
GoNZBNet rows in that cache are therefore never used as search results.
Federated search always queries `federated_release_cards` through the GoNZBNet
source, which applies the caller's current pool grants. The shared row may still
resolve a Newznab result ID, but source authorization runs before any cached NZB
payload can be returned.

