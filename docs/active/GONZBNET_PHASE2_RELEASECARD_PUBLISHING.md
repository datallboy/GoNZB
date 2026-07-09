# GoNZBNet Phase 2 ReleaseCard Publishing

Status: in progress

Scope:

- Map local public-ready indexer catalog releases to GoNZBNet ReleaseCard bodies.
- Sign local ReleaseCard events with the Phase 1 node identity.
- Append accepted events to `federation_events`.
- Project accepted local cards into `federated_release_cards` and
  `federated_release_sources`.
- Keep publishing disabled by default with
  `gonzbnet.publish_release_cards_enabled`.

Source tables:

- `releases`
- `release_catalog_files`
- `release_newsgroups`
- existing catalog article helpers for manifest-ID inputs

Boundary:

- No remote federation.
- No GoNZBNet Newznab source registration.
- No manifest fetch endpoints.
- No writes to existing indexer release/catalog tables.
