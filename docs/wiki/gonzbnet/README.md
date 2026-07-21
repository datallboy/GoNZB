# GoNZBNet

GoNZBNet is GoNZB's pool-scoped federation module. It lets independently
operated nodes exchange signed release metadata, resolution manifests,
validation results, health statements, and scanner coordination without
sharing local accounts, API keys, searches, grabs, or download history.

The module runs inside the `gonzb` modular monolith. There is no separate
GoNZBNet service. A deployment can participate as a consumer, scanner,
validator, cache, relay, or a combination of those roles by enabling the
corresponding capabilities.

## Documentation

- [Architecture And Data Flow](./architecture-and-data-flow.md) explains the
  runtime, storage ownership, event flow, release discovery, and manifest path.
- [Configuration And Deployment](./configuration-and-deployment.md) covers
  prerequisites, configuration patterns, capability combinations, identity,
  and transport settings.
- [Runtime Settings Reference](./runtime-settings-reference.md) defines every
  WebUI runtime setting, dropdown choice, default, and operational tradeoff.
- [Deployment Recommendations](./deployment-topologies.md) explains when one
  all-in-one node is best and when roles deserve separate containers, hosts, or
  operators.
- [Federation Protocol And Security](./federation-protocol-and-security.md)
  documents signed events, pool authorization, admission, synchronization,
  protocol endpoints, limits, and privacy boundaries.
- [Administration And Operations](./administration-and-operations.md) covers
  the WebUI, CLI, routine workflows, metrics, recovery, and troubleshooting.
- [Development Reference](./development-reference.md) maps packages, database
  projections, extension points, and test expectations.
- [Four-Node E2E Testing](./e2e-testing.md) documents the disposable local
  federation test and the guarantees it verifies.

The [implementation specification](../../GoNZBNet_Codex_Implementation_Spec.md)
is retained as design background. It inspired the module, but this wiki and
the code describe current behavior when the specification differs.

## What The Module Does

GoNZBNet maintains a persistent Ed25519 node identity and an append-only signed
event log. Nodes join one or more trust pools, then exchange only events they
are authorized to see. Accepted events are projected into local PostgreSQL
tables used by the aggregator and administrative views.

The normal release path is:

1. A scanner or local indexer forms a release and publishes a pool-scoped
   `ReleaseCard`.
2. Peers pull, receive, validate, and project the card into their local
   federated catalog.
3. Local users search that cache through the normal GoNZB/Newznab API. Search
   is never forwarded live to federation peers.
4. On grab, the home node resolves the signed `ResolutionManifest` from an
   authorized source, verifies it, caches it under the configured policy, and
   generates an NZB locally.
5. A repeat grab uses the verified local cache while it remains valid.

Pool governance, membership, moderation, validation, coverage, health, and
scanner coordination use the same signed-event and local-projection model.

## Deployment Roles

Roles are capability combinations, not separate binaries:

| Role | Typical capabilities |
| --- | --- |
| Consumer | consumer, index projection, manifest cache, pull sync |
| Publisher/scanner | scanner, release-card publishing, manifest builder, coverage |
| Validator | validator, health checker, NNTP access |
| Relay | relay and sync, optionally without scanning or validation |
| Integrated | any combination appropriate for an all-in-one node |

`mode` describes the intended deployment shape, while individual capability
flags determine actual runtime behavior. PostgreSQL is required whenever the
GoNZBNet module is enabled.

## Security And Privacy Invariants

- Node identity is independent of local user identity.
- Signed protocol objects use RFC 8785 canonical JSON and Ed25519 signatures.
- Pool events are visible only to active members of the named pool.
- Protocol v1 accepts exactly one pool per protected event.
- Unknown pools fail closed.
- Local sessions and API keys authenticate only to the home node.
- Search, grab, and download history remain local.
- Live federated search and user-context forwarding are disabled by validation.
- HTTPS is required for non-loopback peers unless insecure HTTP is explicitly
  enabled for a private local test.
- Node private keys and runtime stores are generated data and must not be
  committed.

## Current Boundaries

GoNZBNet is pre-release and implements the integrated v1 design. Public global
discovery, DHT/mDNS discovery, NAT traversal, cross-pool bridging, and a
standalone relay process are not implemented. First contact uses an explicit
node address or a signed invitation. Protocol metrics are process-local. The
admin UI keeps bounded local activity history for operational visibility;
external scraping is still required for alerting or retention beyond 90 days.

Accepted inbound events and their required typed projections commit in one
PostgreSQL transaction. The pending-projection table remains only as a repair
path for events written by older builds or explicit recovery operations.
