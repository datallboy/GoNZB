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
- Addendum Phase F adds deterministic weighted rendezvous scheduler helpers,
  read-only coverage plans, and later automatic stale range reassignment.
- Addendum Phase G adds ScannerHeartbeat, node capability admin reads, group
  catalog reads, validation-gap reads, and stale-claim penalty materialization.
- Addendum Phase H adds the local GoNZBNet admin WebUI for capability,
  coverage, scheduler, validation-gap, and manual signed coverage workflows.
- Addendum Phase I adds local admin diagnostics for peers, accepted/rejected
  events, peer deliveries, and validation task state.
- Addendum Phase J adds local trust-pool, pool-member, and tombstone moderation
  workflows to the GoNZBNet admin WebUI.
- Addendum Phase K adds local peer management, peer enable/disable, and manual
  pull/push/gossip sync triggers.
- Addendum Phase L aligns local RBAC with the remaining core and optional-module
  GoNZBNet permission names.
- Addendum Phase M adds local admin diagnostics for release sources, manifest
  sources, health attestations, and node reputation/trust visibility.
- Addendum Phase N adds local node profile visibility and redacted GoNZBNet
  configuration validation to the admin API/UI.
- Capability cleanup aligns NodeProfile contribution booleans with optional
  module configuration.
- Addendum Phase O adds a local admin action to resolve federated manifests
  through the existing signed manifest resolver path.
- Addendum Phase P adds local peer removal to the peer-management admin API/UI.
- Admin requirements cleanup tracks spec admin requirements that are implemented
  outside named phases, including local node block/unblock controls.
- Admin requirements cleanup adds a local signed `PoolJoinRequest` action for
  requesting trust-pool membership without mutating membership directly.
- Admin requirements cleanup adds a local signed `PoolMemberApproved` action for
  promoting reviewed join requests into active pool membership.
- Admin requirements cleanup adds a local signed `PoolMemberRevoked` action for
  auditable pool membership removal.
- Admin requirements cleanup adds a local pool-control event view for accepted
  join requests, approvals, and revocations.
- Admin requirements cleanup adds a local federated score recomputation action.
- Admin requirements cleanup adds local role-level federation pool access
  management.
- Security cleanup adds optional encrypted node-key storage when
  `gonzbnet.key_password` is configured.
- Security cleanup adds explicit encrypted node-key export guarded by
  `gonzbnet.admin.keys`.
- Security cleanup adds explicit local node-key rotation guarded by
  `gonzbnet.admin.keys`.
- Security cleanup hardens public federation transport with GoNZBNet-specific
  body limits, inbox/manifest rate limiting, and stable machine-readable error
  codes.
- Security cleanup enforces HTTPS/WSS peer transport by default, with
  loopback-only insecure HTTP for explicit local development.
- Admin cleanup requires active pool-admin membership before the local node can
  publish pool-scoped Tombstone moderation votes.
- Config cleanup treats GoNZBNet as a first-class enabled module and requires
  PostgreSQL when `modules.gonzbnet.enabled` is true.
- Config cleanup maps the spec shorthand `GONZBNET_ENABLED` to the existing
  `modules.gonzbnet.enabled` module gate.
- Config cleanup adds direct route-gate coverage for
  `modules.gonzbnet.enabled` and `gonzbnet.http_enabled`.
- Public endpoint cleanup adds the spec-listed `/events/batch` inbox alias,
  `/pools/:pool_id/checkpoint`, `/pools/:pool_id/members`, and `/peers`
  discovery routes.
- Pool checkpoint cleanup adds `PoolCheckpoint` validation and projection over
  the accepted append-only event log.
- Trust attestation cleanup adds signed `TrustAttestation` reputation deltas
  with local bounded scoring.
- Public coverage read cleanup adds signed node-to-node coverage group, plan,
  work, and node capability read endpoints.
- Public coverage write cleanup adds signed node-to-node coverage claim and
  checkpoint convenience endpoints.
- Validation cleanup adds a signed node-to-node validation request endpoint for
  locally cached manifests.
- Scanner coordination cleanup wires the existing usenet-indexer scrape loop to
  signed range claims/outcomes and provider-scope-compatible trusted range
  suppression.
- Assignment-driven scanner cleanup lets the existing scrape loop consume local
  range `CoverageAssignment` suggestions without advancing scrape cursors.
- Time-window scanner assignment cleanup lets the scrape loop resolve local
  time-window `CoverageAssignment` suggestions to article ranges and claim them
  with signed `TimeWindowClaim` events.
- Stale-claim reassignment cleanup creates signed replacement range and
  time-window assignments in automatic coverage mode.
- Config addendum alignment adds typed scanner, coverage, validation, and
  manifest-cache settings with direct `GONZBNET_*` aliases.
- Security cleanup adds temporary in-memory throttling after repeated
  federation rate-limit violations.
- Profile cleanup adds NodeProfile module status, scanner capacity, validator
  capacity, and provider-scope advertisement.
