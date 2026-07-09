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
- Phase 9 adds signed Tombstone moderation events, local-only blocklisting,
  pool-threshold tombstone activation, search hiding, and manifest/NZB cache
  invalidation.
- Phase 10 adds optional signed WebSocket gossip, TTL/fanout limits, delivery
  dedupe, peer exchange controls, and connection backoff.
- Phase 11 adds relay-ready controls inside the modular monolith: public
  federation HTTP isolation and relay capability advertising.
- Addendum Phase A adds capability advertisement, durable node capability
  snapshots, pool-approved contribution grants, and module switch no-op
  behavior.
- Addendum Phase B adds validator-only signed attestations, validation task
  queueing, checksum-attestation schema support, and validation score
  projection.
- Addendum Phase C adds scanner scan-output publishing, optional manifest
  availability events, and search isolation when local index projection is
  disabled.
- Addendum Phase D adds signed coverage coordination events, manual assignment
  APIs, active/stale claim projection, and coverage dashboard reads.
- Addendum Phase E adds local dedup-aware coverage work suggestions, gap and
  duplicate detection, and coverage score computation.
- Addendum Phase F adds deterministic weighted rendezvous scheduler helpers and
  read-only coverage plans for stale-claim failover review.
- Addendum Phase G adds ScannerHeartbeat, node capability admin reads, group
  catalog reads, validation-gap reads, and stale-claim penalty materialization.
- Addendum Phase H adds the local GoNZBNet admin WebUI for capability,
  coverage, scheduler, validation-gap, and manual signed coverage workflows.

Maintained pages:

- [Phase 1 Identity And Events](./phase-1-identity-and-events.md)
- [Phase 2 ReleaseCard Publishing](./phase-2-releasecard-publishing.md)
- [Phase 3 Manual Pull Sync](./phase-3-manual-pull-sync.md)
- [Phase 4 Inbox Push Sync](./phase-4-inbox-push-sync.md)
- [Phase 5 RBAC And Aggregator Integration](./phase-5-rbac-aggregator.md)
- [Phase 6 Trust Pools](./phase-6-trust-pools.md)
- [Phase 7 Resolution Manifests](./phase-7-resolution-manifests.md)
- [Phase 8 Health Attestations](./phase-8-health-attestations.md)
- [Phase 9 Moderation And Tombstones](./phase-9-moderation-tombstones.md)
- [Phase 10 WebSocket Gossip](./phase-10-websocket-gossip.md)
- [Phase 11 Relay Mode Controls](./phase-11-relay-mode.md)
- [Phase A Capability Registry](./phase-a-capabilities.md)
- [Phase B Validation-Only Contribution](./phase-b-validation.md)
- [Phase C Scan-Without-Index Contribution](./phase-c-scan-output.md)
- [Phase D Coverage Events And Manual Assignments](./phase-d-coverage.md)
- [Phase E Dedup-Aware Local Scheduler](./phase-e-dedup-scheduler.md)
- [Phase F Automated Coverage Improvements](./phase-f-coverage-scheduler.md)
- [Phase G Addendum Checklist Gaps](./phase-g-addendum-checklist-gaps.md)
- [Phase H Admin UI](./phase-h-admin-ui.md)
