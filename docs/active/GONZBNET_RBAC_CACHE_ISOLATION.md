# GoNZBNet Pool RBAC And Cache Isolation

## Spec Scope

Close the local authorization gap in the Newznab get path:

- require local `gonzbnet.get` and `gonzbnet.resolve_manifest` permissions;
- require the principal to have pool-level `can_get` and
  `can_resolve_manifest` access to a source for the requested release;
- apply authorization before returning a generic aggregator blob-cache hit;
- never use shared `aggregator_release_cache` rows as GoNZBNet search results,
  because those rows do not retain pool identity.

## Implementation Plan

1. Add a PostgreSQL principal/release authorization query over user and role
   federation pool grants.
2. Add an optional aggregator source get-authorizer contract and invoke it
   before the generic NZB cache path.
3. Implement that contract in the GoNZBNet source and retain the same check in
   direct source calls.
4. Exclude persisted GoNZBNet rows from the generic search-cache read path;
   authorized results continue to come from `federated_release_cards`.
5. Add unit tests for pool denial, authorized get, cache-hit denial, and shared
   search-cache isolation.

## Out Of Scope

- Remote node authorization, which is handled by signed manifest requests and
  trust-pool membership.
- User-specific pool-grant admin UI.
- Changing Newznab IDs or existing non-GoNZBNet source caching.