- Core checklist evidence maps the original final implementation checklist to
  current code and test coverage.
- Final checklist audit documents implemented addendum requirements and current
  deferred standalone relay boundaries.
- Security cleanup rejects remote signed events with future `created_at` /
  `not_before` windows, expired `expires_at` values, or event ages beyond
  `gonzbnet.max_event_age_hours`.
- Security cleanup rejects unknown signed event types before accepted-event
  storage unless explicit compatibility support is added.
- Security cleanup dead-letters same-author sequence conflicts before accepted
  event projection.
- Security cleanup limits fetched peer manifest responses with
  `gonzbnet.max_manifest_bytes`.
- Security cleanup rejects malformed ResolutionManifest segment Message-IDs
  before caching or NZB generation.
- Test cleanup adds direct coverage for non-member event rejection and local
  GoNZBNet RBAC denial paths.
- Live-query privacy cleanup rejects `gonzbnet.live_query_enabled=true` and
  keeps public profiles from advertising unsupported live user search.
- Pool RBAC cleanup authorizes federated gets before shared blob-cache reads and
  prevents pool-less aggregator cache rows from bypassing search isolation.
- Receive validation cleanup rejects malformed typed bodies before accepted
  storage and signs pool-member capability grants as part of approval events.

Maintained pages:

- [Implementation Status](./implementation-status.md)
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
- [Phase I Admin Diagnostics](./phase-i-admin-diagnostics.md)
- [Phase J Pool And Moderation UI](./phase-j-pool-moderation-ui.md)
- [Phase K Peer Management](./phase-k-peer-management.md)
- [Phase L RBAC Alignment](./phase-l-rbac-alignment.md)
- [Admin Pool Tombstone Authorization](./admin-pool-tombstone-authorization.md)
- [Phase M Source And Health Diagnostics](./phase-m-source-health-diagnostics.md)
- [Phase N Node Profile And Config Admin](./phase-n-node-config-admin.md)
- [Phase O Force Resolve Manifest](./phase-o-force-resolve-manifest.md)
- [Phase P Remove Peer](./phase-p-remove-peer.md)
- [Admin Requirements Cleanup](./admin-requirements-cleanup.md)
- [Admin: Pool Join Request](./admin-pool-join-request.md)
- [Admin: Pool Member Approval](./admin-pool-member-approval.md)
- [Admin: Pool Member Revocation](./admin-pool-member-revocation.md)
- [Admin: Pool Control Events](./admin-pool-control-events.md)
- [Admin: Recompute Scores](./admin-recompute-scores.md)
- [Admin: Role Pool Access](./admin-role-pool-access.md)
- [Config Validation Cleanup](./config-validation-cleanup.md)
- [Config Enable Alias](./config-enable-alias.md)
- [Config Addendum Alignment](./config-addendum-alignment.md)
- [Config Route Gate Coverage](./config-route-gate-coverage.md)
- [Public Endpoint Alignment](./public-endpoint-alignment.md)
- [Pool Checkpoints](./pool-checkpoints.md)
- [Trust Attestations](./trust-attestations.md)
- [Public Coverage Read Endpoints](./public-coverage-read-endpoints.md)
- [Public Coverage Write Endpoints](./public-coverage-write-endpoints.md)
- [Validation Request Endpoint](./validation-request-endpoint.md)
- [Scanner Coordination Cleanup](./scanner-coordination-cleanup.md)
- [Assignment-Driven Scanner Cleanup](./assignment-driven-scanner-cleanup.md)
- [Time-Window Scanner Assignments](./time-window-scanner-assignments.md)
- [Stale Claim Reassignment](./stale-claim-reassignment.md)
- [Capability Profile Alignment](./capability-profile-alignment.md)
- [Profile Capacity Alignment](./profile-capacity-alignment.md)
- [Core Checklist Evidence](./core-checklist-evidence.md)
- [Final Checklist Audit](./final-checklist-audit.md)
- [Test Coverage Cleanup](./test-coverage-cleanup.md)
- [Security: Node Key Encryption](./security-key-encryption.md)
- [Security: Key Export](./security-key-export.md)
- [Security: Node Key Rotation](./security-key-rotation.md)
- [Security: Federation Transport Hardening](./security-transport-hardening.md)
- [Security: Flood Throttle](./security-flood-throttle.md)
- [Security: Peer TLS Policy](./security-peer-tls-policy.md)
- [Security: Event Time Windows](./security-event-time-windows.md)
- [Security: Event Type Compatibility](./security-event-type-compatibility.md)
- [Security: Sequence Conflicts](./security-sequence-conflicts.md)
- [Security: Manifest Response Limit](./security-manifest-response-limit.md)
- [Security: Manifest Message-IDs](./security-manifest-message-ids.md)
- [Live Query Privacy Hardening](./live-query-privacy-hardening.md)
- [Pool RBAC And Cache Isolation](./rbac-cache-isolation.md)
- [Receive Body Validation](./receive-body-validation.md)
