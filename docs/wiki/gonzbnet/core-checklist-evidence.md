# Core Checklist Evidence

This page maps the original GoNZBNet final implementation checklist to current
code and tests. It is not a new implementation phase; it is traceability for the
core v1 work already represented by the phase pages.

## Evidence

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
  rejected events, and projects accepted cards.
- Inbox push sync, signed node request auth, nonce replay rejection, and peer
  delivery tracking are covered by `internal/gonzbnet/requestauth`,
  `internal/gonzbnet/sync`, and `internal/api/controllers/gonzbnet.go`.
- Trust pools, protected event authorization, member revocation, and
  role/capability checks are covered by `internal/gonzbnet/pools`,
  `internal/store/pgindex/federation_pool_store.go`, and related tests.
- Local RBAC controls GoNZBNet search/get/manifest resolution through the local
  aggregator source and manager tests.
- Newznab search uses the local federated cache; remote peers are not
  live-broadcast user search queries.
- `gonzbnet.live_query_enabled=true` is rejected during config validation, and
  public node profiles do not advertise live-query support.
- Newznab get resolves missing manifests through trusted manifest sources,
  verifies the signed `ResolutionManifest`, validates `manifest_id`, caches the
  manifest, generates an NZB, and returns it locally.
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

- The core implementation is not yet complete. Pool-scoped get authorization,
  receive-side body validation before accepted storage, protected outbox
  visibility, local manifest building, and the specified three-node end-to-end
  harness remain open. See [Implementation Status](./implementation-status.md).
- The standalone `gonzbnet-relay` process remains deferred. Phase 11 implements
  relay-ready modular-monolith controls because v1 is intentionally not split
  into microservices.
- Automatic creation of replacement `CoverageAssignment` events is implemented
  for stale article range and time-window claims when automatic coverage mode is
  enabled.
- Range assignments are consumed directly by the scrape loop. Time-window
  assignments are resolved to article ranges locally, claimed with
  `TimeWindowClaim`, and completed with range outcomes.
