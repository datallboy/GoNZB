# Administration And Operations

GoNZBNet administration is local to each node. The WebUI, CLI, and admin API
operate against that node's PostgreSQL database and persistent identity.
Federation authenticates node keys; local users, sessions, and API keys never
cross the federation boundary.

## Admin WebUI

Open `/admin/gonzbnet`. `Overview` contains routine operations:

- node identity, advertised profile, readiness, and capability state;
- peer status and synchronization;
- trust-pool creation and policy;
- direct-address or invitation-based joining;
- pending admission approval/rejection and membership state.

`Advanced` contains event and delivery diagnostics, rejected events,
capabilities, coverage, validation tasks, manifests, release sources, health,
reputation, role access, protocol metrics, and governance controls.

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

Authenticated local-admin endpoints expose process-local metrics:

```text
GET /api/v1/admin/gonzbnet/metrics
GET /api/v1/admin/gonzbnet/metrics/prometheus
```

Metrics contain no local usernames, API keys, searches, grabs, or download
history. Counters reset when the process restarts. Scrape the Prometheus route
when durable history or alerting is required.

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
