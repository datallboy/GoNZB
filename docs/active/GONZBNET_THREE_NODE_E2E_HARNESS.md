# GoNZBNet Three-Node E2E Harness

Status: active

## Scope

Provide a repeatable local environment with three independent GoNZB processes,
three PostgreSQL databases, three SQLite control-plane stores, three Ed25519
identities, separate blob/key directories, and loopback federation HTTP URLs.

## Node Roles

- Node A: scanner, coverage coordinator, manifest builder, cache, consumer.
- Node B: validator, health checker, cache, consumer.
- Node C: consumer/cache node with relay mode enabled.

## Artifacts

- `docker-compose.gonzbnet-e2e.yml` provisions disposable PostgreSQL.
- `test/e2e/gonzbnet/node-{a,b,c}.yaml` define isolated node roles.
- `scripts/gonzbnet_e2e.sh` starts, bootstraps, checks, logs, and resets the
  environment.
- `docs/wiki/gonzbnet/e2e-testing.md` is the operator test matrix.

## Automated Baseline

The harness now validates independent identities, local admin bootstrap, trust
pool membership, signed push/pull transport, exactly-once append and tombstone
projection, rejection of unsigned protected reads, and rejection of cross-node
reuse of a local user session.

ReleaseCard, manifest, article-availability, and scanner scenarios still need
indexed Message-ID fixtures or a test NNTP provider. The wiki documents those
workflows and the existing indexer subcommands used to produce their inputs.
