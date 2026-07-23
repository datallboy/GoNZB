# Architecture And Data Flow

## Runtime Boundary

GoNZBNet is an optional module registered in the main `gonzb serve` runtime.
It shares GoNZB's configuration, PostgreSQL connection, NNTP manager, logging,
HTTP server, local authentication, and lifecycle management. Module startup
loads or creates the node identity, persists its public identity, registers
configured peers, applies cache policy, and starts only the enabled workers.

The runtime can start these recurring activities:

- pool-admission polling;
- release-card and resolution-manifest publication;
- validation and health-attestation publication;
- signed pull, push, and WebSocket gossip synchronization;
- automatic stale coverage-claim reassignment;
- scanner coordination through the existing indexer scrape runtime.

Workers select the local node's active pool memberships on every pass.
`publish_pool_ids` may restrict those memberships, but cannot grant access to a
pool. This makes newly approved pools usable without restarting the process and
keeps multi-pool publication isolated.

## Main Components

| Component | Responsibility |
| --- | --- |
| Identity | Persistent Ed25519 key, deterministic node ID, signing |
| Events | Signed envelope construction and verification |
| Canonical JSON | RFC 8785 bytes for hashes, IDs, and signatures |
| Pools/admission | Genesis, membership, invitations, approval quorum, revocation |
| Publisher | Release cards, manifests, validation, health, and availability events |
| Sync | Discovery, handshake, signed pull/push/gossip, receive validation |
| Coverage | Capacity, plans, assignments, claims, outcomes, checkpoints |
| Manifest resolver | Authorized source selection, fetch, verification, cache, NZB generation |
| PostgreSQL store | Event log, pool state, typed projections, diagnostics, delivery state |
| Aggregator adapter | Reads the local federated projection for normal search/get operations |
| Admin API/UI/CLI | Local operation and diagnostics; never federation authentication |

## Event Ingest

All network ingest paths converge on the same verification and persistence
rules:

1. Enforce request body, batch, and rate limits.
2. Authenticate the remote node where the route requires it.
3. Reject invalid JSON and duplicate object names before ordinary decoding.
4. Canonicalize and verify the body hash, event ID, author key, and signature.
5. Validate the typed event body, timestamps, pool fields, and event type.
6. Enforce pool membership, policy, capability, moderation, and chain rules.
7. Append the accepted event and apply its required typed projection in one
   PostgreSQL transaction.
8. Record rejection or fork evidence for diagnostics without projecting it.

Accepted signed envelopes remain the source evidence. Relational projections
make authorization, search, scheduling, and diagnostics efficient. A projection
does not replace or modify the signed event.

## Release Publication And Search

Publisher nodes read locally public-ready indexer releases or completed scan
output. Readiness uses the effective indexer release policy rather than a
separate federation threshold. Each selected pool receives its own signed
event, while stable release and manifest IDs remain content-derived.

`ReleaseCard` projections populate the local federated release catalog. The
GoNZBNet aggregator source searches only this local PostgreSQL projection.
Results are filtered by the local user's RBAC pool grants. No query, username,
API key, result selection, or grab action is sent to a remote node.

## Manifest Resolution And NZB Generation

A ReleaseCard advertises a stable manifest ID and eligible sources. The
manifest builder creates a signed `ResolutionManifest` containing the release
files and article Message-IDs. Availability statements describe whether a
specific source can serve that manifest to a pool.

On a local get request, the resolver:

1. authorizes the local role for the release pool;
2. checks for an accepted, unexpired local manifest cache entry;
3. selects an authorized trusted source when the cache misses;
4. sends a node-authenticated manifest request without user context;
5. verifies the signed response, manifest identity, source, pool, size, and
   message IDs;
6. caches the accepted manifest under the byte and TTL policy;
7. renders the NZB locally.

The shared aggregator cache is not an authorization shortcut. Pool access is
checked before cached GoNZBNet content is served.

## Scanner, Coverage, And Validation

Scanner-capable nodes publish provider-scoped capacity and heartbeats. Coverage
coordination represents work as signed group observations, plans, assignments,
claims, outcomes, and checkpoints. Provider scope is shared as a hash when
enabled; raw provider account details remain local.

Claims are leases. Scanners can respect remote claims, require assignments, and
publish progress checkpoints. Automatic mode can penalize and reassign stale
claims. Scan results can become ReleaseCards and manifests without requiring
the node to expose its full local index.

Validator nodes consume queued manifest work. Configured tiers range from
metadata checks through NNTP article/segment checks and optional checksum work.
Results become signed validation and health events. Without suitable NNTP
access, structural results remain explicitly unverified rather than being
presented as provider-backed evidence.

## Storage Ownership

PostgreSQL owns durable federation state:

- node and peer records;
- accepted and rejected events, author sequence state, and chain issues;
- trust pools, policies, memberships, role access, and invitations/admissions;
- release cards, sources, manifests, validation, health, trust, and tombstones;
- coverage plans, assignments, claims, outcomes, and checkpoints;
- delivery cursors, pending work, and administrative diagnostics.

SQLite remains the local settings/authentication store where configured. Blob
and generated NZB storage follow the normal GoNZB store configuration. The
node's private key lives under `gonzbnet.keys_dir`. These runtime directories
are deployment data, not repository content.

## Failure And Recovery Semantics

- Duplicate delivery is idempotent.
- Out-of-order author sequences create a gap that can close when the missing
  predecessor arrives.
- Conflicting sequences or known link mismatches are retained as fork evidence
  and are not projected.
- A projection failure rolls back the accepted append.
- `federation_pending_projections` supports legacy repair, not normal ingest.
- A revoked node may retrieve only its own signed revocation so it can converge
  on the loss of access.
- Cache loss causes a verified refetch; identity-key loss creates a different
  node identity and therefore is not equivalent to cache loss.
