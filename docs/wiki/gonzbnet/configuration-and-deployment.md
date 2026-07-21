# Configuration And Deployment

## Prerequisites

GoNZBNet requires the API module and PostgreSQL index store. Enable the
aggregator and its GoNZBNet source when local users should search federated
releases. Scanner and validator roles additionally need appropriate indexer or
NNTP configuration.

```yaml
modules:
  aggregator: { enabled: true }
  gonzbnet: { enabled: true }
  api: { enabled: true }

aggregator:
  sources:
    gonzbnet: { enabled: true }

store:
  pg_dsn: "postgres://gonzb:change-me@postgres/gonzb?sslmode=require"
```

Set the top-level `bind_address` when the HTTP server must listen on a specific
interface. An empty value preserves the default all-interface listener. The
local E2E fixture sets `bind_address: 127.0.0.1` on every test node.

The module is disabled by default. Configuration is loaded from YAML and can
be overridden through GoNZB's environment binding. Local runtime settings can
also change supported operational fields through the admin settings surface.

## Minimal Consumer

This node joins an existing pool, synchronizes signed events, projects releases
into its local catalog, and caches manifests for local grabs:

```yaml
gonzbnet:
  mode: consumer
  node_alias: "home-consumer"
  advertise_url: "https://node.example/gonzbnet/v1"
  keys_dir: "/var/lib/gonzb/gonzbnet/keys"
  visibility: private
  consumer_enabled: true
  index_projection_enabled: true
  manifest_cache_enabled: true
  pull_sync_enabled: true
  pull_sync_interval_minutes: 10
  push_sync_enabled: true
  push_sync_interval_minutes: 10
  live_query_enabled: false
  send_user_context: false
```

Join with an explicit HTTPS node address or a signed invitation in the admin
UI. `manual_peers` is optional and is not a membership grant.

## Publisher Or Scanner

Publisher nodes need an active pool membership with the required capabilities.
The local indexer remains responsible for forming public-ready releases.

```yaml
gonzbnet:
  mode: integrated
  scanner_enabled: true
  index_projection_enabled: true
  publish_release_cards_enabled: true
  publish_release_cards_batch_size: 50
  publish_release_cards_interval_minutes: 10
  manifest_builder_enabled: true
  manifest_availability_enabled: true
  coverage_enabled: true
  scheduler_enabled: true
  coverage_mode: automatic
  scanner_respect_remote_claims: true
  scanner_allow_unassigned_work: false
```

An empty `publish_pool_ids` uses every active local membership for which the
node has the event's required capabilities. A non-empty list restricts that
set; it cannot publish into a pool the node has not joined.

## Validator

Validator and health-checker nodes should have a configured NNTP provider with
the `validator` role:

```yaml
gonzbnet:
  validator_enabled: true
  health_checker_enabled: true
  health_attestations_enabled: true
  validation_interval_minutes: 15
  validation_batch_size: 25
  validation_tiers: [metadata, article_stat, segment_stat]
  validation_sample_percent: 10
  checksum_validation_enabled: false
```

Supported validation tiers are `metadata`, `article_stat`, `segment_stat`, and
`checksum`. Payload sampling, PAR2 validation, and checksum work are opt-in
because they can consume substantially more bandwidth and CPU.

## Identity And Key Storage

The first module start creates an Ed25519 identity in `keys_dir`. The node ID is
derived from its public key and remains stable as long as that key remains
stable.

- Back up the key independently from caches and databases.
- Restrict filesystem access to the GoNZB process account.
- Set `key_password` through a protected secret source when encrypted key
  storage is required; do not commit it in repository configuration.
- Use the authenticated admin key-export and rotation operations deliberately.
  Rotation changes protocol identity and requires governance coordination.
- Never place `keys_dir` inside source-controlled test or project content.

Runtime `store/`, `store-*`, `.e2e/`, database, blob, NZB, cookie, and token
content is generated and ignored by Git.

## Network And Discovery

| Setting | Meaning |
| --- | --- |
| `advertise_url` | Externally reachable protocol base URL for this node |
| `http_base_path` | Protocol route prefix; defaults to `/gonzbnet/v1` |
| `http_enabled` | Exposes federation HTTP routes when API is enabled |
| `visibility` | `private`, `unlisted`, `pool`, or `public` discovery posture |
| `allow_pool_creation` | Allows local creation of pool genesis state |
| `allow_join_requests` | Accepts candidate admission submissions |
| `admission_relay_enabled` | Allows a member to relay admission fragments |
| `manual_peers` | Explicit synchronization peers, not automatic trust |
| `peer_exchange_enabled` | Allows authenticated pool peers to exchange endpoints |
| `allow_insecure_peer_http` | Test/private-network exception to HTTPS policy |
| `network_id` | Gossip network separation value |

Production peers should use HTTPS. Plain HTTP is rejected except loopback or
when `allow_insecure_peer_http` is explicitly enabled. The checked-in E2E
fixture enables it only for loopback processes.

## Capability Flags

Capability flags are independent so deployments remain modular:

- `consumer_enabled` consumes federated catalog data.
- `scanner_enabled` participates in scanning and permits release publication.
- `index_projection_enabled` projects accepted release data locally.
- `manifest_builder_enabled` builds signed manifests from local source data.
- `manifest_cache_enabled` stores verified remote manifests.
- `validator_enabled` processes validation tasks.
- `health_checker_enabled` produces health evidence.
- `coverage_enabled` participates in coordination.
- `scheduler_enabled` creates or maintains coordinated coverage work.
- `relay_enabled` forwards eligible signed events in-process.
- `pull_sync_enabled`, `push_sync_enabled`, and
  `websocket_gossip_enabled` select transport workers.

Advertised capabilities are derived from active behavior. Enabling a flag does
not bypass pool membership or capability grants.

## Cache And Resource Limits

`manifest_cache_max_bytes` and `manifest_cache_ttl_days` bound verified cache
storage. `manifest_cache_serve_to_trusted_pools` controls whether eligible pool
members can use cached manifests. Event, manifest, batch, request-rate,
timestamp, nonce, and fetch-timeout settings bound protocol work before it
reaches expensive storage or projection paths.

Scanner controls include maximum groups, article budget, claim TTL, checkpoint
interval, remote-claim behavior, and provider-scope policy. Coverage thresholds
and validation sampling should be sized for the provider and node role.

## Privacy Settings

`send_user_context` must remain false and `live_query_enabled` is reserved.
Configuration validation rejects either being enabled. Provider backbone and
source-indexer hashes are also disabled by default and reveal only a scoped
hash when explicitly enabled, never provider credentials.

## Deployment Shapes

GoNZBNet does not introduce a dependency between otherwise independent GoNZB
modules beyond the capabilities selected by the operator:

- downloader-only deployments leave GoNZBNet disabled;
- aggregator-only deployments can act as federated consumers;
- indexer-only deployments can publish and scan without serving downloads;
- all-in-one deployments can combine consumer, publisher, validator, and admin
  behavior in one process.

All enabled GoNZBNet shapes require PostgreSQL because federation state and
authorization are relational and transactional.
