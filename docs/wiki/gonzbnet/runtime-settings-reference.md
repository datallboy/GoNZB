# Runtime Settings Reference

GoNZBNet runtime settings are edited under **Settings > GoNZBNet** and take
effect after the federation runtime reloads. The defaults are deliberately
conservative: a new node can consume and cache authorized data, but it does not
scan NNTP, publish local releases, validate payloads, contact peers, or relay
events until the operator opts in.

Bootstrap identity and listener settings remain in `config.yaml`; see
[Configuration And Deployment](./configuration-and-deployment.md).

## Recommended Defaults

The checked-in defaults are suitable as a safe starting point and should not be
changed globally for a specific topology:

- `visibility: unlisted` permits direct-address admission without claiming
  global discovery that does not exist yet.
- consumer, index projection, and the bounded manifest cache are enabled;
  scanning, publishing, validation, health, coverage, scheduling, relay, and
  network synchronization are opt-in.
- `coverage_mode: manual` prevents a newly enabled indexer from unexpectedly
  accepting pool work. Choose another mode only after creating assignments.
- metadata/article/segment validation is configured, while payload sampling,
  PAR2, and checksum work remain off because they cost more bandwidth and CPU.
- provider/source identifiers and insecure peer HTTP remain off for privacy and
  transport safety.
- protocol sizes, rates, and time windows are bounded. Raise them only when
  metrics show a real need.

The defaults describe safe behavior, not a complete working federation. A node
must join a pool, receive the required capabilities, configure peers and a sync
transport, and enable the appropriate publication switches before data moves.

## Participation Roles

An enabled role advertises what the process is prepared to do. It does not
grant itself pool authority. The node also needs an active membership with the
corresponding capability.

| Setting | Default | Meaning |
| --- | --- | --- |
| Aggregator federation source | off | Makes projected federated releases searchable to local users and resolves their manifests. This is an aggregator setting displayed here for convenience. |
| `consumer_enabled` | on | Accepts authorized signed pool events and permits remote manifest resolution. |
| `scanner_enabled` | off | Connects indexer scraping to eligible coverage work and permits scanner evidence/publication paths. Requires indexer and NNTP configuration. |
| `index_projection_enabled` | on | Projects accepted release cards into the local federated catalog. Enable this on nodes that search, report, or retain the catalog. |
| `manifest_builder_enabled` | off | Builds signed resolution manifests from eligible releases in the local indexer. It has nothing to build on a consumer-only node. |
| `manifest_cache_enabled` | on | Stores verified manifests for repeat resolution and, when allowed, serves them to trusted pool members. |
| `validator_enabled` | off | Runs configured validation tiers against manifests and article samples. Requires NNTP access for article-level work. |
| `health_checker_enabled` | off | Produces local release/manifest health observations. Enable health publication separately to share them. |
| `coverage_enabled` | off | Reads and publishes coverage plans, assignments, claims, checkpoints, and outcomes. It does not perform the NNTP scan itself. |
| `scheduler_enabled` | off | Permits coordination work. In automatic mode the current runtime reassigns stale claims; initial plans/assignments are still created administratively. |
| `relay_enabled` | off | Forwards eligible signed pool events. It does not bypass event authorization or turn the process into a separate relay service. |

It is valid to enable every role on one node. Splitting roles is an operational
choice for independent evidence, availability, security boundaries, and
resource isolation. See [Deployment Topologies](./deployment-topologies.md).

## Node, Peers, And Admission

| Setting | Default | Meaning |
| --- | --- | --- |
| `node_alias` | empty | Human-readable name shown to administrators. The Ed25519-derived node ID remains the authoritative identity. |
| `advertise_url` | empty | Externally reachable GoNZBNet protocol base URL that peers should call. Use HTTPS outside loopback development. |
| `publish_pool_ids` | empty | Restricts local publication to listed pool IDs. Empty means every active local membership that grants the event's required capability. |
| `manual_peers` | empty | Explicit peer protocol URLs used by sync workers. A URL supplies reachability, not membership or trust. |
| `allow_pool_creation` | on | Allows an authenticated local administrator to create pool genesis state. It does not allow remote anonymous creation. |
| `allow_join_requests` | on | Lists eligible pools during admission and accepts signed candidate requests. Administrators must still approve them. |
| `admission_relay_enabled` | on | Relays signed admission fragments between candidates and pool administrators. A relay cannot approve a request by itself. |
| `allow_insecure_peer_http` | off | Allows the private-network HTTP exception. Keep off in production; loopback development is already treated specially. |

### Visibility Choices

Visibility is admission/discovery posture, not a firewall or authentication
control. The node profile and well-known endpoint remain reachable if someone
already knows the address.

