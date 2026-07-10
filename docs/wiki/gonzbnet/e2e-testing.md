# Three-Node Federation Testing

This harness runs three independent GoNZB server processes against three
separate PostgreSQL databases. Each node also has its own SQLite settings DB,
blob cache, logs, and persistent Ed25519 key directory.

## Prerequisites

- Go 1.25 or the version declared by `go.mod`
- Docker with Compose
- `curl` and `jq`
- free ports `18081`, `18082`, `18083`, and `55432`

## Lifecycle

```sh
make gonzbnet-e2e-start
make gonzbnet-e2e-bootstrap
make gonzbnet-e2e-verify
make gonzbnet-e2e-status
```

The nodes are available at:

| Node | URL | Intended role |
|---|---|---|
| A | `http://127.0.0.1:18081` | scanner, coverage, manifest builder |
| B | `http://127.0.0.1:18082` | validator and health checker |
| C | `http://127.0.0.1:18083` | consumer/cache and relay mode |

The local bootstrap password defaults to `gonzb-e2e-local`. Override it with
`GONZBNET_E2E_PASSWORD`. This credential is local to each node and is never
used for federation.

Use `./scripts/gonzbnet_e2e.sh logs` for combined logs. Stop while preserving
state with `make gonzbnet-e2e-stop`; remove all test identities and databases
with `make gonzbnet-e2e-reset`.

## Basic Federation Checks

The smoke command verifies that all three nodes are healthy, advertise
`gonzbnet/1.0`, expose capabilities, and have distinct deterministic node IDs.
Manual peers are configured in each node YAML, and pull/push workers run every
minute.

The federation smoke command creates a signed pool tombstone on Node A, pushes
it to B and C, checks accepted append and projection in every PostgreSQL
database, and verifies duplicate delivery remains exactly once. It also checks
that protected outbox reads require node signatures and that Node A's local
admin session is not valid on Node B.

Inspect public profiles directly:

```sh
curl -s http://127.0.0.1:18081/.well-known/gonzbnet | jq
curl -s http://127.0.0.1:18082/gonzbnet/v1/node | jq
curl -s http://127.0.0.1:18083/gonzbnet/v1/caps | jq
```

Open `/admin/gonzbnet` on each node for peer diagnostics, accepted/rejected
events, deliveries, capabilities, coverage, validation tasks, manifests,
health, reputation, and pool controls.

## Trust Pool Test

The `configure-pool` command creates the same protected `pool.e2e` projection
on all three nodes, adds each node with role-appropriate capabilities, grants
the local admin role search/get/manifest access, and triggers a pull. To test
the UI/API manually instead:

1. On Node A, create `pool.e2e` with `accept_mode=pool_member`, threshold `1`,
   and the default accepted event types.
2. Read Node B and Node C IDs from their node-profile pages.
3. Add all three node IDs as active members. Grant scanner/indexer/coverage and
   manifest-builder capabilities to A, validator/health-checker/cache to B,
   and consumer/cache/relay capabilities to C.
4. Repeat or synchronize the pool projection on the other nodes.
5. Trigger pull/push from the peer-management panel and verify accepted events.
6. Revoke B, trigger sync again, and verify new B events are rejected while
   prior accepted events remain append-only.

This validates node authentication and pool membership without sharing local
users, sessions, API keys, searches, grabs, or download history.

## ReleaseCard And Manifest Test

Node A needs a local release fixture with complete segment Message-IDs. The
most representative route is to configure a test NNTP account and newsgroup in
Node A's settings, enable the usenet-indexer module, then run:

```sh
.e2e/gonzbnet/gonzb indexer pipeline --once --config test/e2e/gonzbnet/node-a.yaml
.e2e/gonzbnet/gonzb indexer recover-yenc --once --config test/e2e/gonzbnet/node-a.yaml
.e2e/gonzbnet/gonzb indexer inspect --once --config test/e2e/gonzbnet/node-a.yaml
.e2e/gonzbnet/gonzb indexer release generate-nzb --once --config test/e2e/gonzbnet/node-a.yaml
```

GoNZBNet has no separate CLI process or microservice. Its scanner, publisher,
validator, health, cache, consumer, and relay capabilities are background
modules of `gonzb serve`, controlled by each node YAML and the local admin API.
The `gonzb indexer` commands above only create and process the local indexer
fixture that Node A projects into federation.

After a public-ready release forms, wait for or trigger ReleaseCard publication
and pull sync. Verify:

- Node B/C search the local federated cache; no live search reaches A.
- The result's download URL points to the node serving the local Newznab API.
- A grab on B/C resolves a missing signed manifest from A.
- The manifest ID verifies, the manifest/NZB is cached locally, and repeat grabs
  use the local cache.
- Peer logs and request bodies contain node identity only, never local user
  credentials or histories.

## Validator And Health Test

Configure an NNTP provider on Node B to exercise real segment availability.
Without one, B intentionally publishes structural `unverified` attestations.
With one, queued manifests use the scoped NNTP body-prefix checker and publish
`available`, `partial`, or `missing` attestations. Check the validation task,
health, and reputation diagnostics on all nodes after sync.

## Coverage And Scanner Test

Enable the usenet-indexer module and a test group on Node A. A completed claimed
range should produce signed scanner capacity, heartbeat, group observation,
checkpoint, and completion events. Use the coverage dashboard to create an
assignment, inspect claims, complete/fail ranges, materialize stale penalties,
and exercise stale reassignment.

## Moderation And Tombstone Test

Create a local or pool-scoped tombstone for a federated release on Node A, then
sync all nodes. Verify the release disappears from authorized Newznab search,
cached manifests are not served when policy rejects them, and the signed
tombstone remains visible in diagnostics. Repeat with a threshold greater than
one to verify a single moderation vote remains pending.

## Push, Pull, Gossip, And Relay

- Use peer controls to force one pull and one push in each direction.
- Enable `websocket_gossip_enabled` in all node configs to test gossip; restart
  and confirm duplicate events are processed once and TTL stops forwarding.
- Node C runs relay mode inside the modular monolith. Confirm its public
  federation routes work while its local aggregator reads projected data.
- Disable peer exchange and confirm no discovered peer is automatically added.

## Database Inspection

```sh
docker compose -p gonzbnet-e2e -f docker-compose.gonzbnet-e2e.yml exec postgres \
  psql -U gonzb -d gonzbnet_b -c \
  "select event_type, validation_status, count(*) from federation_events group by 1,2 order by 1,2"

docker compose -p gonzbnet-e2e -f docker-compose.gonzbnet-e2e.yml exec postgres \
  psql -U gonzb -d gonzbnet_c -c \
  "select release_id, title, pool_id from federated_release_cards order by updated_at desc limit 20"
```

Use the same pattern for `federation_peers`, `pool_members`,
`resolution_manifests`, `federated_manifest_sources`, `health_attestations`,
`coverage_claims`, `tombstones`, and `federation_pending_projections`.

## Automated Go Integration Tests

The PostgreSQL integration tests use `GONZB_TEST_PG_DSN` and are skipped by
default. Point them at a disposable database, never a development database:

```sh
GONZB_TEST_PG_DSN='postgres://gonzb:gonzb@127.0.0.1:55432/gonzbnet_test?sslmode=disable' \
  go test ./internal/store/pgindex -run Federation -count=1
```

The `gonzbnet_test` database is separate from the three running node databases,
so integration-test cleanup cannot disrupt the harness nodes.
