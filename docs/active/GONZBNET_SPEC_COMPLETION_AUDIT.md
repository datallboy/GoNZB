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

### Critical correctness and privacy

No open item remains in this category from the current audit. Public discovery
is limited to well-known metadata, node profile, and capabilities.

### Missing contribution behavior

1. Build and cache ResolutionManifests from local indexer/scan data when
   `manifest_builder_enabled` is active. At present manifests only enter the
   cache through remote resolution, so a network has no complete local manifest
   origin path.
2. Execute validator tiers against the local NNTP provider. The current
   validator emits structural `unverified` attestations and does not perform the
   spec's article/segment existence checks.
3. Publish scanner capacity/heartbeat/group observations and periodic coverage
   checkpoints from scanner execution. The schemas and projections exist, but
   the scanner loop currently emits claims and terminal outcomes only.
4. Enforce manifest-cache byte/TTL/serving settings. These settings are typed
   and displayed but do not currently control cache retention or serving.

### Protocol and security conformance

5. Validate per-author chain continuity (`previous_event_id` and monotonic
    sequence), not only same-sequence conflicts.
6. Make capabilities truthful. The node currently advertises gzip/zstd and
    event types that the normal receive path does not implement.
7. Reconcile wire-body differences where current typed objects diverge from
    the specification, especially `ManifestAvailability` and coverage events.
8. Complete config semantics and aliases for controls that affect behavior,
    including manifest-specific rate limits, remote get timeout, configurable
    route base path, and currently display-only addendum limits.
9. Make accepted-event storage and projection atomic, or retain explicit
    pending/quarantine state until projection succeeds.

### Verification and operations

10. Add the specified PostgreSQL-backed three-node end-to-end harness covering
    publish, pull/push, authorized search/get, manifest fetch, malicious-node
    rejection, revocation, and tombstone propagation.
11. Add direct PostgreSQL integration tests for event append/rejection,
    projections, pool authorization, and migration behavior. Current GoNZBNet
    tests rely primarily on fakes.
12. Add the named GoNZBNet counters/histograms and fill remaining structured-log
    events from the observability section.

## Documentation Drift

- `GONZBNET_PHASE1_IMPLEMENTATION_PLAN.md` still says `Status: in progress`
  even though Phase 1 is implemented and merged.
- The prior final-checklist audit overstated completion by treating phase pages
  and unit tests as proof of end-to-end conformance.
- Phase B accurately documents structural-only validation, which is evidence of
  remaining work rather than completed validation-only operation.
- Phase 7 accurately documents that local manifest building was out of scope;
  the optional manifest-builder module still needs that behavior.

## Execution Order

1. Event-chain continuity and transactional receive projection.
2. Local manifest building and validator/scanner contribution behavior.
3. Config enforcement and truthful protocol advertisement.
4. Integration/E2E tests and observability.
