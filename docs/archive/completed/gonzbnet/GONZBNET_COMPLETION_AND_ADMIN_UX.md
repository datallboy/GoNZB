# GoNZBNet Completion and Admin UX

Status: complete

## Objective

Finish the remaining GoNZBNet implementation-spec gaps while making routine pool administration understandable without exposing protocol and governance details by default.

## Scope

### Admin experience

- Make `Overview` the default GoNZBNet admin view.
- Keep routine workflows in Overview: node status, pool membership, create pool, discover/join pool, pending admissions, peer health, and recent sync state.
- Put raw protocol state, capability data, coverage, validation, manifests, event logs, and detailed diagnostics in an `Advanced` view.
- Put key export/rotation and destructive governance actions in a red `Destructive stuff` section at the bottom of Advanced.
- Require the documented confirmation phrase for destructive actions. Key export also continues to require an encryption password.
- Preserve backend RBAC boundaries; the UI is organization, not authorization.

### Operator CLI

Add local operator commands under `gonzb gonzbnet`:

- `status`: identity, enabled modules, visibility, and pool/peer counts.
- `pools`: configured trust pools and local membership.
- `peers`: known peer endpoints and sync state.
- `sync`: run one pull/push synchronization pass without starting the HTTP server.

Commands use the configured local database and node identity. They do not introduce cross-node user authentication.

### Transactional event acceptance

- Add a PostgreSQL transaction boundary for accepted inbound event append plus its derived projection.
- Cover pool-control, ReleaseCard, validation, coverage, health/trust, manifest-availability, and tombstone projections used by inbound synchronization.
- Roll back the append when projection fails so an event cannot appear accepted without its required projection.
- Keep the pending-projection retry mechanism for events written by older versions and for explicit repair.

### Protocol metrics

- Add a dependency-free, process-local GoNZBNet metric registry.
- Expose Prometheus text through an authenticated admin metrics endpoint and show a compact metrics summary in Advanced.
- Instrument received, accepted, rejected, projected, peer failure/duration, manifest resolution, health attestation, and active tombstone measurements.
- Do not attach usernames, API keys, searches, grabs, or download data to metric labels.

### E2E coverage

- Extend the four-node harness with a two-admin quorum admission scenario proving one approval remains pending and the threshold approval admits the node.
- Add a deterministic TCP NNTP fixture and a repeatable harness scenario proving scanner/validator behavior against NNTP rather than database-only fixture insertion.
- Keep all harness databases isolated from the existing `gonzb` database in the `gonzb-postgres` container.

## Implementation order

1. Admin Overview/Advanced split and destructive section.
2. Local operator CLI.
3. Transactional inbound append/projection API and integration tests.
4. Protocol metrics and endpoint/UI summary.
5. Multi-admin quorum and real-NNTP E2E scenarios.
6. Update `docs/wiki/gonzbnet`, run Go/UI/PostgreSQL tests, and archive this document when complete.

## Constraints

- Modular monolith only; no new service boundary.
- Federation authenticates nodes, never users.
- Search remains local-cache-first.
- No automatic global discovery network or blockchain.
- Existing Newznab behavior and local RBAC/API-key behavior remain unchanged.
- Existing four-node harness processes and databases are reused only by the explicit harness command.

## Validation

- `go test ./...`
- UI typecheck/build and relevant UI tests
- PostgreSQL federation integration tests against `gonzb-postgres`
- four-node admission, quorum, release-sharing, and NNTP fixture smoke scenarios
