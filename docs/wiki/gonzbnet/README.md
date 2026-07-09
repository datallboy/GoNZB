# GoNZBNet Wiki

GoNZBNet is a modular-monolith federation extension for GoNZB. It authenticates
nodes, not users, and keeps local GoNZB accounts, API tokens, searches, grabs,
and download history private to the home node.

Current implementation state:

- Phase 1 creates a persistent Ed25519 node identity.
- Node IDs are deterministic hashes of public keys.
- Signed events use canonical JSON bytes for signatures and event IDs.
- PostgreSQL stores accepted event canonical bytes separately from JSONB body
  projections.
- Phase 2 maps local public-ready indexer releases into signed ReleaseCard
  events and separate federated release-card projections.
- Phase 3 adds read-only node discovery/outbox endpoints and manual pull sync
  for trusted peer URLs.
- Phase 4 adds signed inbox push sync with nonce replay protection and peer
  delivery tracking.
- Phase 5 adds local RBAC and aggregator/Newznab search integration over the
  local federated ReleaseCard cache.
- Phase 6 adds trust pool projections, M-of-N membership validation, protected
  pool event authorization, and local pool administration APIs.
- Phase 7 adds signed ResolutionManifest retrieval, verified manifest/NZB
  caching, and Newznab get integration through the local GoNZBNet aggregator
  source.
- Phase 8 adds signed HealthAttestation events, optional local health
  publishing, health-based availability aggregation, reputation deltas, and
  search ranking that uses health-adjusted scores.

Maintained pages:

- [Phase 1 Identity And Events](./phase-1-identity-and-events.md)
- [Phase 2 ReleaseCard Publishing](./phase-2-releasecard-publishing.md)
- [Phase 3 Manual Pull Sync](./phase-3-manual-pull-sync.md)
- [Phase 4 Inbox Push Sync](./phase-4-inbox-push-sync.md)
- [Phase 5 RBAC And Aggregator Integration](./phase-5-rbac-aggregator.md)
- [Phase 6 Trust Pools](./phase-6-trust-pools.md)
- [Phase 7 Resolution Manifests](./phase-7-resolution-manifests.md)
- [Phase 8 Health Attestations](./phase-8-health-attestations.md)
