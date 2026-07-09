# GoNZBNet Phase 5 RBAC And Aggregator Integration

Scope:

- Add local GoNZBNet permissions for search, get, manifest resolution, and admin surfaces.
- Preserve local-only user auth; no cross-node login and no remote user context.
- Add local pool access tables keyed by local role IDs and user IDs.
- Add a `gonzbnet` aggregator source that reads the local federated ReleaseCard cache.
- Filter federated results by local permission and pool access from the request principal.
- Keep Newznab result URLs local by returning normal aggregator releases.
- Keep remote federation out of search; the source must not call peers.

Implementation plan:

1. Add auth permission constants and carry role IDs on local principals.
2. Attach principals to `context.Context` in existing auth middleware.
3. Add `aggregator.sources.gonzbnet.enabled` to bootstrap and runtime config.
4. Add PostgreSQL pool access tables and query helpers.
5. Add `internal/aggregator/sources/gonzbnet` for local federated-card searches.
6. Wire the source into aggregator runtime only when enabled and PG is present.
7. Add tests for permission filtering, pool filtering, and local Newznab link behavior.

Out of scope:

- Remote live query fanout.
- Manifest fetch/generation on get.
- Trust-pool administration UI.
- Pool invitation or enrollment flows.
