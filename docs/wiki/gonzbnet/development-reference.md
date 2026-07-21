# Development Reference

## Source Layout

| Path | Purpose |
| --- | --- |
| `internal/gonzbnet/identity` | Node keys and deterministic identity |
| `internal/gonzbnet/canonical` | RFC 8785 canonicalization |
| `internal/gonzbnet/events` | Signed event envelopes and verification |
| `internal/gonzbnet/eventbody` | Typed inbound event validation |
| `internal/gonzbnet/requestauth` | Signed HTTP request authentication |
| `internal/gonzbnet/transportpolicy` | Peer URL and HTTPS policy |
| `internal/gonzbnet/pools` | Pool governance and event definitions |
| `internal/gonzbnet/admission` | Discovery, invitations, and admission client flow |
| `internal/gonzbnet/sync` | Pull, push, gossip, receive, and delivery |
| `internal/gonzbnet/publisher` | Release, manifest, validation, and health publication |
| `internal/gonzbnet/manifestresolver` | Manifest source resolution and verified caching |
| `internal/gonzbnet/coverage` | Scanner coverage model and scheduler |
| `internal/gonzbnet/reassigner` | Stale claim recovery |
| `internal/gonzbnet/metrics` | Process-local protocol counters |
| `internal/store/pgindex/federation_*` | PostgreSQL event and projection stores |
| `internal/runtime/wiring/gonzbnet_*` | Runtime lifecycle and scanner integration |
| `internal/api/controllers/gonzbnet*` | Federation and local-admin HTTP handlers |
| `ui/src/modules/admin/AdminGoNZBNetPage.tsx` | Admin WebUI |

Configuration is defined in `internal/infra/config/config.go`, runtime settings
in `internal/app/settings_types.go`, routes in `internal/api/router.go`, and CLI
commands in `internal/runtime/commands/gonzbnet.go` plus `cmd/gonzb/main.go`.

## Database Model

Migrations beginning with `002_gonzbnet_` introduce the event log and typed
projections. Migration filenames retain their historical sequence because they
are an applied database contract; they are not the current documentation model.

The durable model has four layers:

1. Signed evidence: accepted/rejected event envelopes and author-chain issues.
2. Governance: pools, policy, membership, role access, admissions, and peers.
3. Content projections: releases, sources, manifests, validation, health,
   trust, moderation, scan output, and coverage.
4. Operations: delivery cursors, pending/repair work, metrics inputs, and admin
   diagnostics.

Inbound accepted append and required typed projection must use the federation
transaction helper. Do not add a receive path that appends first and projects
later. Pending projections exist only for legacy repair.

## Adding Or Changing An Event

A protocol event change normally requires all of the following:

1. Define or update the typed body and canonical stable-ID rules.
2. Validate required fields, author/body relationships, timestamps, pool scope,
   private-field exclusions, and forward-compatibility policy.
3. Register the required capability and pool policy behavior.
4. Add transactional projection and idempotent duplicate handling.
5. Apply outbox, push, pull, gossip, and direct-read authorization.
6. Expose diagnostics only when operators need them.
7. Test canonicalization, signature verification, rejection cases, projection
   rollback, multi-pool isolation, and delivery eligibility.
8. Update this wiki to describe final behavior, not the implementation phase.

Use the raw canonical JSON boundary for signed input. Do not decode into a Go
map before duplicate-key validation. Never use a shared cache as a substitute
for pool authorization.

## Extension Boundaries

- Keep GoNZBNet inside the modular monolith unless a separate service is
  explicitly designed and approved.
- Access indexer data through focused store interfaces; do not make downloader,
  aggregator, and indexer deployment shapes depend on one another implicitly.
- Capabilities describe real enabled behavior. Do not advertise placeholder
  functionality.
- Pool IDs and source provenance remain explicit in queries and unique keys.
- Protocol-v1 protected events remain single-pool.
- Local auth and federation node auth remain separate.
- No search, grab, API-key, session, or download-history data may cross the
  federation boundary.

## Testing

Run focused unit and controller tests while developing:

```sh
go test ./internal/gonzbnet/... ./internal/api/controllers ./internal/runtime/wiring
```

PostgreSQL integration tests use an explicitly supplied disposable DSN:

```sh
GONZB_TEST_PG_DSN='postgres://gonzb:gonzb@127.0.0.1:55432/gonzbnet_test?sslmode=disable' \
  go test ./internal/store/pgindex -run Federation -count=1
```

The four-node test covers behavior that unit tests cannot prove: independent
identities and databases, admission quorum, pool isolation, signed transport,
restart persistence, revocation, release search, remote manifest resolution,
cache reuse, local-secret non-disclosure, and real TCP NNTP client I/O. See
[Four-Node E2E Testing](./e2e-testing.md).

Test artifacts belong under `.e2e/` or disposable Docker volumes. Do not write
generated keys, databases, blobs, NZBs, tokens, or logs into tracked fixtures.

## Documentation Policy

This wiki describes the current system and its operator/developer contract.
Completed plans and implementation evidence belong in `docs/archive/` only
when they retain decision value. The original implementation specification is
reference material and does not override current code or this wiki.
