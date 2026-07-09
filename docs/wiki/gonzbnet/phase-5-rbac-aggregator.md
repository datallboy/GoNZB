# Phase 5 RBAC And Aggregator Integration

Phase 5 exposes accepted federated ReleaseCards through the local aggregator
and Newznab search path. It searches only the local federated cache and never
broadcasts user searches to peers.

Local permissions:

- `gonzbnet.search` allows federated cache results to appear in local
  aggregator/Newznab searches.
- `gonzbnet.get` is reserved for later local manifest/NZB resolution.
- `gonzbnet.resolve_manifest` is reserved for later manifest fetch/build
  workflows.
- `gonzbnet.admin.read` and `gonzbnet.admin.write` are reserved for local
  administration surfaces.

Pool access:

- `role_federation_pool_access` grants pool access to local auth role IDs.
- `user_federation_pool_access` grants pool access directly to local user IDs.
- Phase 5 currently reads `can_search` grants.
- User IDs and role IDs remain local database identifiers; they are not sent to
  peers.

Aggregator source:

- Enable with `aggregator.sources.gonzbnet.enabled`.
- The source name is `gonzbnet`.
- The source reads `federated_release_cards` and `federated_release_sources`.
- Results require both `gonzbnet.search` and at least one local pool grant.
- Results are filtered to accepted, unexpired cards from granted pools with
  positive trust score.
- Generic cache replay is also filtered so a prior authorized search cannot
  expose cached federated results to an unauthorized principal.

Newznab behavior:

- Federated results are returned as normal local aggregator releases.
- Download links still point to the local `/api?t=get&id=...` endpoint.
- No remote node receives usernames, API keys, search history, grab history, or
  download history.

Out of scope:

- Remote live query fanout.
- Manifest retrieval and NZB generation.
- Trust-pool enrollment and scoring policy.
- Admin UI for editing pool grants.
