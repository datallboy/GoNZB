# GoNZBNet Phase 7 Resolution Manifests And NZB Generation

Scope:

- Add a typed `ResolutionManifest` schema.
- Compute and validate deterministic `man_...` IDs from canonical
  `manifest_core`.
- Cache verified manifests and generated NZBs in PostgreSQL.
- Track manifest sources from ReleaseCard projections.
- Add signed `POST /gonzbnet/v1/manifests/:manifest_id/request`.
- Add `GET /gonzbnet/v1/manifests/:manifest_id` for locally cached
  manifests.
- Add a GoNZBNet manifest resolver used by the `gonzbnet` aggregator source.
- Generate NZB XML from a verified manifest and cache it.

Implementation plan:

1. Add `internal/gonzbnet/manifest` for schema, ID validation, and NZB XML
   generation.
2. Add migration `008_gonzbnet_resolution_manifests.up.sql`.
3. Add `pgindex` manifest cache/source lookup methods.
4. Add manifest request/get endpoints using node request authentication.
5. Add resolver code that fetches a signed manifest event from a trusted peer,
   verifies it, caches it, generates NZB, and returns a reader.
6. Wire the resolver into the `gonzbnet` aggregator source for Newznab `t=get`.
7. Add tests for manifest ID validation, tamper rejection, NZB parseability,
   and resolver request privacy.

Out of scope:

- Building manifests from local indexer releases.
- Encrypted or compressed manifest bodies.
- Multi-source quorum selection.
- Manifest checkpointing.
