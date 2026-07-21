# Four-Node Federation Testing

This is a disposable test fixture, not a deployment template. Every service is
bound to loopback, all checked-in credentials are test-only, and generated
keys, databases, API tokens, cookies, logs, binaries, blobs, and NZBs remain
under ignored `.e2e/` state or the disposable Docker volume. Never substitute
production credentials or production databases in these files.

This harness runs four independent GoNZB server processes against four
separate PostgreSQL databases. Each node also has its own SQLite settings DB,
blob cache, logs, and persistent Ed25519 key directory.

## Prerequisites

- Go 1.25 or the version declared by `go.mod`
- Docker with Compose
- `curl` and `jq`
- free ports `11119`, `18081`, `18082`, `18083`, `18084`, and `55432`

## Lifecycle

The definitive test starts from empty fixture state, runs every scenario, and
removes the disposable state and database volume on success or failure:

```sh
make gonzbnet-e2e-test
```

For interactive inspection, run the lifecycle in separate steps:

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
| D | `http://127.0.0.1:18084` | standalone consumer with no configured peers |

The local bootstrap password defaults to `gonzb-e2e-local`. Override it with
`GONZBNET_E2E_PASSWORD`. This credential is local to each node and is never
used for federation.

For a fresh consumer node, `aggregator.sources.gonzbnet.enabled: true` in the
bootstrap YAML is effective even before the node has saved runtime settings.
After pool approval and event synchronization, a local API key with GoNZBNet
search/get permissions can search received ReleaseCards through Newznab.
Grabbing a result fetches and verifies the trusted manifest when absent,
caches it locally, and returns a generated NZB.

Use `./scripts/gonzbnet_e2e.sh logs` for combined logs. Stop while preserving
state with `make gonzbnet-e2e-stop`; remove all test identities and databases
with `make gonzbnet-e2e-reset`.

`reset` stops only the fixture processes, removes the named E2E Compose volume,
and deletes `.e2e/gonzbnet`. It does not touch normal runtime `store/`
directories or any configured production database.

## Basic Federation Checks

The smoke command verifies that all four nodes are healthy, advertise
`gonzbnet/1.0`, expose capabilities, and have distinct deterministic node IDs.
Node D has no manual peers. It learns its first peer only through admission.

The federation smoke command creates a signed pool tombstone on Node A, pushes
it to B, C, and D, checks accepted append and projection in every PostgreSQL
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

## Admission And Pool Isolation Test

The `configure-pool` command creates `pool.e2e` on A, admits B and C through A,
then has D contact B so the request is approved by A through a non-admin relay.
D creates `pool.side` and admits C with a signed invitation. No membership row
is inserted directly. The `admission-smoke` check verifies all four P1
memberships, proves A/B did not receive P2 state, restarts every node to verify
persistent identities, and verifies a P2 revocation does not affect C's P1
membership.

1. On Node A, create `pool.e2e` with `accept_mode=pool_member`, threshold `1`,
   and the default accepted event types.
2. On B, paste A's address in **Join pool** and request access.
3. On A, approve B in **Pool admissions**. Repeat for C.
4. On D, paste B's address; approve the relayed request on A.
5. Trigger pull/push from the peer-management panel and verify accepted events.
6. Revoke B, trigger sync again, and verify new B events are rejected while
   prior accepted events remain append-only.

This validates node authentication and pool membership without sharing local
users, sessions, API keys, searches, grabs, or download history.

## Multi-Administrator Quorum Test

The `quorum-smoke` scenario creates a separate pool, admits B as a second
administrator, raises the membership threshold to two, and has C request
membership. It proves A's first signed approval remains pending and B's second
independent approval creates the final `PoolMemberApproved` event. All three
databases must project the same final event and three distinct active nodes.

```sh
./scripts/gonzbnet_e2e.sh quorum-smoke
```

## ReleaseCard And Manifest Test

`make gonzbnet-e2e-verify` includes a deterministic `release-smoke` test. It
inserts a scanner-owned release candidate on A and then uses normal background
workers and HTTP APIs to prove all of the following:

- A publishes a signed `ReleaseCard` and signed `ResolutionManifest`.
- D receives and searches the release through its local federated cache.
- D's local Newznab get requests the manifest from A without user context.
- D verifies and caches the manifest, generates the expected NZB, and serves a
  second identical grab without contacting A again.
- The local Newznab API token appears in neither federation events nor node
  logs.

Run that check directly with:

```sh
./scripts/gonzbnet_e2e.sh release-smoke
```

The deterministic release fixture tests federation independently of NNTP. The
separate `nntp-smoke` scenario starts a real TCP NNTP fixture and runs the
production `nntp.Manager` through DATE, GROUP, XOVER, and BODY operations:

```sh
./scripts/gonzbnet_e2e.sh nntp-smoke
```

Use the following path when validating the complete indexer-to-federation flow
against an external NNTP provider.

Node A needs a local release fixture with complete segment Message-IDs. The
most representative route is to configure a test NNTP account and newsgroup in
Node A's settings, enable the usenet-indexer module, then run:

```sh
.e2e/gonzbnet/gonzb indexer pipeline --once --config test/e2e/gonzbnet/node-a.yaml
.e2e/gonzbnet/gonzb indexer recover-yenc --once --config test/e2e/gonzbnet/node-a.yaml
.e2e/gonzbnet/gonzb indexer inspect --once --config test/e2e/gonzbnet/node-a.yaml
.e2e/gonzbnet/gonzb indexer release generate-nzb --once --config test/e2e/gonzbnet/node-a.yaml
```

GoNZBNet has no separate service or microservice. Its scanner, publisher,
validator, health, cache, consumer, and relay capabilities are background
modules of `gonzb serve`. Local `gonzb gonzbnet` operator commands inspect the
same database or trigger a one-shot sync; they do not run another daemon.

To test against an existing indexer database, use the GoNZBNet feature build
(`v0.8.0` plus the GoNZBNet commits), stop the plain `v0.8.0` process, and test
against a database copy first. Point Node A's `store.pg_dsn` at that copy and
keep its existing E2E key directory. Starting the feature build applies newer
database migrations, so do not continue running an older binary against the
migrated database.

After a public-ready release forms, wait for or trigger ReleaseCard publication
and pull sync. The automated smoke test covers the following behavior; repeat
it with the real release:

- Node B/C search the local federated cache; no live search reaches A.
- The result's download URL points to the node serving the local Newznab API.
- A grab on B/C resolves a missing signed manifest from A.
- The manifest ID verifies, the manifest/NZB is cached locally, and repeat grabs
  use the local cache.
- Peer logs and request bodies contain node identity only, never local user
  credentials or histories.

Receiving a ReleaseCard makes it searchable but does not fetch its manifest or
queue validation by itself. On Node B, use **Manifest resolve** on the GoNZBNet
admin page with the received `release_id`. The signed manifest is fetched from
Node A, verified, cached, converted to an NZB, and queued for Node B's validator.
The E2E publisher and validator intervals are one minute.

## Validator And Health Test

Run `nntp-smoke` for repeatable production-client coverage. Configure an
external NNTP provider on Node B to exercise provider-specific segment
availability. Without one, B intentionally publishes structural `unverified`
attestations. With one, queued manifests use the scoped NNTP body-prefix checker
and publish `available`, `partial`, or `missing` attestations.

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
  "select c.release_id, c.title, s.pool_id, s.source_node_id
   from federated_release_cards c
   join federated_release_sources s using (release_id)
   order by c.updated_at desc limit 20"
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

The `gonzbnet_test` database is separate from the four running node databases,
so integration-test cleanup cannot disrupt the harness nodes.
