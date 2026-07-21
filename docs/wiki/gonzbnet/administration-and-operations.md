# Administration And Operations

GoNZBNet administration is local to each node. The WebUI, CLI, and admin API
operate against that node's PostgreSQL database and persistent identity.
Federation authenticates node keys; local users, sessions, and API keys never
cross the federation boundary.

## Admin WebUI

Use **Settings > GoNZBNet** to configure operational participation: local
roles, aggregator consumption, peers/admission, publication and shared health,
coverage, validation, caching, synchronization, protocol limits, and privacy.
The page identifies the bootstrap-only fields that still require YAML and a
process restart.

Open `/admin/gonzbnet`. Its four views separate routine understanding from
protocol detail:

- **Overview** summarizes the node, healthy enabled jobs, connected peers,
  active pools, warnings, and pending admissions. Its pool-member roster counts
  unique active nodes and groups each node's pool roles and operational
  capabilities, using shared aliases and advertised addresses when available.
- **Roles** explains the grouped jobs **Find and use releases**, **Contribute
  releases**, **Verify release health**, **Coordinate scanning**, and the
  **Connection layer**. Select an enabled role tab to see what it reads, what it
  produces, what an idle pass means, its workers, and recent durable results.
  For example, validators show their pending/completed manifest tasks and signed
  article or release-health evidence. A successful empty poll is reported as a
  check, not as useful work. Off jobs are kept in one collapsed section.
- **Pools** shows release-health evidence, article-availability evidence,
  freshness, reporting members, and their signed contributions. Pool creation,
  joining, admission, and membership controls are kept with this view.
- **Activity** graphs successful work, processed items, and failures from
  bounded five-minute/hourly history. Raw protocol diagnostics, manual actions,
  governance, and key operations remain available under **Advanced tools**.

These views are read-only descriptions of effective behavior. Use **Settings >
GoNZBNet** to enable jobs or change their cadence and limits.

Sensitive governance and key controls are grouped under **Destructive stuff**.
Governance actions require `CONFIRM`. Key export and rotation retain the backend
phrases `export-gonzbnet-node-key` and `rotate-gonzbnet-node-key`; encrypted
export also requires an encryption password.

Access is controlled by granular local permissions for read, pools, peers,
moderation, and keys. The server remains the authorization boundary even when
the UI hides an unavailable action.

## CLI

Commands use the normal `--config` option and output JSON:

```sh
gonzb --config config.yaml gonzbnet status
gonzb --config config.yaml gonzbnet pools
gonzb --config config.yaml gonzbnet peers
gonzb --config config.yaml gonzbnet sync pull
gonzb --config config.yaml gonzbnet sync push --limit 100
```

`status`, `pools`, and `peers` inspect local state. `sync` performs one signed
pull or push pass and exits; it does not start another daemon.

For NNTP provider diagnostics, `nntp-check` uses the production NNTP manager:

```sh
gonzb --config config.yaml gonzbnet nntp-check \
  --group alt.binaries.example \
  --message-id '<article@example.invalid>'
```

## Initial Setup

1. Enable the API and GoNZBNet modules and configure PostgreSQL.
2. Set a durable `keys_dir`, protected key password if required, and reachable
   HTTPS `advertise_url`.
3. Start the node and record/back up its node identity.
4. Create a pool or join one using an explicit address or signed invitation.
5. Approve the candidate from the required number of active administrators.
6. Grant only the pool capabilities needed by the node's role.
7. Grant local roles the required pool search/get/resolve access.
8. Enable pull/push and role-specific workers, then confirm readiness and
   synchronization diagnostics.

Do not distribute local admin credentials to peers. Pool membership is granted
only by signed governance events.

## Routine Pool Operations

For a join request, verify the candidate node ID, advertised URL, requested
capabilities, pool/genesis identity, and existing administrator fragments
before approval. Repeating the same approval is idempotent.

Revocation is pool-specific. After revoking a node, trigger or wait for sync and
confirm the target receives its revocation while losing normal outbox access.
Membership in another pool must remain unchanged.

Use tombstones for signed moderation of federated content. Blocking a node is a
local transport/acceptance decision and does not rewrite the append-only event
history.

## Metrics

Authenticated local-admin endpoints expose process-local metrics and bounded
operator reporting:

```text
GET /api/v1/admin/gonzbnet/metrics
GET /api/v1/admin/gonzbnet/metrics/prometheus
GET /api/v1/admin/gonzbnet/overview
GET /api/v1/admin/gonzbnet/roles
GET /api/v1/admin/gonzbnet/activity?window=24h&pool_id=POOL
GET /api/v1/admin/gonzbnet/pools/POOL/health
GET /api/v1/admin/gonzbnet/diagnostics/article-availability?pool_id=POOL
```

Metrics contain no local usernames, API keys, searches, grabs, or download
history. Prometheus counters reset when the process restarts. Activity reporting
is flushed in five-minute batches, keeps five-minute buckets for 48 hours, then
hourly buckets for 90 days. Pool health and member contribution summaries are
derived from durable signed federation evidence rather than runtime counters.
Scrape the Prometheus route when external alerting or longer retention is
required.

Useful operational signals include accepted/rejected events, signature and
authorization failures, pull/push delivery results, manifest fetch/cache
outcomes, admission status, validation work, and stale coverage claims.

## Troubleshooting

### Node will not start

- Confirm `modules.gonzbnet.enabled`, `modules.api.enabled`, and `store.pg_dsn`.
- Check PostgreSQL connectivity and migration completion.
- Verify `keys_dir` permissions and key password.
- Review configuration validation for unsupported visibility, coverage mode,
  validation tier, negative limit, live-query, or user-context settings.

### Peer cannot synchronize

- Verify both advertised URLs and HTTPS trust.
- Confirm the remote profile's node ID matches the expected key.
- Check active membership in the event's pool and required capabilities.
- Inspect rejected events for signature, timestamp, nonce, chain, pool, policy,
  rate, and body-size failures.
- A manual peer entry provides connectivity only; it does not grant pool access.

### Release is not searchable

- Confirm the publisher considered it public-ready under the effective indexer
  policy.
- Confirm release publication is enabled and the node has an eligible active
  pool capability.
- Check accepted ReleaseCard events and the release/source projection.
- Verify the home node has `index_projection_enabled`, the aggregator GoNZBNet
  source enabled, and local role access to the pool.

### Grab cannot resolve a manifest

- Verify pool get/resolve permission before investigating cache state.
- Confirm the card has a stable manifest ID and at least one authorized source.
- Check manifest availability, source health, fetch timeout, response-size
  limit, signature validation, and cache policy.
- A publisher needs `manifest_builder_enabled` to create locally sourced signed
  manifests.

### Scanner or validation work stalls

- Confirm the node's pool capability grant and relevant enable flags.
- Check assignment expiry, active claims, provider-scope hashes, checkpoints,
  stale-claim reassignment, NNTP provider roles, and rate budgets.
- Structural `unverified` validation is expected when provider-backed checks
  are unavailable.

## Backup And Recovery

Back up PostgreSQL and the node identity key. The identity is not replaceable
by rebuilding projections: losing it changes the node ID and invalidates the
node's existing membership relationship.

Blob/manifest caches can be rebuilt from authorized sources. Accepted events
and typed projections should be restored together from a consistent PostgreSQL
backup. The pending-projection mechanism is only for legacy or explicit repair;
normal inbound projection failures roll back their event append.

Never commit runtime `store/`, `store-*`, `.e2e/`, key, database, blob, NZB,
cookie, token, or log data.