| Choice | Current behavior | Use when |
| --- | --- | --- |
| `private` | Hides the node's admission pool list unless the request contains a valid signed invitation. Private pools are also invitation-gated. | Invitation-only nodes and pools. |
| `unlisted` | Returns eligible admission pools to a direct visitor but does not claim public discovery. | Recommended default for explicit-URL or invitation-based deployments. |
| `pool` | Advertises pool-scoped discovery intent in the node profile. Admission currently behaves like `unlisted`. | Recording the intended future posture for a pool-facing node. |
| `public` | Advertises public discovery intent in the node profile. Admission currently behaves like `unlisted`. | Recording the intended future posture for a publicly reachable relay or directory participant. |

GoNZBNet v1 does not yet implement a public directory, DHT, mDNS, or NAT
traversal, so `pool` and `public` do not automatically publish the node
anywhere. Protect the listener with normal bind, firewall, reverse-proxy, TLS,
and authentication controls.

## Publication And Shared Health

| Setting | Default | Meaning |
| --- | --- | --- |
| `publish_release_cards_enabled` | off | Periodically signs compact searchable cards for eligible local indexer releases. Requires publisher/scanner pool authority. |
| `publish_release_cards_batch_size` | 50 | Maximum release cards considered in one publication batch. |
| `publish_release_cards_interval_minutes` | 10 | Delay between release publication runs. |
| `manifest_availability_enabled` | off | Publishes that this node can resolve or serve a signed manifest. It does not reveal user grabs. |
| `health_attestations_enabled` | off | Publishes signed aggregate health attestations created from local health/validation results. |
| `health_attestations_batch_size` | 50 | Maximum health items considered per publication run. |
| `health_attestations_interval_minutes` | 30 | Delay between health publication runs. |

The role flags enable producers; the publication flags decide whether their
results leave the node. For example, a local validator can run with health
attestation publication off, but its evidence then remains local.

## Scanner And Coverage

Coverage divides indexer scrape ranges among eligible scanners. It avoids
duplicated work; it is not an alternative scanner or validator.

| Setting | Default | Meaning |
| --- | --- | --- |
| `scanner_max_groups` | 25 | Advertised scanner group capacity and the bound used by stale-claim reassignment. |
| `scanner_max_articles_per_hour` | 250,000 | Advertised article-rate budget for coverage planning. Size it below the actual provider and host limit. |
| `scanner_claim_ttl_minutes` | 30 | How long a scanner claim remains active without progress or completion. |
| `scanner_checkpoint_interval_seconds` | 300 | Progress checkpoint cadence; also the automatic stale-claim reassigner cadence. |
| `scanner_respect_remote_claims` | on | Avoids locally scraping a range currently claimed by another eligible scanner. Recommended for every shared pool. |
| `scanner_allow_unassigned_work` | off | Allows local scrape work without a signed pool assignment. Useful for a solo node, but can duplicate work in a multi-scanner pool. |
| `coverage_min_trust_for_claim` | 0.65 | Minimum projected trust score required for a node to claim coordinated work. |
| `coverage_validation_overlap_percent` | 10 | Portion of work intended for independent overlap checking. More overlap improves confidence but repeats NNTP work. |
| `coverage_stale_claim_penalty` | on | Allows abandoned/expired claims to reduce the claimant's coordination standing. |

### Coverage Mode Choices

| Choice | What the runtime does | Recommended scenario |
| --- | --- | --- |
| `manual` | Does not attach assigned pool coverage to indexer scraping when unassigned work is also disabled. Plans, assignments, and reassignments are administrator-driven. | Safe default, local-only indexing, or a pool being brought up deliberately. |
| `scheduler` | Allows the scanner to consume signed assignments and publish claims/outcomes. It does not run automatic stale-claim reassignment. | A scanner whose assignments are managed by a pool administrator or another coordinator. |
| `automatic` | Does everything in `scheduler` and runs the stale-claim reassigner when coverage and scheduler roles are enabled and the pool grants coordinator authority. | The designated coordinator in an established pool. |

Automatic mode currently maintains existing coordinated work; it does not
invent the initial coverage plan or assignments. Create those through pool
administration first. On a solo node, either keep coverage manual and enable
unassigned work, or create an assignment and use automatic mode if testing the
full coordination path.

### Provider Scope Choices

`coverage_provider_scope_mode` controls whether coordinated coverage includes
the node's provider scope.

| Choice | Meaning |
| --- | --- |
| `hash_only` | Includes a stable scoped provider/backbone hash in coordination data. This lets the pool distinguish viewpoints without receiving provider credentials or a plain provider name. Recommended when measuring evidence diversity. |
| `disabled` | Omits provider scope. This maximizes privacy but makes it harder to avoid assigning validation or overlap work to equivalent NNTP viewpoints. |

