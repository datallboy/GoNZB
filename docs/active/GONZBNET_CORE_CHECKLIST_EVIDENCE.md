# GoNZBNet Core Checklist Evidence

This audit maps the original GoNZBNet final implementation checklist to current
code and tests. It is not a new implementation phase; it is traceability for the
core v1 work already represented by the phase docs.

## Proven By Current Code

- `modules.gonzbnet.enabled`, `gonzbnet.http_enabled`, and
  `GONZBNET_ENABLED` gate startup/routes. Evidence:
  `internal/infra/config/config.go`, `internal/infra/config/config_test.go`,
  and `internal/api/router_test.go`.
- Persistent Ed25519 identity, deterministic node IDs, signed events,
  deterministic event IDs, body hashes, and tamper rejection are covered by
  `internal/gonzbnet/identity` and `internal/gonzbnet/events` tests.
- Accepted events store canonical signed JSON separately from JSONB body
  projections in `federation_events.canonical_event_json` and `body_json`.
  Rejected events keep `raw_event_json`.
- Local indexer/cache records publish signed `ReleaseCard` events and project
  into `federated_release_cards`; deterministic IDs are covered by
  `internal/gonzbnet/releasecard` and `internal/gonzbnet/publisher` tests.
- Manual peer pull sync fetches profiles/outboxes, verifies remote events,
  rejects invalid signatures, rejects non-member protected-pool events, records
  rejected events, and projects accepted cards. Evidence:
  `internal/gonzbnet/sync/service.go` and `service_test.go`.
- Inbox push sync, signed node request auth, nonce replay rejection, and peer
  delivery tracking are covered by `internal/gonzbnet/requestauth`,
  `internal/gonzbnet/sync`, and `internal/api/controllers/gonzbnet.go`.
- Trust pools, protected event authorization, member revocation, and
  role/capability checks are covered by `internal/gonzbnet/pools`,
  `internal/store/pgindex/federation_pool_store.go`, and related tests.
- Local RBAC controls GoNZBNet search/get/manifest resolution. Evidence:
  `internal/aggregator/manager_test.go`,
  `internal/aggregator/sources/gonzbnet/source_test.go`, and
  `internal/store/pgindex/federation_releasecard_store.go`.
- Newznab search uses the local federated cache through the local aggregator
  source; remote peers are not live-broadcast user search queries.
- Newznab get resolves missing manifests through trusted manifest sources,
  verifies the signed `ResolutionManifest`, validates `manifest_id`, caches the
  manifest, generates an NZB, and returns it locally. Evidence:
  `internal/gonzbnet/manifestresolver` and `internal/gonzbnet/manifest` tests.
- Federation request/event paths authenticate nodes, not users. Config
  validation rejects `gonzbnet.send_user_context=true`, and manifest resolver
  tests verify remote manifest fetches do not include local user context.
- Health attestations update availability/scoring through
  `internal/gonzbnet/health`, `internal/gonzbnet/publisher`, and
  `internal/store/pgindex/federation_health_store.go`.
- Tombstones project into release hiding/rejection and manifest/NZB cache
  invalidation through `internal/gonzbnet/moderation` and
  `internal/store/pgindex/federation_tombstone_store.go`.
- Admin diagnostics expose peers, accepted/rejected events, peer deliveries,
  validation tasks, pools, release sources, manifest sources, health, and
  reputation through `internal/api/controllers/gonzbnet_admin.go` and
  `internal/store/pgindex/federation_diagnostics_store.go`.
- Test evidence exists for tampering, replay, non-member event rejection, RBAC
  denial, remote manifest fetch, malformed manifests, oversize manifest
  responses, future/stale event rejection, unknown event type rejection, and
  same-author sequence conflicts.

## Boundaries

- The standalone `gonzbnet-relay` process remains deferred. Phase 11 implements
  relay-ready modular-monolith controls because v1 is intentionally not split
  into microservices.
- Automatic creation of replacement `CoverageAssignment` events is implemented
  for stale article range claims when automatic coverage mode is enabled.
- Time-window assignment execution in the scrape loop remains future work. Range
  assignments are implemented and consumed.
