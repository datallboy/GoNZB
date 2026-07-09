# GoNZBNet Phase 1 Implementation Plan

Status: in progress

Scope:

- Persistent Ed25519 node identity stored under `gonzbnet.keys_dir`.
- Deterministic `node_...` ID derived from SHA-256 of the raw public key.
- Canonical JSON bytes for event signing and hashing.
- Signed GoNZBNet event envelope with deterministic `evt_...` ID.
- Basic local event verification: body hash, event ID, author key, signature.
- PostgreSQL tables for federation nodes, accepted events, and rejected raw events.
- Focused unit tests for identity persistence and tamper detection.

Out of scope for Phase 1:

- Peer sync, inbox/outbox APIs, trust pools, release-card projection, manifests,
  aggregator source registration, health checks, relay mode, and UI.

Existing integration points:

- Config lives in `internal/infra/config/config.go` with `modules.gonzbnet.enabled`
  disabled by default.
- PostgreSQL migration is owned by `internal/store/pgindex/migrations`.
- Future aggregator integration should register a GoNZBNet source through
  `internal/aggregator`'s `catalogSource` contract.
- Local user auth remains in `internal/auth`; Phase 1 does not add cross-node
  user authentication.