## Validation And Manifest Cache

| Setting | Default | Meaning |
| --- | --- | --- |
| `validation_batch_size` | 25 | Maximum manifests selected in one validation run. |
| `validation_interval_minutes` | 15 | Delay between validation runs. |
| `validation_tiers` | `metadata`, `article_stat`, `segment_stat` | Ordered enabled checks. `metadata` verifies structure/signatures; article and segment tiers query NNTP availability; `checksum` performs the deeper checksum tier. |
| `validation_max_manifests_per_hour` | 500 | Advertised validator capacity and hourly work budget. |
| `validation_sample_percent` | 10 | Percentage of manifest articles/segments sampled rather than checking the entire payload. |
| `validation_allow_sample_payload_fetch` | off | Permits fetching sample article bodies. Costs substantially more bandwidth than status checks. |
| `validation_allow_par2_validation` | off | Permits PAR2-based content validation when suitable data and tools are available. |
| `validation_publish_provider_scope_hash` | on | Includes the validator's scoped provider hash in evidence so results from the same viewpoint can be grouped. |
| `checksum_validation_enabled` | off | Enables checksum validation work. Also include `checksum` in `validation_tiers`; leave off unless the extra I/O is intentional. |
| `manifest_cache_max_bytes` | 10 GiB | Maximum verified-manifest cache size. Reduce it on small disks. |
| `manifest_cache_ttl_days` | 90 | Maximum cache residence time before expiry. |
| `manifest_cache_serve_to_trusted_pools` | on | Allows authorized members of trusted pools to fetch eligible cached manifests. |

The default validation profile is a reasonable low-cost baseline after the
validator role is deliberately enabled. Independent validators should use
different NNTP providers or backbones when possible; three validators on the
same provider do not provide three independent availability viewpoints.

## Synchronization And Gossip

All transports are off by default so a newly installed node does not contact
configured addresses unexpectedly. A real multi-node deployment should enable
at least one transport and supply reachable peers.

| Setting | Default | Meaning |
| --- | --- | --- |
| `pull_sync_enabled` | off | Periodically asks peers for authorized events missing locally. This is the simplest baseline transport. |
| `pull_sync_interval_minutes` | 10 | Delay between pull cycles. |
| `push_sync_enabled` | off | Periodically sends authorized local events to peers, reducing propagation delay. |
| `push_sync_interval_minutes` | 10 | Delay between push cycles. |
| `push_sync_batch_size` | 100 | Maximum events in one push batch. |
| `websocket_gossip_enabled` | off | Maintains WebSocket-based event gossip for lower-latency, multi-peer propagation. |
| `gossip_interval_minutes` | 1 | Gossip worker cadence. |
| `gossip_batch_size` | 100 | Maximum events selected per gossip exchange. |
| `gossip_ttl` | 4 | Maximum forwarding hops for a gossip item. |
| `gossip_fanout` | 4 | Maximum peers selected for each gossip fanout. |
| `peer_exchange_enabled` | off | Lets authenticated members of a shared pool exchange endpoints. It never grants pool membership. |

For two or three stable nodes, pull plus push is easy to reason about. Add
WebSocket gossip and peer exchange when lower latency or a changing/larger
peer set justifies the extra connections.

## Transport Limits And Privacy

| Setting | Default | Meaning |
| --- | --- | --- |
| `max_event_bytes` | 262,144 (256 KiB) | Rejects oversized signed events before expensive processing. |
| `max_manifest_bytes` | 10,485,760 (10 MiB) | Rejects oversized resolution manifests. |
| `manifest_fetch_timeout_seconds` | 20 | Deadline for a peer manifest request. |
| `max_batch_events` | 100 | Maximum accepted events in a protocol batch. |
| `rate_limit_events_per_minute` | 120 | Per-peer/event ingestion rate bound. |
| `time_tolerance_seconds` | 120 | Allowed clock skew for time-sensitive protocol validation. Keep node clocks synchronized. |
| `max_event_age_hours` | 720 (30 days) | Oldest event accepted during normal synchronization. |
| `nonce_ttl_seconds` | 600 | Replay-protection nonce lifetime. |
| `share_provider_backbone_hash` | off | Adds a non-credential provider/backbone hash to advertised evidence context. Useful for diversity reporting; disabled for privacy. |
| `share_source_indexer_hash` | off | Adds a non-credential source-indexer hash for provenance grouping; disabled for privacy. |

Changing size, rate, and time limits can create interoperability failures when
peers use smaller limits. Prefer the defaults unless observed traffic requires
a coordinated pool-wide adjustment.
