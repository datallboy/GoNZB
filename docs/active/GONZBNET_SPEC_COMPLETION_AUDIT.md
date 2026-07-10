# GoNZBNet Specification Completion Audit

Status: active

Source of truth: `docs/GoNZBNet_Codex_Implementation_Spec.md`.

This audit reconciles the implementation specification with the current code,
phase documents, and maintained wiki. It does not define new phases. Cleanup
work should be committed directly on `feature/gonzbnet` unless a remaining
spec item is large enough to require an implementation branch.

## Implemented Baseline

- Core phases 1 through 10 have working code paths for identity, signed events,
  release-card publication, pull/push transport, local RBAC search, trust pools,
  on-demand manifest resolution, health projections, tombstones, and optional
  WebSocket gossip.
- Phase 11 correctly remains relay-ready modular-monolith behavior. A standalone
  relay process is a future extension and is not a v1 completion requirement.
- Addendum phases A through E are substantially present. Optional module
  switches, capabilities, scan output, validation queues, coverage events, and
  local dedup-aware scheduling exist.
- Optional Phase F scheduling helpers and stale-claim reassignment are present,
  although Phase F is not a v1 blocker.
- Admin APIs/UI and the documented security cleanups cover most required local
  operational controls.

## Required Remaining Work

Completed during this audit:

- Pool-level `can_get` and `can_resolve_manifest` grants are now enforced before
  generic aggregator blob-cache reads.
- Shared `aggregator_release_cache` rows are no longer used as GoNZBNet search
  results because they do not retain pool identity.
- Inbox, gossip, and pull now validate typed bodies before accepted storage;
  ReleaseCard identity is recomputed and private user/context fields are
  rejected.
- PoolMemberApproved now signs and projects allowed capabilities and limits.
- Protected federation reads now require signed node requests and filter events
  to public visibility or active shared-pool membership.
- Pull now signs outbox requests and synchronizes all supported event types;
  push and gossip apply the same destination-pool visibility filter.
- Signed and hashed JSON now uses RFC 8785 canonicalization, with direct vectors
  and duplicate-key rejection before federation payload decoding.
- Per-author append transactions now enforce known chain links, track and
  resolve partial-sync gaps, and retain fork evidence with suspicious status.
- Node profiles and `/caps` now advertise only active publication paths,
  structural validation, uncompressed JCS JSON, and implemented event routes.
- `ManifestAvailability` now uses the specified source/pool availability body
  and updates only the matching manifest source projection.
- Coverage event bodies now use the addendum field names, nested plan shape,
  provider scope, and relational projection columns.

### Critical correctness and privacy

No open item remains in this category from the current audit. Public discovery
is limited to well-known metadata, node profile, and capabilities.

### Missing contribution behavior

1. Complete. When `manifest_builder_enabled` is active, the local ReleaseCard
   publisher maps complete indexed files/segments to a shared canonical
   manifest core, validates and generates the manifest/NZB, and stores them in
   `resolution_manifests`.
2. Article-availability validation is now implemented when the local NNTP
   manager is available: the validator checks each manifest segment through the
   scoped body-prefix fetch path and publishes available/partial/missing
   attestations. Structural `unverified` behavior remains the fallback when no
   NNTP checker is configured; checksum/PAR2 tiers still require their specific
   fetchers.
3. Partially complete. Claimed range completion now emits and projects signed
   provider-scoped `CoverageCheckpoint` events, and the scrape run observer now
   publishes `ScannerCapacity`, `ScannerHeartbeat`, and provider-scoped
   `GroupObservation` events from completed ranges. Richer periodic checkpoint
   counters remain to be connected to scanner runtime metrics.
4. Complete for retention and serving reads. The PostgreSQL manifest store now
   applies TTL expiry and byte-budget pruning, and excludes expired manifests
   from local manifest/NZB/event reads. Manifest serving remains restricted to
   active trusted pool members.

### Protocol and security conformance

5. Partially complete. The spec's shared federation rate limit is enforced,
    and the remote manifest fetch timeout is now typed,
    defaulted, and wired into the resolver, and the configurable federation
    route base path is used by route registration with the existing default
    fallback. Currently display-only addendum limits still need behavioral
    enforcement.
6. Pending projection state is now durable: accepted-event projection failures
    are recorded with event/type, retry attempts, error, and resolution state.
    Pull-sync startup now replays supported pending events and resolves rows
    after successful projection. Full single-transaction append/projection
    remains a future optimization, but accepted events no longer lack an
    explicit failure or retry state.

### Verification and operations

7. Add the specified PostgreSQL-backed three-node end-to-end harness covering
    publish, pull/push, authorized search/get, manifest fetch, malicious-node
    rejection, revocation, and tombstone propagation.
8. Partially complete. Direct PostgreSQL integration tests now cover migration,
    accepted/rejected event persistence, ReleaseCard and validator-capacity
    projections, chain continuity, and pending projection lifecycle.
    Coverage and article-attestation typed projection coverage are now
    included; pool-authorization integration coverage remains to be added.
9. Add the named GoNZBNet counters/histograms and fill remaining structured-log
    events from the observability section.

## Documentation Drift

- The prior final-checklist audit overstated completion by treating phase pages
  and unit tests as proof of end-to-end conformance.
- Phase B accurately documents structural-only validation, which is evidence of
  remaining work rather than completed validation-only operation.
- Phase 7 now distinguishes the original remote resolver scope from the
  completed optional local manifest-builder module.

## Execution Order

1. Transactional receive projection and wire-body conformance.
2. Local manifest building and validator/scanner contribution behavior.
3. Config enforcement and truthful protocol advertisement.
4. Integration/E2E tests and observability.
