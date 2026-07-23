# GoNZBNet Federation Implementation Specification

**Status:** Codex-ready architecture and implementation brief
**Target project:** GoNZB
**Target runtime shape:** modular monolith first, optional relay extraction later
**Database assumption:** PostgreSQL
**Date:** 2026-07-07

This document captures the architecture and design decisions made for the GoNZBNet module. It is intended to be handed to Codex as the implementation source of truth. Codex should implement this design without inventing alternative trust, identity, transport, RBAC, or data-sharing semantics unless explicitly instructed later.

---

## 0. Executive Summary

GoNZBNet is a federated Usenet metadata discovery layer for GoNZB. It is not a new Usenet provider, not a BitTorrent-style payload-sharing network, and not a blockchain. It is a signed metadata and manifest federation layer that allows approved self-hosted GoNZB instances to share release discovery data, health observations, and on-demand NZB-equivalent manifests.

The intended behavior is:

```text
A user logs into their own GoNZB node.
The user configures Sonarr/Radarr/Prowlarr/SAB/NZBGet against their own GoNZB Newznab endpoint.
Their node federates with approved nodes.
Searches on the user's node return local and trusted remote results.
If the user's node does not have the NZB or manifest, it fetches a signed manifest from trusted peers.
The user's node validates the manifest, generates the NZB locally, caches it, and returns it through the user's own Newznab API endpoint.
Remote nodes never receive user API keys, usernames, search history, or download history.
```

GoNZBNet should be built around two layers:

```text
Layer 1: Federated discovery
Small signed release cards, health attestations, trust events, membership events, moderation events.

Layer 2: On-demand resolution
Large NZB-equivalent manifests fetched from trusted peers only when needed.
```

The most important architectural decisions are:

1. Keep GoNZBNet inside the modular monolith for v1.
2. Do not implement blockchain for v1.
3. Use signed append-only logs, event hashes, pool checkpoints, and M-of-N approval events.
4. Use Ed25519 node identity and canonical JSON signatures.
5. Use PostgreSQL relational tables plus `jsonb` projections, but never use `jsonb` reserialization as the source of signed bytes.
6. Local users authenticate only to their home node.
7. Remote nodes authorize other nodes, not users.
8. Search remote metadata from the local federated cache by default; do not live-broadcast user search queries.
9. Fetch full manifests on demand from trusted peers.
10. Expose accepted federated results through the existing GoNZB Newznab-compatible aggregator.

---

## 1. External References and Design Influences

These references inform the design, but GoNZBNet is not required to be wire-compatible with any of them.

1. **CometNet**: Used as a conceptual model for decentralized metadata sharing, signed contributions, trust pools, reputation, integrated mode, relay mode, manual peers, WebSocket gossip, and metadata-only sharing. CometNet explicitly shares torrent metadata rather than actual files.
   - https://github.com/g0ldyy/comet/blob/main/docs/cometnet/README.md
   - https://github.com/g0ldyy/comet/blob/main/docs/cometnet/cometnet.md
2. **ActivityPub**: Used as a conceptual model for actor-like federation, inbox/outbox delivery, and home-instance identity. GoNZBNet should copy the shape, not require full JSON-LD ActivityPub compatibility.
   - https://www.w3.org/TR/activitypub/
3. **Newznab API**: GoNZB already exposes a Newznab-compatible API; GoNZBNet should feed the existing aggregator and keep Newznab as the compatibility edge for NZB-aware clients.
   - https://torznab.github.io/spec-1.3-draft/external/newznab/api.html
4. **RFC 8785 JSON Canonicalization Scheme (JCS)**: Recommended for stable JSON signing and hashing.
   - https://www.rfc-editor.org/rfc/rfc8785.html
5. **RFC 8032 EdDSA / Ed25519**: Recommended signature algorithm for node identity and signed events.
   - https://www.rfc-editor.org/rfc/rfc8032.html
6. **RFC 8949 CBOR**: Optional future encoding for compact binary payloads. Do not use for v1 unless explicitly requested.
   - https://www.rfc-editor.org/rfc/rfc8949.html
7. **PostgreSQL JSON/JSONB documentation**: Use `jsonb` for queryable projections, not as the source of signed bytes.
   - https://www.postgresql.org/docs/current/datatype-json.html

---

## 2. Goals and Non-Goals

### 2.1 Goals

GoNZBNet must:

1. Allow GoNZB nodes to share release metadata without relying entirely on third-party indexer sites.
2. Make self-hosted GoNZB instances collaborative without exposing local user accounts or user behavior to remote nodes.
3. Support private, approved federation pools.
4. Support signed release metadata, signed health observations, and signed trust/membership/moderation events.
5. Support on-demand resolution of missing NZBs or NZB-equivalent manifests from trusted peers.
6. Feed the existing GoNZB aggregator and Newznab-compatible API as another source.
7. Maintain compatibility with existing *Arr and NZB downloader workflows.
8. Keep remote data quarantined until validated.
9. Provide auditable event logs and deterministic event IDs.
10. Be implementable incrementally inside the current modular monolith.

### 2.2 Non-Goals

GoNZBNet must not:

1. Transmit Usenet article payloads, video files, binaries, or downloaded content between nodes.
2. Become a BitTorrent payload network.
3. Replace NNTP download providers.
4. Require users to log into remote nodes.
5. Send local user API keys, usernames, search history, grab history, or download history to remote nodes.
6. Use blockchain in v1.
7. Gossip full NZBs or full segment manifests by default.
8. Depend on live federated search for normal user queries.
9. Require a microservice deployment for v1.
10. Trust remote data just because it is signed. Signatures prove origin, not quality.

---

## 3. Terminology

```text
Node
  A GoNZB instance participating in GoNZBNet federation.

Home node
  The GoNZB instance where a user has a local account/API key.

Remote node
  Any federated GoNZBNet peer.

Release card
  Small signed searchable metadata about a release. It is suitable for gossip/federation.

Resolution manifest
  Full NZB-equivalent metadata needed to generate an NZB, including files, segments, groups, and Message-IDs. It is fetched on demand.

Manifest source
  A node that advertises or can provide a resolution manifest.

Health attestation
  Signed observation that a release/manifest is complete, incomplete, repairable, unavailable, etc.

Trust pool
  An approved community of nodes that accept contributions according to pool policy.

Pool member
  A node approved for a trust pool.

Pool admin
  A node authorized by pool policy to approve/revoke members or moderate objects.

Witness
  A node authorized to sign pool checkpoints or approval events.

Tombstone
  A signed moderation/revocation event hiding or rejecting a release, manifest, node, or prior event.

Event
  The signed append-only primitive used for all federation state changes.
```

---

## 4. High-Level Architecture

### 4.1 System Shape

GoNZBNet should be a new module family inside the existing GoNZB modular monolith.

```text
GoNZB indexer module
  └── emits local ReleaseCandidate records from header scans/indexer cache

GoNZBNet publisher
  └── converts local indexed releases into signed ReleaseCard events

GoNZBNet federation module
  ├── identity manager
  ├── signed event manager
  ├── peer manager
  ├── inbox/outbox transport
  ├── trust pool manager
  ├── reputation engine
  ├── event validation pipeline
  ├── release projection worker
  ├── manifest resolver
  ├── health attestation worker
  └── moderation/tombstone manager

GoNZB aggregator module
  └── treats accepted federated release cards as another source

GoNZB Newznab-compatible API
  ├── search returns local + accepted federated results
  └── get resolves manifests and generates NZBs locally
```

### 4.2 Data Plane vs Control Plane

Usenet remains the data plane. GoNZBNet is the control/discovery plane.

```text
NNTP / Usenet provider
  Stores and serves Usenet articles.

GoNZBNet
  Shares metadata, trust signals, health signals, and manifests.

NZB generation
  Happens at the user's home node for compatibility with existing clients.
```

GoNZBNet must not upload or proxy Usenet article bodies between peers.

### 4.3 Discovery vs Resolution

Do not gossip full NZBs or full manifests by default.

```text
ReleaseCard
  Small. Searchable. Safe to sync widely within approved pools.

ResolutionManifest
  Large. Contains Message-IDs. Fetch only on demand from trusted peers.
```

This keeps federation cheap, reduces leakage, and avoids turning GoNZBNet into a giant distributed NZB dump.

---

## 5. Deployment Model

### 5.1 v1: Modular Monolith

Keep v1 in the modular monolith.

Reasons:

1. GoNZBNet needs tight access to the local indexer cache.
2. It must integrate with the existing aggregator and Newznab API.
3. It must reuse existing RBAC and API-key auth.
4. It must share the PostgreSQL database and migrations.
5. Self-hosted deployment should remain simple.
6. Splitting too early would create distributed transactions, duplicated auth, internal API versioning, and more deployment burden.

### 5.2 Future: Optional Relay Mode

Design modules so a future `gonzbnet-relay` process can be extracted.

Relay mode should eventually handle:

```text
public federation inbox/outbox
WebSocket gossip
peer exchange
large peer lists
pool checkpoints
rate-limited manifest serving
```

The main GoNZB app should still own:

```text
local users
RBAC
indexer cache
aggregator
Newznab API
local manifest cache
NZB generation
```

Do not implement relay mode first unless explicitly requested. Implement internal interfaces so relay mode can be added later.

---

## 6. Module Layout

Codex should create a module layout similar to this. Adjust naming to match the existing repository style, but preserve responsibilities.

```text
/internal/gonzbnet
  /identity
    node key creation, storage, loading, rotation helpers

  /canonical
    RFC 8785 JCS canonical JSON implementation/wrapper
    hash helpers
    base32/base64url helpers

  /events
    event envelope structs
    signing and verification
    event ID computation
    schema registry
    event repository

  /schemas
    JSON schema or typed validators for each event body
    version compatibility rules

  /transport/http
    HTTP handlers for node profile, caps, inbox, outbox, event fetch, manifest fetch
    node-to-node request signing
    replay protection

  /transport/ws
    optional later WebSocket gossip transport

  /peers
    manual peer config
    peer profiles
    peer cursors
    backoff/retry
    peer status

  /pools
    PoolGenesis, membership, M-of-N approval, revocation, role checks

  /trust
    node trust score
    manifest confidence score
    availability score
    policy score
    reputation events

  /publisher
    converts local indexer cache records to ReleaseCard events

  /projection
    validates accepted events and projects them into queryable tables

  /resolver
    manifest source selection
    node-to-node manifest requests
    manifest validation
    NZB generation integration

  /health
    local availability checks
    health attestation creation
    health aggregation

  /moderation
    tombstones
    local blocklists
    pool moderation quorum

  /aggregator
    FederatedAggregatorSource implementation
    result mapping to existing aggregator models

  /rbac
    local user/role to federation-pool permission mapping

  /admin
    admin service methods used by the UI/API
```

Recommended interfaces:

```go
// Names are illustrative. Match project conventions.

type IdentityService interface {
    LocalNodeID(ctx context.Context) (string, error)
    PublicKey(ctx context.Context) ([]byte, error)
    Sign(ctx context.Context, payload []byte) ([]byte, error)
    Verify(ctx context.Context, nodeID string, payload []byte, sig []byte) error
}

type EventService interface {
    CreateEvent(ctx context.Context, eventType string, pools []string, body any) (*SignedEvent, error)
    VerifyEvent(ctx context.Context, event *SignedEvent) (*ValidationResult, error)
    StoreEvent(ctx context.Context, event *SignedEvent, result *ValidationResult) error
}

type PoolAuthorizer interface {
    CanAcceptEvent(ctx context.Context, nodeID string, poolID string, eventType string) (bool, string, error)
    CanFetchManifest(ctx context.Context, requesterNodeID string, poolID string, manifestID string) (bool, string, error)
}

type ManifestResolver interface {
    ResolveManifest(ctx context.Context, userID string, releaseID string) (*ResolutionManifest, error)
}

type FederatedAggregatorSource interface {
    Search(ctx context.Context, request AggregatorSearchRequest, user UserContext) ([]AggregatorResult, error)
    Get(ctx context.Context, releaseID string, user UserContext) (*NZBResponse, error)
}
```

---

## 7. Encoding, Storage, and Signing Decisions

### 7.1 Wire Encoding

Use canonical JSON for v1.

Reasons:

1. Easy for Codex and maintainers to debug.
2. Easy to store as JSONB projections in Postgres.
3. Easy to inspect in logs and admin UI.
4. Good enough for v1 payload sizes because full manifests are not gossiped by default.
5. RFC 8785 JCS gives deterministic bytes for hashing/signing.

Use CBOR only as a future optional optimization.

### 7.2 PostgreSQL Storage Rule

PostgreSQL `jsonb` is appropriate for querying event bodies and release metadata, but it must not be used as the canonical signature source.

Store both:

```text
canonical_event_json TEXT or BYTEA
  Exact canonical JCS string/bytes that were signed and hashed.

body_jsonb JSONB
  Parsed queryable projection for filtering, indexing, admin UI, and validation display.
```

Why this matters:

```text
jsonb can reorder keys, remove whitespace, normalize some values, and discard duplicate keys.
Canonical signatures require deterministic exact bytes.
Therefore signatures must be verified against stored canonical bytes, not jsonb reserialization.
```

### 7.3 Event Canonicalization

Use RFC 8785 JCS for:

```text
body_hash computation
unsigned event canonical payload
event_id computation
signature verification
manifest_id computation
pool checkpoint leaf hashes
```

All JSON objects used for signing must obey these rules:

1. UTF-8 only.
2. No duplicate object keys.
3. No NaN or Infinity.
4. Large integers that could exceed safe JSON number handling should be encoded as strings, except fields explicitly defined as integer and known to fit.
5. Object keys sorted by JCS rules.
6. No insignificant whitespace in canonical bytes.
7. Timestamps are RFC3339 UTC strings with `Z` suffix.

### 7.4 Hashing

Use SHA-256 for v1 unless the project already standardizes on BLAKE3.

Recommended IDs:

```text
node_id     = node_<base32nopad(sha256(raw_ed25519_public_key))>
event_id    = evt_<base32nopad(sha256(JCS(unsigned_event)))>
body_hash   = sha256:<base64urlnopad(sha256(JCS(body)))>
release_id  = rel_<base32nopad(sha256(JCS(release_identity_core)))>
manifest_id = man_<base32nopad(sha256(JCS(manifest_core)))>
pool_id     = pool.<dns-or-slug-style-id>
```

Use lower-case prefixes consistently.

### 7.5 Signatures

Use Ed25519 signatures over the canonical unsigned event payload.

```text
signature = ed25519_sign(node_private_key, JCS(unsigned_event))
```

`unsigned_event` means the event envelope without `event_id` and without `signature`.

Validation:

```text
1. Remove event_id and signature from received event.
2. Canonicalize the unsigned event with JCS.
3. Compute sha256 of canonical unsigned event.
4. Confirm it matches event_id.
5. Verify Ed25519 signature against canonical unsigned event.
6. Confirm author_node_id matches the hash of the public key known for that node.
```

---

## 8. Identity Model

### 8.1 Local User Identity

Existing GoNZB users remain local to their home node.

Local user identity includes:

```text
user account
roles
API keys
category permissions
Newznab API access
rate limits
admin permissions
federation-pool access
```

A local user's API key must only work on their home node.

### 8.2 Federated Node Identity

GoNZBNet nodes authenticate to each other with Ed25519 keys.

Node identity includes:

```text
node_id
public key
node profile
advertised endpoints
capabilities
pool memberships
local trust score
remote reputation observations
```

Remote nodes trust or distrust a node. They do not know or authenticate that node's local users.

### 8.3 Lemmy/Fediverse-Style Home Node Behavior

Implement v1 like a fediverse home-instance model:

```text
A user logs into Node A.
Node A federates with Node B/C/D.
The user searches Node A.
Node A returns accepted local and federated results.
The user grabs from Node A.
Node A resolves any missing manifest from trusted peers.
Node A returns the NZB to the user.
```

Do not implement this in v1:

```text
User from Node A points Sonarr directly at Node B.
Node B accepts Node A's user API key.
```

Cross-node user login is a separate future feature and should not be included in GoNZBNet v1.

---

## 9. Authentication and Authorization

### 9.1 Local User Authentication

Keep existing GoNZB auth and RBAC for:

```text
web UI login
local API keys
Newznab API keys
admin routes
local rate limits
```

The existing Newznab endpoint remains the user-facing API.

### 9.2 Node-to-Node Authentication

Every node-to-node request that mutates state or fetches protected data must be signed.

Use this header shape:

```http
Authorization: GoNZBNet node_id="node_abc",timestamp="2026-07-07T12:00:00Z",nonce="base64url",signature="base64url"
X-GoNZBNet-Version: 1
Content-Type: application/gonzbnet+json
```

The signature covers this canonical request object:

```json
{
  "method": "POST",
  "path": "/gonzbnet/v1/inbox",
  "query_hash": "sha256:<hash-of-canonical-query-or-empty>",
  "body_hash": "sha256:<hash-of-raw-request-body-or-empty>",
  "timestamp": "2026-07-07T12:00:00Z",
  "nonce": "base64url-random-128-bit",
  "node_id": "node_abc"
}
```

Validation:

```text
1. Parse node_id, timestamp, nonce, signature.
2. Reject if timestamp outside tolerance.
3. Reject if nonce was already used by node_id within replay window.
4. Fetch known public key for node_id.
5. Canonicalize the request signing object.
6. Verify signature.
7. Store nonce until expiration.
8. Apply pool and endpoint authorization.
```

Default tolerances:

```text
timestamp tolerance: 120 seconds
nonce replay cache TTL: 10 minutes
minimum nonce entropy: 128 bits
```

### 9.3 Local RBAC Additions

Add these permissions:

```text
gonzbnet.search
  User can see federated release-card results.

gonzbnet.get
  User can grab/download an NZB generated from federated data.

gonzbnet.resolve_manifest
  User can trigger on-demand remote manifest resolution.

gonzbnet.view_trust_score
  User can see trust/health details in UI.

gonzbnet.view_source_node
  User can see source-node details in UI.

gonzbnet.admin.peers
  Admin can add/remove peers and view peer status.

gonzbnet.admin.pools
  Admin can create/join/manage trust pools.

gonzbnet.admin.moderation
  Admin can issue tombstones and manage local blocklists.

gonzbnet.admin.keys
  Admin can view/rotate node identity.
```

Add pool-level entitlements:

```text
user/role -> pool -> can_search/can_get/can_resolve_manifest
```

A user can see federated results only if:

```text
user has gonzbnet.search
AND result.pool_id is in user's allowed pools
AND result is not tombstoned
AND result meets minimum trust score
AND result is resolvable when serving Newznab clients
```

A user can grab a federated result only if:

```text
user has gonzbnet.get
AND user has access to the result's pool
AND user has gonzbnet.resolve_manifest if manifest is not cached locally
AND release is not tombstoned
AND manifest can be resolved locally or from trusted peers
```

### 9.4 Node Authorization

Remote nodes authorize the requesting node, not the local user.

For manifest fetches, the remote node checks:

```text
requesting node signature is valid
requesting node is an active member of the target pool
requesting node has manifest_fetch capability for that pool
manifest belongs to that pool
requesting node is not revoked or locally blocked
request is within rate limits
```

Remote nodes must never receive:

```text
local username
local user ID
local API key
local search query
local grab history
local download history
local downloader identity
```

---

## 10. Core Data Objects

All event bodies should include a `schema_version`. Event envelope has `spec_version`.

### 10.1 SignedEvent Envelope

Every federated state change uses this envelope.

```json
{
  "spec_version": "gonzbnet/1.0",
  "event_id": "evt_...",
  "event_type": "ReleaseCard",
  "author_node_id": "node_...",
  "author_public_key": "base64url-raw-ed25519-public-key",
  "sequence": 18291,
  "previous_event_id": "evt_...",
  "created_at": "2026-07-07T12:00:00Z",
  "not_before": null,
  "expires_at": "2026-10-05T12:00:00Z",
  "pool_ids": ["pool.private.movies"],
  "visibility": "pool",
  "body_schema": "gonzbnet.ReleaseCard/1.0",
  "body_hash": "sha256:...",
  "body": {},
  "signature_alg": "Ed25519",
  "signature": "base64url..."
}
```

Rules:

```text
event_id is derived from JCS(unsigned_event), where unsigned_event excludes event_id and signature.
signature is over JCS(unsigned_event).
body_hash is over JCS(body).
sequence is monotonically increasing per author node.
previous_event_id is the author's prior event ID, or null for the first event.
pool_ids controls which trust pools this event belongs to.
visibility is one of: public, private, pool, direct.
```

### 10.2 NodeProfile

Advertised via `GET /gonzbnet/v1/node` and optionally as a signed event.

```json
{
  "schema_version": "1.0",
  "type": "NodeProfile",
  "node_id": "node_...",
  "alias": "example-node",
  "software": "GoNZB",
  "software_version": "0.8.0",
  "protocols": ["gonzbnet/1.0"],
  "public_key": "base64url-raw-ed25519-public-key",
  "endpoints": {
    "base": "https://node.example.com/gonzbnet/v1",
    "inbox": "https://node.example.com/gonzbnet/v1/inbox",
    "outbox": "https://node.example.com/gonzbnet/v1/outbox",
    "events": "https://node.example.com/gonzbnet/v1/events/{event_id}",
    "manifests": "https://node.example.com/gonzbnet/v1/manifests/{manifest_id}",
    "ws": "wss://node.example.com/gonzbnet/v1/ws"
  },
  "capabilities": {
    "release_cards": true,
    "resolution_manifests": true,
    "health_attestations": true,
    "trust_pools": true,
    "pool_witness": true,
    "websocket_gossip": false,
    "peer_exchange": false,
    "relay_mode": false
  },
  "limits": {
    "max_event_bytes": 262144,
    "max_manifest_bytes": 10485760,
    "max_batch_events": 100,
    "rate_limit_events_per_minute": 120
  },
  "policy": {
    "private_network": true,
    "live_query_supported": false,
    "manifest_fetch_requires_pool_membership": true
  },
  "created_at": "2026-07-07T12:00:00Z",
  "updated_at": "2026-07-07T12:00:00Z"
}
```

Share in node profile:

```text
node ID
public key
software and version
federation endpoints
capabilities
public policy
rate limits
```

Do not share:

```text
provider credentials
NNTP usernames
API keys
local users
local user roles
download history
search history
private indexer source credentials
```

### 10.3 ReleaseCard

A ReleaseCard is the small searchable unit replicated across trusted nodes.

```json
{
  "schema_version": "1.0",
  "type": "ReleaseCard",
  "release_id": "rel_...",
  "manifest_id": "man_...",
  "title": "Example.Release.Name.2026.2160p.WEB-DL",
  "normalized_title": "example release name 2026 2160p web dl",
  "category": ["movies", "uhd"],
  "newznab_categories": [2000, 2040],
  "size_bytes": 12345678900,
  "posted_at": "2026-07-07T10:55:00Z",
  "groups": ["alt.binaries.example"],
  "file_count": 94,
  "segment_count": 1200,
  "poster_hash": "sha256:...",
  "subject_fingerprint": "sha256:...",
  "file_fingerprint": "sha256:...",
  "nzb_guid": null,
  "media": {
    "imdb_id": "tt1234567",
    "tmdb_id": 12345,
    "tvdb_id": null,
    "season": null,
    "episode": null,
    "year": 2026
  },
  "quality": {
    "resolution": "2160p",
    "source": "WEB-DL",
    "codec": "HEVC",
    "audio": "Atmos"
  },
  "flags": {
    "passworded": "unknown",
    "encrypted_names": false,
    "contains_executable": "unknown",
    "requires_repair": "unknown",
    "obfuscated_subjects": true
  },
  "resolution": {
    "status": "remote_manifest_available",
    "fetch_policy": "trusted_peers_only",
    "compressed_size_bytes": 81234,
    "manifest_sources": ["node_..."]
  },
  "source": {
    "kind": "local_header_scan",
    "confidence": 0.86,
    "indexer_name_hash": null
  },
  "expires_at": "2026-10-05T12:00:00Z"
}
```

ReleaseCard requirements:

```text
Must be small enough to gossip/pull in batches.
Must be sufficient for search, ranking, category filtering, and Newznab result mapping.
Must not require full segment list.
Must not include local user data.
Must not include provider credentials.
May include manifest_id if a manifest exists.
May include manifest availability sources.
```

Release ID identity core:

```json
{
  "normalized_title": "example release name 2026 2160p web dl",
  "size_bytes": 12345678900,
  "posted_at_day": "2026-07-07",
  "groups": ["alt.binaries.example"],
  "file_count": 94,
  "segment_count": 1200,
  "subject_fingerprint": "sha256:...",
  "file_fingerprint": "sha256:..."
}
```

Sort arrays before canonicalization where order is not meaningful. `posted_at_day` is used instead of precise timestamp for release ID stability, while the original `posted_at` is still stored.

### 10.4 ResolutionManifest

The ResolutionManifest is the NZB-equivalent object. It should be fetched on demand only.

Wire body:

```json
{
  "schema_version": "1.0",
  "type": "ResolutionManifest",
  "manifest_id": "man_...",
  "release_id": "rel_...",
  "manifest_core": {
    "groups": ["alt.binaries.example"],
    "poster": "poster@example.invalid",
    "posted_at": "2026-07-07T10:55:00Z",
    "files": [
      {
        "name": "example.part001.rar",
        "subject": "Example.Release.Name part001.rar yEnc",
        "date": "2026-07-07T10:56:00Z",
        "size_bytes": 104857600,
        "segments": [
          {
            "number": 1,
            "bytes": 739284,
            "message_id": "<abc123@example.invalid>"
          }
        ]
      }
    ],
    "par2": {
      "present": true,
      "base_files": 1,
      "volume_files": 12
    },
    "hashes": {
      "file_list_hash": "sha256:...",
      "segment_list_hash": "sha256:..."
    },
    "nzb": {
      "generator": "GoNZBNet",
      "xml_charset": "utf-8"
    }
  },
  "compression": "zstd",
  "encrypted": false
}
```

Manifest ID rules:

```text
manifest_id = man_<base32nopad(sha256(JCS(manifest_core)))>
```

Validation must recompute `manifest_id` from `manifest_core`.

Storage requirements:

```text
Store compressed canonical manifest bytes for exact integrity.
Store queryable projection in jsonb if useful.
Store generated NZB separately only after successful validation/generation.
```

Manifest fetch policy:

```text
Do not gossip ResolutionManifest bodies by default.
Return manifests only to trusted active pool members.
Use node-signed ManifestRequest.
Cache successful manifests locally.
```

### 10.5 ManifestAvailability

This may be embedded in ReleaseCard or sent as a separate event.

```json
{
  "schema_version": "1.0",
  "type": "ManifestAvailability",
  "manifest_id": "man_...",
  "release_id": "rel_...",
  "source_node_id": "node_...",
  "pool_id": "pool.private.movies",
  "available": true,
  "fetch_policy": "trusted_peers_only",
  "compressed_size_bytes": 81234,
  "updated_at": "2026-07-07T12:00:00Z"
}
```

For v1, embedding this in ReleaseCard is acceptable. Implement a table that can support multiple manifest sources.

### 10.6 HealthAttestation

Signed observation about availability/completeness.

```json
{
  "schema_version": "1.0",
  "type": "HealthAttestation",
  "release_id": "rel_...",
  "manifest_id": "man_...",
  "checked_at": "2026-07-07T13:00:00Z",
  "status": "complete",
  "articles_total": 1200,
  "articles_available": 1200,
  "missing_articles": 0,
  "repair_available": true,
  "repair_confidence": 0.9,
  "provider_scope": {
    "provider_backbone_hash": null,
    "retention_days_observed": 5400
  },
  "confidence": 0.94,
  "method": "article_stat_sampled"
}
```

Allowed `status` values:

```text
unknown
complete
incomplete
missing
repairable
unverified
provider_limited
```

Default privacy:

```text
provider_backbone_hash should be null unless the admin explicitly opts in.
Never share provider account identity or server credentials.
```

### 10.7 TrustAttestation

TrustAttestation is a signed signal. It is not automatically final truth.

```json
{
  "schema_version": "1.0",
  "type": "TrustAttestation",
  "subject_node_id": "node_...",
  "pool_id": "pool.private.movies",
  "score_delta": 10,
  "reason": "valid_contributions",
  "evidence": {
    "valid_manifest_count": 50,
    "bad_manifest_count": 0,
    "event_ids": ["evt_..."]
  },
  "expires_at": "2026-08-07T12:00:00Z"
}
```

Each node computes local trust independently. TrustAttestation is only one input.

### 10.8 PoolGenesis

Creates a trust pool.

```json
{
  "schema_version": "1.0",
  "type": "PoolGenesis",
  "pool_id": "pool.private.movies",
  "display_name": "Private Movies Pool",
  "description": "Private approved GoNZB federation pool",
  "created_at": "2026-07-07T12:00:00Z",
  "admins": ["node_admin1", "node_admin2", "node_admin3"],
  "witnesses": ["node_admin1", "node_admin2"],
  "policy": {
    "membership_threshold": 2,
    "moderation_threshold": 2,
    "checkpoint_witness_threshold": 2,
    "manifest_quorum": 1,
    "health_quorum": 1,
    "accept_mode": "pool_member",
    "min_node_trust_score": 0.35,
    "min_result_score": 0.50,
    "max_release_card_age_days": 30,
    "allow_manifest_fetch": true,
    "manifest_fetch_requires_membership": true,
    "allow_live_query": false,
    "share_resolution_manifests": "trusted_only",
    "allow_encrypted_manifests": true
  }
}
```

### 10.9 PoolJoinRequest

```json
{
  "schema_version": "1.0",
  "type": "PoolJoinRequest",
  "pool_id": "pool.private.movies",
  "candidate_node_id": "node_...",
  "candidate_profile_event_id": "evt_...",
  "requested_roles": ["member"],
  "message": "optional admin-visible message",
  "created_at": "2026-07-07T12:00:00Z"
}
```

### 10.10 PoolMemberApproved

```json
{
  "schema_version": "1.0",
  "type": "PoolMemberApproved",
  "pool_id": "pool.private.movies",
  "subject_node_id": "node_...",
  "role": "member",
  "proposal_event_id": "evt_join_request",
  "approvals_required": 2,
  "approvals": [
    {
      "node_id": "node_admin1",
      "approved_at": "2026-07-07T12:05:00Z",
      "signature": "base64url..."
    },
    {
      "node_id": "node_admin2",
      "approved_at": "2026-07-07T12:06:00Z",
      "signature": "base64url..."
    }
  ]
}
```

Each approval signature should sign a canonical approval object:

```json
{
  "pool_id": "pool.private.movies",
  "proposal_event_id": "evt_join_request",
  "subject_node_id": "node_...",
  "role": "member",
  "approved_at": "2026-07-07T12:05:00Z"
}
```

### 10.11 PoolMemberRevoked

```json
{
  "schema_version": "1.0",
  "type": "PoolMemberRevoked",
  "pool_id": "pool.private.movies",
  "subject_node_id": "node_...",
  "reason": "compromised_key",
  "effective_at": "2026-07-07T13:00:00Z",
  "approvals_required": 2,
  "approvals": [
    {
      "node_id": "node_admin1",
      "signature": "base64url..."
    },
    {
      "node_id": "node_admin2",
      "signature": "base64url..."
    }
  ]
}
```

### 10.12 PoolCheckpoint

Checkpoint event for tamper-evident pool history.

```json
{
  "schema_version": "1.0",
  "type": "PoolCheckpoint",
  "pool_id": "pool.private.movies",
  "height": 12991,
  "event_count": 12991,
  "from_event_id": "evt_...",
  "to_event_id": "evt_...",
  "merkle_root": "sha256:...",
  "created_at": "2026-07-07T12:30:00Z",
  "witnesses": [
    {
      "node_id": "node_admin1",
      "signature": "base64url..."
    },
    {
      "node_id": "node_admin2",
      "signature": "base64url..."
    }
  ]
}
```

### 10.13 Tombstone

```json
{
  "schema_version": "1.0",
  "type": "Tombstone",
  "target_type": "release",
  "target_id": "rel_...",
  "pool_id": "pool.private.movies",
  "reason": "malformed_manifest",
  "severity": "reject",
  "evidence_event_ids": ["evt_..."],
  "effective_at": "2026-07-07T12:00:00Z",
  "expires_at": null
}
```

Allowed `target_type` values:

```text
release
manifest
event
node
pool_member
health_attestation
trust_attestation
```

Allowed `severity` values:

```text
hide
reject
warn
local_only
```

---

## 11. What Data to Share

### 11.1 Share by Default Within Approved Pools

```text
NodeProfile
ReleaseCard
ManifestAvailability metadata
HealthAttestation
TrustAttestation
PoolGenesis
PoolJoinRequest
PoolMemberApproved
PoolMemberRevoked
PoolCheckpoint
Tombstone
```

### 11.2 Share Only On Demand

```text
ResolutionManifest
Generated NZB XML
```

Generated NZB XML does not need to be federated if the manifest is sufficient. Prefer sharing manifests and generating NZB locally.

### 11.3 Never Share

```text
Usenet article payloads
provider credentials
NNTP username/password
indexer API keys
GoNZB local user API keys
local usernames
local email addresses
local user IDs
local search history
local grab/download history
downloader client identity
source indexer credentials
private network secrets
raw IP addresses in signed events
```

### 11.4 Optional/Opt-In Share

```text
provider_backbone_hash
retention_days_observed
source indexer name hash
coarse geographic/latency hints
```

These should be disabled by default.

---

## 12. Federation Protocol

### 12.1 Transport Choice

v1 should use HTTPS request/response federation:

```text
manual peers
GET node profile
GET capabilities
POST inbox
GET outbox cursor sync
GET event by ID
POST manifest request
GET/POST manifest fetch
```

WebSocket gossip should be added later after pull-based federation works.

### 12.2 Endpoint Shape

Base path:

```text
/gonzbnet/v1
```

Endpoints:

```text
GET  /.well-known/gonzbnet
GET  /gonzbnet/v1/node
GET  /gonzbnet/v1/caps
POST /gonzbnet/v1/handshake
POST /gonzbnet/v1/inbox
GET  /gonzbnet/v1/outbox?since=<cursor>&pool=<pool_id>&limit=<n>&type=<event_type>
GET  /gonzbnet/v1/events/{event_id}
POST /gonzbnet/v1/events/batch
POST /gonzbnet/v1/manifests/{manifest_id}/request
GET  /gonzbnet/v1/manifests/{manifest_id}
GET  /gonzbnet/v1/pools/{pool_id}/checkpoint
GET  /gonzbnet/v1/pools/{pool_id}/members
GET  /gonzbnet/v1/peers
WS   /gonzbnet/v1/ws       # later, not v1 critical path
```

### 12.3 Well-Known Discovery

`GET /.well-known/gonzbnet`

Response:

```json
{
  "spec_version": "gonzbnet/1.0",
  "node_url": "https://node.example.com/gonzbnet/v1/node",
  "base_url": "https://node.example.com/gonzbnet/v1",
  "public_key": "base64url...",
  "node_id": "node_..."
}
```

### 12.4 Capabilities

`GET /gonzbnet/v1/caps`

Response:

```json
{
  "spec_versions": ["gonzbnet/1.0"],
  "event_types": [
    "NodeProfile",
    "ReleaseCard",
    "ResolutionManifest",
    "HealthAttestation",
    "PoolGenesis",
    "PoolJoinRequest",
    "PoolMemberApproved",
    "PoolMemberRevoked",
    "TrustAttestation",
    "Tombstone",
    "PoolCheckpoint"
  ],
  "encodings": ["jcs-json"],
  "compressions": ["none", "gzip", "zstd"],
  "transports": ["https"],
  "max_event_bytes": 262144,
  "max_manifest_bytes": 10485760
}
```

### 12.5 Handshake

Manual peer flow:

```text
1. Admin adds peer base URL.
2. Local node fetches /.well-known/gonzbnet.
3. Local node fetches /node and /caps.
4. Local node pins node_id -> public_key after admin approval or existing trust path.
5. Local node sends signed handshake.
6. Remote node verifies and replies with signed handshake response.
7. Peer status becomes connected or pending approval.
```

Request:

```json
{
  "schema_version": "1.0",
  "type": "HandshakeRequest",
  "node_id": "node_local",
  "public_key": "base64url...",
  "nonce": "base64url-random",
  "supported_versions": ["gonzbnet/1.0"],
  "requested_pools": ["pool.private.movies"],
  "created_at": "2026-07-07T12:00:00Z",
  "signature": "base64url-signature-over-request-without-signature"
}
```

Response:

```json
{
  "schema_version": "1.0",
  "type": "HandshakeResponse",
  "node_id": "node_remote",
  "nonce": "same-nonce",
  "accepted_versions": ["gonzbnet/1.0"],
  "status": "accepted",
  "created_at": "2026-07-07T12:00:01Z",
  "signature": "base64url-signature-over-response-without-signature"
}
```

### 12.6 Inbox

`POST /gonzbnet/v1/inbox`

Request body accepts one event or a batch:

```json
{
  "schema_version": "1.0",
  "type": "EventBatch",
  "events": [
    {
      "spec_version": "gonzbnet/1.0",
      "event_id": "evt_...",
      "event_type": "ReleaseCard",
      "author_node_id": "node_...",
      "body": {},
      "signature": "..."
    }
  ]
}
```

Response:

```json
{
  "accepted": ["evt_1"],
  "duplicate": ["evt_2"],
  "rejected": [
    {
      "event_id": "evt_3",
      "code": "invalid_signature",
      "message": "Signature verification failed"
    }
  ],
  "cursor": "opaque-remote-cursor"
}
```

### 12.7 Outbox

`GET /gonzbnet/v1/outbox?since=<cursor>&pool=<pool_id>&limit=100&type=ReleaseCard`

Response:

```json
{
  "schema_version": "1.0",
  "type": "OutboxPage",
  "events": [],
  "next_cursor": "opaque-cursor",
  "has_more": true
}
```

Rules:

```text
Cursor must be opaque.
Do not expose internal database IDs.
Apply pool visibility checks.
Limit max page size.
Return events in deterministic order, preferably created_at then event_id or sequence.
```

### 12.8 Manifest Request

`POST /gonzbnet/v1/manifests/{manifest_id}/request`

Signed request body:

```json
{
  "schema_version": "1.0",
  "type": "ManifestRequest",
  "request_id": "req_...",
  "manifest_id": "man_...",
  "release_id": "rel_...",
  "pool_id": "pool.private.movies",
  "requesting_node_id": "node_local",
  "reason": "user_get",
  "created_at": "2026-07-07T12:00:00Z"
}
```

Response:

```json
{
  "schema_version": "1.0",
  "type": "ManifestResponse",
  "request_id": "req_...",
  "status": "ok",
  "manifest_event": {
    "spec_version": "gonzbnet/1.0",
    "event_type": "ResolutionManifest",
    "body": {}
  }
}
```

Error examples:

```json
{
  "status": "error",
  "code": "not_pool_member",
  "message": "Requesting node is not authorized for this pool"
}
```

### 12.9 Status Codes

```text
200 OK                Read success
202 Accepted          Inbox accepted at least one event
207 Multi-Status      Mixed event-batch result, if implemented
400 Bad Request       Invalid JSON/schema
401 Unauthorized      Missing/invalid node request signature
403 Forbidden         Node not authorized for pool/resource
404 Not Found         Unknown event/manifest or not visible to requester
409 Conflict          Duplicate or sequence conflict
413 Payload Too Large Event/manifest too large
422 Unprocessable     Valid JSON but semantically invalid
429 Too Many Requests Rate-limited
500 Internal Error    Server error
503 Unavailable       Peer temporarily unavailable
```

---

## 13. Sync and Federation Flows

### 13.1 Manual Pull Sync v1

```text
1. Admin configures manual peer.
2. Peer profile and capabilities are fetched.
3. Node key is pinned or matched against known trust event.
4. Local node pulls remote outbox with a cursor.
5. Remote events are verified and stored in quarantine.
6. Accepted events are projected into federated cache tables.
7. Aggregator sees projected accepted release cards.
```

### 13.2 Push Sync v1.5

```text
1. Local node creates signed event.
2. Peer manager selects approved peers for relevant pools.
3. Local node POSTs event to peer inboxes.
4. Peer validates and responds accepted/duplicate/rejected.
5. Local node records delivery status.
```

### 13.3 WebSocket Gossip v2

Only after pull/push work.

```json
{
  "schema_version": "1.0",
  "type": "GossipBatch",
  "network_id": "private-network-id",
  "ttl": 4,
  "events": ["evt_1", "evt_2"],
  "want_missing": true
}
```

Rules:

```text
Deduplicate by event_id.
Decrement TTL on forward.
Never forward invalid events.
Never forward events outside pool visibility.
Apply fanout and rate limits.
```

### 13.4 Live Federated Query

Do not implement or enable live federated query by default.

Reason:

```text
Live query broadcasts user search interests to remote nodes.
The preferred model is decentralized cache sync, then local search.
```

A future opt-in setting may allow live query to selected trusted peers, with explicit privacy warnings.

---

## 14. Trust Model

### 14.1 Trust Is Local-First

A signature proves who sent data. It does not prove that the data is good.

Each node computes local trust based on:

```text
local admin allow/block lists
pool membership
signature validity
schema validity
historical contribution quality
manifest resolution success
health attestation accuracy
peer uptime/reliability
moderation/tombstone events
trust attestations from other trusted nodes
```

### 14.2 Separate Scores

Do not collapse all trust into one vague reputation number. Maintain at least these scores:

```text
node_trust_score
  How much this local node trusts the remote node.

manifest_confidence_score
  How likely the release card/manifest is structurally correct.

availability_score
  How likely the release is currently downloadable.

policy_score
  Whether the release is allowed by local and pool policy.
```

### 14.3 Ranking Formula

Initial formula:

```text
final_score =
  0.35 * node_trust_score
+ 0.25 * manifest_confidence_score
+ 0.25 * availability_score
+ 0.10 * quorum_score
+ 0.05 * freshness_score
- penalties
```

Clamp all scores to `[0.0, 1.0]` after normalization.

### 14.4 Hard Rejects

Reject remote data immediately for:

```text
invalid signature
event_id mismatch
body_hash mismatch
unknown required schema version
malformed JSON
payload too large
future timestamp beyond tolerance
expired event when policy requires current events
unknown author key for protected pool
node not active pool member
revoked node
local blocklist match
tombstone with reject severity
manifest_id mismatch
manifest with invalid Message-ID structure
```

### 14.5 Penalties

Suggested reputation penalties:

```text
invalid signature: hard reject + severe penalty
schema violation: hard reject + medium penalty
future timestamp: reject + small/medium penalty
manifest unavailable after repeated advertisement: -0.10
health false positive: -0.20
spam duplicate flood: -0.10
bad manifest discovered locally: -0.25
rate-limit violation: temporary throttle
```

### 14.6 Rewards

Suggested reputation rewards:

```text
valid signed event accepted: small reward
manifest resolved successfully: medium reward
locally verified complete: large reward
health attestation matched local check: medium reward
long-lived active pool member: small periodic reward
admin/witness role: policy-dependent trust floor, not infinite trust
```

### 14.7 Quorum Score

`quorum_score` should increase when multiple trusted nodes independently observe the same release/manifest.

Example:

```text
0 trusted sources: 0.0
1 trusted source: 0.4
2 trusted sources: 0.7
3+ trusted sources: 1.0
```

Pool policy may require `manifest_quorum >= 2` for high-trust pools.

---

## 15. Trust Pool Design

### 15.1 Pool Roles

Supported roles:

```text
owner
  Created the pool; can propose policy changes. May be equivalent to admin in v1.

admin
  Can approve/revoke members and approve moderation actions.

witness
  Can sign checkpoints and approval bundles.

moderator
  Can propose tombstones and moderation actions.

member
  Can contribute and receive release cards according to policy.

source
  Can contribute release cards/manifests but may not receive data if policy says so.

consumer
  Can receive data but cannot contribute release cards.
```

For v1, implement at least `admin`, `witness`, and `member`. Other roles can be represented in schema and enforced later.

### 15.2 Approval Thresholds

Every pool has policy thresholds:

```text
membership_threshold
  Number of admin approvals required to add a member.

moderation_threshold
  Number of admin/moderator approvals required for pool-wide tombstone.

checkpoint_witness_threshold
  Number of witness signatures required for checkpoint acceptance.
```

Default:

```text
if admin_count == 1: threshold = 1
if admin_count >= 2: threshold = 2
```

### 15.3 Join Flow

```text
1. Candidate node creates or sends NodeProfile.
2. Candidate sends PoolJoinRequest.
3. Pool admins review candidate.
4. M admins sign approval objects.
5. PoolMemberApproved event is created with approvals.
6. Nodes validate threshold and admin authority.
7. Candidate becomes active pool member.
8. Future candidate events for that pool may be accepted.
```

### 15.4 Revocation Flow

```text
1. Admin/moderator proposes revocation.
2. M authorized nodes sign PoolMemberRevoked.
3. PoolMemberRevoked is accepted into pool log.
4. Future events from revoked node are rejected for that pool.
5. Past events may remain stored but should be rescored or hidden based on policy.
```

### 15.5 Key Rotation

Preferred key rotation:

```text
RotateKeyEvent signed by old key and new key.
Pool membership transfers only if both signatures validate.
```

Lost key recovery:

```text
Pool admins may approve a new node identity.
Reputation should not automatically transfer unless pool policy explicitly allows it.
```

---

## 16. Append-Only Log and Checkpoints

### 16.1 Per-Node Event Chain

Each node maintains a signed event chain:

```text
author_node_id
sequence
previous_event_id
event_id
signature
```

Validation:

```text
sequence should increase by 1 for events from the same author.
previous_event_id should match the last known event from that author when available.
sequence gaps are allowed during partial sync but should be tracked.
conflicting events with same author_node_id and sequence indicate a fork.
```

Fork handling:

```text
store both events
mark node as forked/suspicious
penalize trust
surface in admin UI
require manual or pool policy resolution
```

### 16.2 Pool Checkpoints

A pool checkpoint commits to an ordered set of pool events via Merkle root.

Use leaf hash:

```text
leaf_hash = sha256(JCS({event_id, author_node_id, sequence, body_hash, created_at}))
```

Checkpoint validation:

```text
1. Fetch required event range if missing.
2. Compute leaf hashes in deterministic order.
3. Build Merkle root.
4. Compare checkpoint root.
5. Verify witness signatures.
6. Apply pool checkpoint_witness_threshold.
```

### 16.3 Why This Replaces Blockchain

GoNZBNet needs:

```text
identity
auditability
tamper evidence
multi-node approval
revocation
fork detection
local trust
```

It does not need:

```text
global consensus
tokens
proof-of-work
public immutable leakage
worldwide total ordering
```

Therefore use signed append-only logs plus pool checkpoints, not blockchain.

---

## 17. PostgreSQL Schema

Use exact table and column names if possible. Adjust only to match existing GoNZB conventions.

Enable useful extensions if the project allows them:

```sql
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS btree_gin;
```

### 17.1 Nodes

```sql
CREATE TABLE IF NOT EXISTS federation_nodes (
  node_id TEXT PRIMARY KEY,
  public_key BYTEA NOT NULL,
  alias TEXT,
  software TEXT,
  software_version TEXT,
  actor_url TEXT,
  base_url TEXT,
  inbox_url TEXT,
  outbox_url TEXT,
  ws_url TEXT,
  capabilities JSONB NOT NULL DEFAULT '{}'::jsonb,
  profile_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_seen_at TIMESTAMPTZ,
  last_verified_at TIMESTAMPTZ,
  local_trust_score DOUBLE PRECISION NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'unknown',
  blocked_reason TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_federation_nodes_status
  ON federation_nodes(status);
```

### 17.2 Peers

```sql
CREATE TABLE IF NOT EXISTS federation_peers (
  id BIGSERIAL PRIMARY KEY,
  node_id TEXT REFERENCES federation_nodes(node_id),
  peer_url TEXT NOT NULL UNIQUE,
  source TEXT NOT NULL DEFAULT 'manual',
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  pinned_public_key BYTEA,
  last_connected_at TIMESTAMPTZ,
  last_sync_at TIMESTAMPTZ,
  failure_count INTEGER NOT NULL DEFAULT 0,
  last_error TEXT,
  status TEXT NOT NULL DEFAULT 'pending',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS federation_peer_cursors (
  peer_id BIGINT NOT NULL REFERENCES federation_peers(id) ON DELETE CASCADE,
  pool_id TEXT,
  event_type TEXT,
  cursor TEXT,
  last_event_id TEXT,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY(peer_id, pool_id, event_type)
);
```

### 17.3 Replay Protection

```sql
CREATE TABLE IF NOT EXISTS federation_nonce_replay_cache (
  node_id TEXT NOT NULL,
  nonce TEXT NOT NULL,
  seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY(node_id, nonce)
);

CREATE INDEX IF NOT EXISTS idx_federation_nonce_expires
  ON federation_nonce_replay_cache(expires_at);
```

### 17.4 Events

```sql
CREATE TABLE IF NOT EXISTS federation_events (
  event_id TEXT PRIMARY KEY,
  spec_version TEXT NOT NULL,
  event_type TEXT NOT NULL,
  author_node_id TEXT NOT NULL,
  author_public_key BYTEA,
  sequence BIGINT,
  previous_event_id TEXT,
  body_schema TEXT NOT NULL,
  body_hash TEXT NOT NULL,
  signature_alg TEXT NOT NULL,
  signature BYTEA NOT NULL,
  canonical_event_json TEXT NOT NULL,
  body_json JSONB NOT NULL,
  pool_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
  visibility TEXT NOT NULL DEFAULT 'pool',
  created_at TIMESTAMPTZ NOT NULL,
  not_before TIMESTAMPTZ,
  expires_at TIMESTAMPTZ,
  received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  validation_status TEXT NOT NULL DEFAULT 'pending',
  rejection_reason TEXT,
  projected BOOLEAN NOT NULL DEFAULT FALSE,
  projected_at TIMESTAMPTZ,
  UNIQUE(author_node_id, sequence)
);

CREATE INDEX IF NOT EXISTS idx_federation_events_type_created
  ON federation_events(event_type, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_federation_events_author_sequence
  ON federation_events(author_node_id, sequence);

CREATE INDEX IF NOT EXISTS idx_federation_events_pool_ids_gin
  ON federation_events USING GIN(pool_ids);

CREATE INDEX IF NOT EXISTS idx_federation_events_body_gin
  ON federation_events USING GIN(body_json jsonb_path_ops);
```

If forks must be stored despite same `(author_node_id, sequence)`, remove the unique constraint and create a separate fork-detection table. For v1, the unique constraint is acceptable if rejected conflicts are stored in a dead-letter table.

```sql
CREATE TABLE IF NOT EXISTS federation_rejected_events (
  event_id TEXT,
  author_node_id TEXT,
  event_type TEXT,
  raw_event_json TEXT NOT NULL,
  rejection_reason TEXT NOT NULL,
  received_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### 17.5 Trust Pools

```sql
CREATE TABLE IF NOT EXISTS trust_pools (
  pool_id TEXT PRIMARY KEY,
  display_name TEXT NOT NULL,
  description TEXT,
  genesis_event_id TEXT NOT NULL REFERENCES federation_events(event_id),
  policy_json JSONB NOT NULL,
  membership_threshold INTEGER NOT NULL DEFAULT 1,
  moderation_threshold INTEGER NOT NULL DEFAULT 1,
  checkpoint_witness_threshold INTEGER NOT NULL DEFAULT 1,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  latest_checkpoint_event_id TEXT,
  latest_merkle_root TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS pool_members (
  pool_id TEXT NOT NULL REFERENCES trust_pools(pool_id) ON DELETE CASCADE,
  node_id TEXT NOT NULL REFERENCES federation_nodes(node_id),
  role TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'active',
  approved_event_id TEXT REFERENCES federation_events(event_id),
  revoked_event_id TEXT REFERENCES federation_events(event_id),
  joined_at TIMESTAMPTZ,
  revoked_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY(pool_id, node_id, role)
);

CREATE INDEX IF NOT EXISTS idx_pool_members_node
  ON pool_members(node_id, status);
```

### 17.6 Release Cards and Sources

```sql
CREATE TABLE IF NOT EXISTS federated_release_cards (
  release_id TEXT PRIMARY KEY,
  manifest_id TEXT,
  title TEXT NOT NULL,
  normalized_title TEXT NOT NULL,
  category_json JSONB NOT NULL DEFAULT '[]'::jsonb,
  newznab_categories JSONB NOT NULL DEFAULT '[]'::jsonb,
  size_bytes BIGINT,
  posted_at TIMESTAMPTZ,
  groups_json JSONB NOT NULL DEFAULT '[]'::jsonb,
  file_count INTEGER,
  segment_count INTEGER,
  poster_hash TEXT,
  subject_fingerprint TEXT,
  file_fingerprint TEXT,
  media_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  quality_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  flags_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  resolution_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  best_score DOUBLE PRECISION NOT NULL DEFAULT 0,
  availability_score DOUBLE PRECISION NOT NULL DEFAULT 0,
  manifest_confidence_score DOUBLE PRECISION NOT NULL DEFAULT 0,
  trust_score DOUBLE PRECISION NOT NULL DEFAULT 0,
  resolvable BOOLEAN NOT NULL DEFAULT FALSE,
  status TEXT NOT NULL DEFAULT 'candidate',
  first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_federated_release_cards_title_trgm
  ON federated_release_cards USING GIN(normalized_title gin_trgm_ops);

CREATE INDEX IF NOT EXISTS idx_federated_release_cards_posted
  ON federated_release_cards(posted_at DESC);

CREATE INDEX IF NOT EXISTS idx_federated_release_cards_resolvable_score
  ON federated_release_cards(resolvable, best_score DESC);

CREATE TABLE IF NOT EXISTS federated_release_sources (
  release_id TEXT NOT NULL REFERENCES federated_release_cards(release_id) ON DELETE CASCADE,
  manifest_id TEXT,
  source_node_id TEXT NOT NULL REFERENCES federation_nodes(node_id),
  source_event_id TEXT NOT NULL REFERENCES federation_events(event_id),
  pool_id TEXT NOT NULL,
  trust_score DOUBLE PRECISION NOT NULL DEFAULT 0,
  availability_score DOUBLE PRECISION NOT NULL DEFAULT 0,
  manifest_confidence_score DOUBLE PRECISION NOT NULL DEFAULT 0,
  resolvable BOOLEAN NOT NULL DEFAULT FALSE,
  first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY(release_id, source_node_id, pool_id)
);

CREATE INDEX IF NOT EXISTS idx_federated_release_sources_manifest
  ON federated_release_sources(manifest_id);

CREATE INDEX IF NOT EXISTS idx_federated_release_sources_pool
  ON federated_release_sources(pool_id, resolvable);
```

### 17.7 Manifests

```sql
CREATE TABLE IF NOT EXISTS resolution_manifests (
  manifest_id TEXT PRIMARY KEY,
  release_id TEXT NOT NULL,
  source_node_id TEXT REFERENCES federation_nodes(node_id),
  source_event_id TEXT REFERENCES federation_events(event_id),
  encoding TEXT NOT NULL DEFAULT 'jcs-json',
  compression TEXT,
  encrypted BOOLEAN NOT NULL DEFAULT FALSE,
  canonical_manifest_json TEXT,
  body_json JSONB,
  body_blob BYTEA,
  nzb_sha256 TEXT,
  generated_nzb BYTEA,
  fetched_at TIMESTAMPTZ,
  verified_at TIMESTAMPTZ,
  validation_status TEXT NOT NULL DEFAULT 'unknown',
  rejection_reason TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_resolution_manifests_release
  ON resolution_manifests(release_id);

CREATE TABLE IF NOT EXISTS federated_manifest_sources (
  manifest_id TEXT NOT NULL,
  release_id TEXT,
  source_node_id TEXT NOT NULL REFERENCES federation_nodes(node_id),
  pool_id TEXT NOT NULL,
  advertised BOOLEAN NOT NULL DEFAULT TRUE,
  last_success_at TIMESTAMPTZ,
  last_failure_at TIMESTAMPTZ,
  failure_count INTEGER NOT NULL DEFAULT 0,
  avg_latency_ms INTEGER,
  trust_score DOUBLE PRECISION NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY(manifest_id, source_node_id, pool_id)
);
```

### 17.8 Health and Reputation

```sql
CREATE TABLE IF NOT EXISTS health_attestations (
  attestation_id TEXT PRIMARY KEY,
  manifest_id TEXT,
  release_id TEXT NOT NULL,
  author_node_id TEXT NOT NULL REFERENCES federation_nodes(node_id),
  pool_id TEXT,
  checked_at TIMESTAMPTZ NOT NULL,
  status TEXT NOT NULL,
  articles_total INTEGER,
  articles_available INTEGER,
  missing_articles INTEGER,
  repair_available BOOLEAN,
  provider_backbone_hash TEXT,
  retention_days_observed INTEGER,
  confidence DOUBLE PRECISION NOT NULL DEFAULT 0,
  method TEXT,
  source_event_id TEXT NOT NULL REFERENCES federation_events(event_id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_health_attestations_release
  ON health_attestations(release_id, checked_at DESC);

CREATE INDEX IF NOT EXISTS idx_health_attestations_manifest
  ON health_attestations(manifest_id, checked_at DESC);

CREATE TABLE IF NOT EXISTS reputation_events (
  id BIGSERIAL PRIMARY KEY,
  node_id TEXT NOT NULL REFERENCES federation_nodes(node_id),
  pool_id TEXT,
  event_id TEXT REFERENCES federation_events(event_id),
  delta DOUBLE PRECISION NOT NULL,
  reason TEXT NOT NULL,
  evidence_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_reputation_events_node
  ON reputation_events(node_id, created_at DESC);
```

### 17.9 Tombstones

```sql
CREATE TABLE IF NOT EXISTS tombstones (
  id BIGSERIAL PRIMARY KEY,
  target_type TEXT NOT NULL,
  target_id TEXT NOT NULL,
  pool_id TEXT,
  reason TEXT NOT NULL,
  severity TEXT NOT NULL,
  source_event_id TEXT NOT NULL REFERENCES federation_events(event_id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_tombstones_unique_target
  ON tombstones(target_type, target_id, COALESCE(pool_id, '__local__'));
```

### 17.10 RBAC Pool Access

Prefer role-based access if the existing RBAC is role-centric. Support user overrides if needed.

```sql
CREATE TABLE IF NOT EXISTS role_federation_pool_access (
  role_id TEXT NOT NULL,
  pool_id TEXT NOT NULL REFERENCES trust_pools(pool_id) ON DELETE CASCADE,
  can_search BOOLEAN NOT NULL DEFAULT TRUE,
  can_get BOOLEAN NOT NULL DEFAULT TRUE,
  can_resolve_manifest BOOLEAN NOT NULL DEFAULT TRUE,
  daily_get_limit INTEGER,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY(role_id, pool_id)
);

CREATE TABLE IF NOT EXISTS user_federation_pool_access (
  user_id TEXT NOT NULL,
  pool_id TEXT NOT NULL REFERENCES trust_pools(pool_id) ON DELETE CASCADE,
  can_search BOOLEAN,
  can_get BOOLEAN,
  can_resolve_manifest BOOLEAN,
  daily_get_limit INTEGER,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY(user_id, pool_id)
);
```

### 17.11 Manifest Requests

```sql
CREATE TABLE IF NOT EXISTS federation_manifest_requests (
  request_id TEXT PRIMARY KEY,
  local_user_id TEXT,
  release_id TEXT NOT NULL,
  manifest_id TEXT NOT NULL,
  requested_from_node_id TEXT NOT NULL REFERENCES federation_nodes(node_id),
  pool_id TEXT NOT NULL,
  status TEXT NOT NULL,
  error TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  completed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_manifest_requests_user
  ON federation_manifest_requests(local_user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_manifest_requests_manifest
  ON federation_manifest_requests(manifest_id, created_at DESC);
```

`local_user_id` is local audit data only. Never send it to remote nodes.

---

## 18. Validation Pipeline

Remote data must never go directly into the normal local indexer cache.

### 18.1 Event Receive Pipeline

```text
Receive event or batch
  -> enforce request auth if protected endpoint
  -> enforce max body size
  -> parse JSON
  -> reject duplicate JSON keys if parser supports it
  -> validate envelope required fields
  -> check supported spec_version
  -> check event_type is known
  -> check timestamp tolerance
  -> check expiration/not_before
  -> canonicalize unsigned event
  -> compute event_id
  -> verify event_id matches
  -> compute body_hash
  -> verify body_hash matches
  -> resolve author public key
  -> verify signature
  -> check node is not locally blocked
  -> check sequence/previous_event_id/fork state
  -> validate event body schema
  -> apply pool membership authorization
  -> apply event-type policy
  -> store event as accepted or rejected
  -> enqueue projection job
```

### 18.2 ReleaseCard Validation

Reject ReleaseCard if:

```text
missing release_id
release_id does not match release identity core if all fields are available
missing title
missing normalized_title
size_bytes is negative or impossible
posted_at is too far in future
groups contain invalid group names
category is unknown and strict mode enabled
manifest_id malformed when present
source node not allowed to publish to pool
expired release card
body contains user/private data fields
```

Projection:

```text
upsert federated_release_cards
upsert federated_release_sources
update manifest sources if manifest availability present
recompute best score
mark resolvable if local manifest exists or trusted manifest source exists
```

### 18.3 ResolutionManifest Validation

Reject ResolutionManifest if:

```text
manifest_id does not equal hash of manifest_core
release_id missing or inconsistent
files array empty
segments array empty for non-empty files
Message-ID syntax invalid
segment bytes <= 0
segment numbers duplicate within a file
groups invalid
size wildly inconsistent with release card
manifest exceeds max size
source node not authorized for pool
```

Optional validation:

```text
generate NZB and parse it back
sample article STAT/HEAD against local provider
verify PAR2 presence
compare segment count to ReleaseCard
compare with health attestations
```

### 18.4 HealthAttestation Validation

Reject if:

```text
release_id missing
status unknown to schema
articles_available > articles_total
missing_articles < 0
confidence outside 0..1
checked_at too far in future
author not pool member
```

Projection:

```text
insert health_attestations
update availability_score for release/manifest
update source node reputation if later contradicted by local checks
```

### 18.5 Trust and Pool Event Validation

Pool events require stricter validation:

```text
PoolGenesis must be signed by creator/admin node.
PoolMemberApproved must contain required approvals.
Approval signers must be active admins at approval time.
PoolMemberRevoked must meet moderation/revocation threshold.
PoolCheckpoint must meet witness threshold.
Tombstone must meet local or pool moderation policy.
```

---

## 19. Aggregator and Newznab Integration

### 19.1 Aggregator Source

Add a `FederatedAggregatorSource` beside existing sources.

```text
Existing sources:
  local indexer cache
  external Newznab aggregators

New source:
  federated release-card cache
```

Search flow:

```text
/api?t=search&q=example&apikey=local_key
  -> authenticate local API key
  -> load local user context and roles
  -> determine allowed categories
  -> determine allowed federation pools
  -> query local indexer cache
  -> query external aggregator cache if configured
  -> query federated_release_cards joined to allowed pools/sources
  -> filter tombstoned/low-score/non-resolvable results
  -> merge and dedupe
  -> rank results
  -> return Newznab XML/JSON
```

### 19.2 Newznab Result Links

For federated results, Newznab result links must point back to the user's own node.

Correct:

```text
https://home-node.example/api?t=get&id=rel_123&apikey=<local_key>
```

Incorrect:

```text
https://remote-node.example/api?t=get&id=rel_123
```

### 19.3 Get Flow

```text
/api?t=get&id=rel_123&apikey=local_key
  -> authenticate local API key
  -> check local RBAC gonzbnet.get
  -> check user's allowed pool access
  -> check tombstone/policy/trust thresholds
  -> if local NZB exists, return it
  -> else if local manifest exists, generate/return NZB
  -> else if user can resolve manifest, select trusted manifest source
  -> send signed node-to-node ManifestRequest
  -> receive signed ResolutionManifest event/body
  -> validate manifest
  -> cache manifest
  -> generate NZB
  -> cache NZB
  -> return NZB to local user
```

### 19.4 What to Expose to *Arr Clients

For Newznab/*Arr output, return only federated results that are likely downloadable:

```text
local_manifest
local_nzb
remote_manifest_available from trusted source
remote_manifest_quorum_available
```

Hide or downrank:

```text
metadata_only
manifest_unknown
manifest_source_offline
low_trust
failed_resolution
```

The web UI may show metadata-only results with a badge, but Newznab clients should not receive results that will probably fail on `t=get`.

---

## 20. Node-to-Node Interaction Examples

### 20.1 Event Sync

```text
Node A has local ReleaseCard event evt_1.
Node B is an approved peer in pool.private.movies.
Node B pulls Node A outbox.
Node B receives evt_1.
Node B validates signature, pool membership, schema, and trust.
Node B projects release into federated_release_cards.
Node B's local users can now find the release if RBAC allows that pool.
```

### 20.2 Remote Manifest Fetch

```text
User on Node B grabs rel_123.
Node B has ReleaseCard but no manifest.
Node B finds manifest source Node A.
Node B signs ManifestRequest for man_456.
Node A verifies Node B is active pool member.
Node A returns signed ResolutionManifest.
Node B validates manifest_id, signature, schema, and policy.
Node B generates NZB and returns it to user.
Node A only sees that Node B requested man_456.
Node A does not see the local user or API key.
```

### 20.3 Pool Join

```text
Node C wants to join pool.private.movies.
Node C sends PoolJoinRequest.
Admin nodes A and B sign approvals.
PoolMemberApproved reaches threshold 2.
Nodes in pool accept Node C as active member.
Future Node C ReleaseCards can be accepted according to policy.
```

---

## 21. Configuration

Add config keys using the existing project config style. Environment names below are canonical unless the project has a different naming convention.

```env
# Core
GONZBNET_ENABLED=false
GONZBNET_MODE=integrated
GONZBNET_NODE_ALIAS=
GONZBNET_ADVERTISE_URL=https://example.com/gonzbnet/v1
GONZBNET_KEYS_DIR=data/gonzbnet/keys
GONZBNET_KEY_PASSWORD=

# Protocol
GONZBNET_SPEC_VERSION=gonzbnet/1.0
GONZBNET_HTTP_ENABLED=true
GONZBNET_HTTP_BASE_PATH=/gonzbnet/v1
GONZBNET_WS_ENABLED=false
GONZBNET_WS_PORT=8765
GONZBNET_HTTP_PORT=8766

# Network / peers
GONZBNET_PRIVATE_NETWORK=true
GONZBNET_NETWORK_ID=default
GONZBNET_BOOTSTRAP_NODES=[]
GONZBNET_MANUAL_PEERS=[]
GONZBNET_PEER_EXCHANGE_ENABLED=false
GONZBNET_REACHABILITY_CHECK=true

# Trust pools
GONZBNET_TRUST_POOLS=[]
GONZBNET_ACCEPT_MODE=pool_member
GONZBNET_POOL_APPROVAL_QUORUM=2
GONZBNET_RELEASE_QUORUM=1
GONZBNET_MANIFEST_QUORUM=1
GONZBNET_HEALTH_QUORUM=1
GONZBNET_MIN_NODE_TRUST_SCORE=0.35
GONZBNET_MIN_RESULT_SCORE=0.50

# Sharing
GONZBNET_SHARE_RELEASE_CARDS=true
GONZBNET_SHARE_HEALTH_ATTESTATIONS=true
GONZBNET_SHARE_TRUST_ATTESTATIONS=true
GONZBNET_SHARE_RESOLUTION_MANIFESTS=trusted_only
GONZBNET_ACCEPT_REMOTE_MANIFESTS=true
GONZBNET_LIVE_QUERY_ENABLED=false

# Privacy
GONZBNET_SHARE_PROVIDER_BACKBONE_HASH=false
GONZBNET_SHARE_SOURCE_INDEXER_HASH=false
GONZBNET_SEND_USER_CONTEXT=false

# Limits
GONZBNET_MAX_EVENT_BYTES=262144
GONZBNET_MAX_MANIFEST_BYTES=10485760
GONZBNET_MAX_BATCH_EVENTS=100
GONZBNET_RATE_LIMIT_EVENTS_PER_MINUTE=120
GONZBNET_RATE_LIMIT_MANIFEST_REQUESTS_PER_MINUTE=30
GONZBNET_REMOTE_GET_TIMEOUT_SECONDS=15
GONZBNET_TIME_TOLERANCE_SECONDS=120
GONZBNET_NONCE_TTL_SECONDS=600

# Gossip v2
GONZBNET_GOSSIP_ENABLED=false
GONZBNET_GOSSIP_FANOUT=3
GONZBNET_GOSSIP_TTL=5
GONZBNET_GOSSIP_INTERVAL_SECONDS=30

# Health
GONZBNET_HEALTH_CHECK_ENABLED=false
GONZBNET_HEALTH_CHECK_SAMPLE_SIZE=50
GONZBNET_HEALTH_CHECK_INTERVAL_MINUTES=360

# Relay future
GONZBNET_RELAY_URL=
GONZBNET_RELAY_API_KEY=
```

---

## 22. Implementation Phases

### Phase 0: Scaffolding and Migrations

Build:

```text
gonzbnet module directory
configuration loader
feature flags
PostgreSQL migrations
basic admin status endpoint
```

Acceptance criteria:

```text
App starts with GONZBNET_ENABLED=false.
App starts with GONZBNET_ENABLED=true.
All migrations apply cleanly.
No federation activity occurs until enabled.
```

### Phase 1: Identity, Canonical JSON, Signing

Build:

```text
Ed25519 key generation and persistent storage
node_id derivation
JCS canonicalization wrapper
body_hash calculation
event_id calculation
SignedEvent creation
SignedEvent verification
nonce replay cache
```

Acceptance criteria:

```text
Node creates persistent identity on first start.
Restart keeps same node_id.
ReleaseCard event can be signed and verified locally.
Changing any signed field breaks verification.
Changing body breaks body_hash.
Event IDs are deterministic.
JCS canonical bytes are stored separately from jsonb projections.
```

### Phase 2: Publish Local Indexer Cache as ReleaseCards

Build:

```text
mapper from local indexer/cache records to ReleaseCard
release normalization and fingerprinting
local publisher worker
local event storage
projection to federated_release_cards
```

Acceptance criteria:

```text
Existing indexed releases produce ReleaseCard events.
Duplicate local releases produce stable release_id where possible.
Release cards appear in federated_release_cards.
No remote peer is required.
Aggregator can optionally search federated cache locally.
```

### Phase 3: Manual Federation Pull Sync

Build:

```text
/.well-known/gonzbnet
/gonzbnet/v1/node
/gonzbnet/v1/caps
/gonzbnet/v1/handshake
/gonzbnet/v1/outbox
/gonzbnet/v1/events/{id}
manual peer config
peer cursor sync
remote event verification
quarantine/rejection storage
```

Acceptance criteria:

```text
Node A can add Node B manually.
Node A can fetch Node B profile/caps.
Node A can pull Node B ReleaseCards.
Invalid signatures are rejected.
Duplicate events are ignored.
Rejected events are visible in logs/admin diagnostics.
Accepted events project into federated cache.
```

### Phase 4: Inbox Push Sync

Build:

```text
/gonzbnet/v1/inbox
EventBatch handling
request signature auth
batch response with accepted/duplicate/rejected
peer delivery tracking
backoff and retry
```

Acceptance criteria:

```text
Node A can push signed event to Node B.
Node B validates and stores event.
Node B rejects invalid event with structured error.
Node A records delivery status.
```

### Phase 5: RBAC and Aggregator Integration

Build:

```text
role_federation_pool_access
user_federation_pool_access if needed
permissions gonzbnet.search/get/resolve_manifest/admin.*
FederatedAggregatorSource
Newznab result mapping
result filtering by pool and trust
```

Acceptance criteria:

```text
A local user without gonzbnet.search sees no federated results.
A local user with pool access sees accepted federated results.
Newznab result links point to the local node.
Remote search queries are not sent to peers.
```

### Phase 6: Trust Pools

Build:

```text
PoolGenesis
PoolJoinRequest
PoolMemberApproved
PoolMemberRevoked
pool_members projection
M-of-N threshold validation
pool authorizer
local admin pool management APIs
```

Acceptance criteria:

```text
Pool can require 2-of-3 admins to approve a node.
Events from non-members are rejected for protected pools.
Revoked members can no longer contribute.
Pool policy controls accepted event types and minimum trust.
```

### Phase 7: Resolution Manifests and NZB Generation

Build:

```text
ResolutionManifest schema
manifest ID validation
manifest source tracking
/gonzbnet/v1/manifests/{id}/request
/gonzbnet/v1/manifests/{id}
manifest resolver
manifest cache
NZB generator from manifest
Newznab t=get integration
```

Acceptance criteria:

```text
Search can return remote ReleaseCard.
Get can fetch manifest from trusted peer.
Manifest hash must match manifest_id.
Generated NZB is valid and parsable.
Manifest and NZB are cached after verification.
Remote peer never receives local user API key or username.
```

### Phase 8: Health Attestations and Scoring

Build:

```text
HealthAttestation event
optional local health checker
availability score aggregation
reputation adjustments from health accuracy
search ranking integration
```

Acceptance criteria:

```text
Node can publish complete/incomplete attestation.
Multiple attestations update availability_score.
Bad health claims can reduce node trust.
Search ranking uses health/availability score.
```

### Phase 9: Moderation and Tombstones

Build:

```text
Tombstone event
local blocklist
pool moderation threshold
projection hiding/rejecting tombstoned targets
admin UI/API for moderation
```

Acceptance criteria:

```text
Admin can tombstone bad release locally.
Pool can enforce M-of-N moderation if configured.
Tombstoned releases are hidden from Newznab search.
Existing cached manifests can be invalidated by tombstone policy.
```

### Phase 10: WebSocket Gossip and Peer Exchange

Build only after phases 1-9 are stable.

```text
/gonzbnet/v1/ws
GossipBatch
TTL/fanout
event dedupe cache
peer exchange
rate limiting
connection backoff
```

Acceptance criteria:

```text
New release propagates through connected peers.
TTL prevents endless propagation.
Duplicate events are processed once.
Invalid events are not forwarded.
Peer exchange can be disabled completely.
```

### Phase 11: Optional Relay Mode

Build only if operational need exists.

```text
standalone gonzbnet-relay process
shared database or narrow internal API
relay API key
P2P public endpoints isolated from main app
```

Acceptance criteria:

```text
Main app can run without public federation port.
Relay handles peer sync and writes accepted events.
Main app aggregator sees relay-projected releases.
```

---

## 23. Security Requirements

### 23.1 Key Security

```text
Store node private key in GONZBNET_KEYS_DIR.
Support optional key encryption with GONZBNET_KEY_PASSWORD.
Never log private key material.
Never return private key material from admin APIs.
Support key backup/export only through explicit admin action.
```

### 23.2 Request Security

```text
Require signed node requests for protected routes.
Reject replayed nonces.
Reject stale timestamps.
Enforce body size limits.
Rate-limit inbox and manifest endpoints.
Require TLS for production peers.
Allow insecure HTTP only for explicit local development.
```

### 23.3 Data Security

```text
Quarantine remote data before projection.
Never trust jsonb reserialization for signatures.
Never expose remote-only diagnostic fields to Newznab clients unless intentional.
Scrub user data from manifest requests.
Avoid storing remote IPs in signed events.
```

### 23.4 Abuse Resistance

```text
deduplicate event IDs
track invalid signature rates
backoff failing peers
temporary ban flooders
limit batch size
limit event age
limit manifest size
reject unknown event types unless compatibility is declared
provide local blocklist
provide pool tombstones
```

### 23.5 Privacy Defaults

Default privacy posture:

```text
live federated query disabled
provider hash sharing disabled
source indexer hash sharing disabled
user context sharing disabled
manifest sharing trusted_only
private network enabled when configured with pools
```

---

## 24. Testing Plan

### 24.1 Unit Tests

```text
JCS canonicalization deterministic output
body_hash changes when body changes
event_id deterministic
event signature valid/invalid cases
node_id derivation
request signature verification
nonce replay rejection
pool threshold validation
release ID computation
manifest ID computation
score formula
RBAC pool access checks
```

### 24.2 Integration Tests

```text
Node A publishes ReleaseCard.
Node B pulls and accepts it.
Node B rejects tampered ReleaseCard.
Node B rejects non-member pool event.
Node B search returns accepted remote result for authorized user.
Unauthorized user cannot see federated result.
Node B get fetches manifest from Node A.
Node B returns generated NZB.
Tombstone hides result.
Revoked node cannot publish future events.
```

### 24.3 End-to-End Test Environment

Create test harness with three local nodes:

```text
Node A: pool admin and source
Node B: member and consumer
Node C: non-member / malicious test node
```

Test:

```text
A creates pool.
A+B approve B.
A publishes release.
B syncs release.
B local user searches.
B local user grabs.
B fetches manifest from A.
C attempts to push event and is rejected.
A/B tombstone release and B hides it.
```

### 24.4 Security Tests

```text
invalid signature
valid signature but wrong event_id
valid event but wrong body_hash
replayed request nonce
future timestamp
expired event
oversized event
oversized manifest
pool approval with insufficient signatures
approval signed by non-admin
revoked node publishing event
manifest_id mismatch
malformed Message-ID
```

---

## 25. Admin UI/API Requirements

The implementation should expose admin service methods even if the UI is added later.

Admin views:

```text
local node profile
node ID and public key
peer list and status
manual add/remove peer
sync status and cursors
event log and validation status
rejected events/dead-letter queue
trust pools
pool members
join requests
approval actions
revocations
tombstones
release source details
manifest source details
health attestations
node reputation
configuration validation
```

Admin actions:

```text
enable/disable GoNZBNet
rotate node key with warning
add manual peer
remove peer
block node
unblock node
create pool
request pool join
approve/revoke member
set role pool access
issue local tombstone
publish pool tombstone if authorized
force sync peer
force resolve manifest
recompute scores
```

---

## 26. Observability

Log structured events for:

```text
node identity loaded/created
peer handshake success/failure
outbox sync start/end
inbox batch accepted/rejected counts
signature verification failure
schema validation failure
pool authorization failure
manifest request sent/received
manifest validation success/failure
NZB generation success/failure
health attestation created
trust score changed
tombstone applied
```

Metrics:

```text
gonzbnet_events_received_total
gonzbnet_events_accepted_total
gonzbnet_events_rejected_total
gonzbnet_peer_sync_duration_seconds
gonzbnet_peer_failures_total
gonzbnet_manifest_requests_total
gonzbnet_manifest_request_failures_total
gonzbnet_manifest_resolution_duration_seconds
gonzbnet_release_cards_projected_total
gonzbnet_health_attestations_total
gonzbnet_tombstones_active_total
```

---

## 27. Error Codes

Use stable machine-readable error codes.

```text
invalid_json
invalid_schema
unsupported_spec_version
unsupported_event_type
payload_too_large
invalid_event_id
invalid_body_hash
invalid_signature
unknown_node
node_blocked
node_revoked
not_pool_member
insufficient_pool_role
insufficient_pool_quorum
expired_event
future_timestamp
replayed_nonce
sequence_conflict
fork_detected
duplicate_event
manifest_not_found
manifest_not_visible
manifest_id_mismatch
manifest_invalid
rate_limited
internal_error
```

---

## 28. Newznab Compatibility Details

Federated results should map into the existing Newznab response model.

Suggested mapping:

```text
Newznab title      <- ReleaseCard.title
Newznab guid       <- ReleaseCard.release_id or existing aggregator GUID wrapper
Newznab size       <- ReleaseCard.size_bytes
Newznab category   <- ReleaseCard.newznab_categories
Newznab pubDate    <- ReleaseCard.posted_at
Newznab link       <- local /api?t=get&id=<release_id>
Newznab comments   <- local GoNZB details page if available
Newznab attrs      <- quality/media fields where compatible
```

Do not expose remote node URL directly as the download link.

Deduping should consider:

```text
release_id
manifest_id
normalized_title + size_bytes
subject_fingerprint
file_fingerprint
existing local indexer GUIDs if mapped
```

Ranking should prefer:

```text
local verified result
trusted federated result with local manifest
trusted federated result with high health score
remote manifest available from multiple trusted sources
newer result, all else equal
```

---

## 29. Open Future Extensions, Not v1 Requirements

Do not implement these unless explicitly requested:

```text
full ActivityPub JSON-LD compatibility
cross-node user login
OIDC between nodes
user-owned signing keys
libp2p transport
DHT-based discovery
blockchain or token incentives
public open federation
live federated search
pool-level encrypted manifest bodies
CBOR as primary wire format
relay mode as separate process
automatic NAT traversal
```

Schema should leave room for some of these, but v1 should stay focused.

---

## 30. Codex Implementation Prompt

Use the following as the direct implementation prompt.

```text
Implement a new GoNZB module family named GoNZBNet inside the existing modular monolith.

Goal:
Create a federated metadata discovery layer for GoNZB. It must share signed release metadata between approved self-hosted nodes, ingest trusted remote metadata into a separate federated cache, expose accepted remote releases through the existing Newznab-compatible aggregator, and resolve missing NZB-equivalent manifests on demand from trusted peers. It must not transmit Usenet article payloads or local user data.

Architecture decisions:
- Keep v1 as a modular-monolith feature, not a separate microservice.
- Design interfaces so an optional relay process can be extracted later.
- Use HTTPS inbox/outbox pull/push federation first.
- Do not implement WebSocket gossip until the core signed event and trust-pool flow works.
- Do not implement blockchain. Use signed append-only event logs, event hashes, pool checkpoints, and M-of-N approval events.
- Use Ed25519 node identity.
- Use RFC 8785-style canonical JSON for signing and hashing.
- Use PostgreSQL relational tables plus jsonb projections.
- Store canonical signed JSON separately from jsonb because jsonb must not be used as the source of signature bytes.
- Local users authenticate only to their home GoNZB node.
- Remote nodes authenticate and authorize other nodes, not users.
- Do not implement cross-node user login in v1.
- Do not live-broadcast user searches to remote nodes by default.

Core event types:
- NodeProfile
- ReleaseCard
- ResolutionManifest
- ManifestAvailability
- HealthAttestation
- TrustAttestation
- PoolGenesis
- PoolJoinRequest
- PoolMemberApproved
- PoolMemberRevoked
- PoolCheckpoint
- Tombstone

Core endpoints:
- GET  /.well-known/gonzbnet
- GET  /gonzbnet/v1/node
- GET  /gonzbnet/v1/caps
- POST /gonzbnet/v1/handshake
- POST /gonzbnet/v1/inbox
- GET  /gonzbnet/v1/outbox
- GET  /gonzbnet/v1/events/{event_id}
- POST /gonzbnet/v1/manifests/{manifest_id}/request
- GET  /gonzbnet/v1/manifests/{manifest_id}
- GET  /gonzbnet/v1/pools/{pool_id}/checkpoint
- GET  /gonzbnet/v1/pools/{pool_id}/members

RBAC:
Add local permissions:
- gonzbnet.search
- gonzbnet.get
- gonzbnet.resolve_manifest
- gonzbnet.view_trust_score
- gonzbnet.view_source_node
- gonzbnet.admin.peers
- gonzbnet.admin.pools
- gonzbnet.admin.moderation
- gonzbnet.admin.keys

Add role/user to federation-pool access mapping with can_search, can_get, and can_resolve_manifest.

Search behavior:
A user searches only their home node. The home node searches local indexer data plus accepted federated release cards from pools the user is authorized to access. Federated results returned through Newznab must point to the home node's /api?t=get endpoint.

Get behavior:
When a user grabs a federated result and the home node lacks the manifest/NZB, the home node sends a signed node-to-node ManifestRequest to trusted manifest source peers. The remote peer authorizes the requesting node by pool membership and node trust, returns a signed ResolutionManifest, and the home node validates, caches, generates NZB, and returns the NZB to the local user.

Privacy:
Never send local user API keys, usernames, user IDs, search history, grab history, download history, provider credentials, or indexer credentials to remote nodes.

Implementation phases:
1. Scaffolding, config, migrations.
2. Identity, canonical JSON, signing, event verification.
3. Local indexer cache -> ReleaseCard publishing.
4. Manual peer federation with node/caps/handshake/outbox.
5. Inbox push and batch handling.
6. RBAC and aggregator integration.
7. Trust pools and M-of-N approvals.
8. Resolution manifests and NZB generation.
9. Health attestations and scoring.
10. Tombstones and moderation.
11. Optional WebSocket gossip.
12. Optional relay mode.

Follow the schema, endpoint, validation, storage, trust, and RBAC details in this document exactly unless existing GoNZB project conventions require minor naming changes.
```

---

## 31. Final Implementation Checklist

Codex should consider the first complete v1 done only when all of these are true:

```text
[ ] GoNZBNet can be enabled/disabled by config.
[ ] Node identity persists across restarts.
[ ] SignedEvent creation and verification works.
[ ] Canonical signed JSON is stored separately from jsonb.
[ ] Local indexer records can publish ReleaseCards.
[ ] Remote ReleaseCards can be pulled from a manual peer.
[ ] Invalid signatures are rejected.
[ ] Trust-pool membership controls event acceptance.
[ ] Local RBAC controls which users see which federation pools.
[ ] Newznab search returns accepted federated results through the local node.
[ ] Newznab get resolves missing manifests through trusted peers.
[ ] Remote nodes never receive local user identity/API key/search query.
[ ] ResolutionManifest validation recomputes manifest_id.
[ ] Generated NZBs are valid.
[ ] Health attestations update availability scoring.
[ ] Tombstones hide/reject results.
[ ] Admin diagnostics expose peer, event, pool, and validation state.
[ ] Tests cover tampering, replay, non-member events, RBAC denial, and remote manifest fetch.
```

---

# GoNZBNet Addendum: Optional Participation Modules, Distributed Coverage, and Dedup-Aware Scanning

**Status:** Codex-ready addendum to `GoNZBNet_Codex_Implementation_Spec.md`
**Target project:** GoNZB
**Target runtime shape:** modular monolith first, optional relay extraction later
**Database assumption:** PostgreSQL
**Date:** 2026-07-07

This addendum extends the existing GoNZBNet implementation specification with a capability-based participation model. The goal is to let different people in a private pool contribute in different ways: some nodes may scrape/index newsgroups, some may only validate article availability or checksums, some may only cache manifests, some may only relay federation traffic, and some may only consume trusted federated metadata through their home node.

The design must preserve these earlier decisions:

```text
- Users authenticate only to their home GoNZB node.
- Remote nodes authenticate and authorize nodes, not users.
- Federation shares metadata, manifests, health, trust, and coverage state.
- Federation must not share user search history, user identities, API keys, NNTP provider credentials, or downloaded payload files.
- Search should normally use the home node's local federated cache.
- Missing manifests/NZBs should be resolved on demand through trusted node-to-node requests.
- GoNZBNet remains a modular monolith in v1, with optional modules that can be enabled/disabled independently.
```

---

## 1. Design Goal

A private GoNZBNet pool should behave like a distributed indexing cooperative:

```text
Node A scans assigned newsgroups/ranges.
Node B validates article existence and manifest health.
Node C builds manifests from header data.
Node D relays federation events.
Node E only consumes/searches accepted federated metadata.
All nodes share signed metadata according to their approved pool roles.
```

Not every node needs to run the full indexer/scanner stack. The implementation must allow partial participation without breaking search, get, validation, trust, or federation flows.

---

## 2. Core Design Decision: Capability-Based Nodes

Each node must advertise what it can and is willing to do. Pool admins may approve a node for one or more contribution roles.

Supported participation roles:

```text
consumer
  Searches accepted federated metadata through its own home node.
  Does not scan, validate, build manifests, or relay unless other capabilities are enabled.

scanner
  Reads NNTP headers for assigned groups/ranges/time windows.
  Emits ReleaseCard events and optional scan checkpoints.
  May or may not expose those releases through its own local Newznab API.

indexer
  Maintains a local user-facing searchable index.
  This is distinct from scanner. A node may scan/publish without being a full user-facing indexer.

manifest_builder
  Builds ResolutionManifest objects from local scan/header data.
  May serve signed manifests to trusted peers.

validator
  Validates remote release cards/manifests using its own NNTP provider.
  Emits HealthAttestation, ArticleAvailabilityAttestation, or ChecksumAttestation events.
  Does not need to index or expose search.

health_checker
  Periodically rechecks known manifests for completeness/availability drift.
  Emits updated HealthAttestation events.

relay
  Handles federation transport, inbox/outbox, event fanout, and optional WebSocket gossip.
  Does not need to scan or validate.

manifest_cache
  Stores signed manifests/NZBs for trusted pools.
  Can serve cached manifests to approved nodes.

coverage_coordinator
  Proposes CoveragePlan and CoverageAssignment events.
  This role is not trusted by default; plans still require pool policy approval/signatures.

admin
  Manages local node settings, peer approval, pool membership, moderation, and local RBAC.
```

A node may have multiple roles:

```text
small private home node:
  consumer + manifest_cache

power user node:
  consumer + scanner + manifest_builder + validator

public relay node:
  relay only

validation-only node:
  validator + health_checker

pool coordinator node:
  coverage_coordinator + relay + admin
```

---

## 3. Required vs Optional Modules

The existing modular monolith should gain explicit module boundaries. Every optional module must degrade gracefully when disabled.

### 3.1 Required foundation modules

These are required when `GONZBNET_ENABLED=true`:

```text
gonzbnet.identity
  Ed25519 keypair, node ID, key rotation.

gonzbnet.events
  SignedEvent envelope, canonical JSON, hashing, signature verification.

gonzbnet.store
  Event store, node store, pool store, projection tables.

gonzbnet.transport.http
  Node profile, caps, inbox/outbox, event fetch, manifest request endpoints.

gonzbnet.peers
  Manual peer configuration, peer state, handshake, sync cursors.

gonzbnet.pools
  Trust pool membership, approvals, revocations, policy enforcement.

gonzbnet.rbac
  Local user/role mapping to federation pools and permissions.
```

### 3.2 Optional contribution modules

These must be independently enableable:

```text
gonzbnet.scanner
  NNTP group scanning, header crawling, release candidate detection, ReleaseCard publication.

gonzbnet.index_projection
  Projects accepted releases into the local user-facing search/indexer cache.
  Can be disabled even if scanner is enabled.

gonzbnet.manifest_builder
  Builds ResolutionManifest objects from local header data.

gonzbnet.manifest_cache
  Stores remote/local manifests and generated NZBs.

gonzbnet.validator
  Validates remote ReleaseCards/ResolutionManifests without indexing them.

gonzbnet.health
  Periodically checks availability/completion of known manifests.

gonzbnet.coverage
  Group catalog, coverage plans, range/time-window claims, checkpoints, dedup-aware scan scheduling.

gonzbnet.scheduler
  Local worker scheduler for scan/validate/build/health tasks.

gonzbnet.relay
  Optional higher-volume federation relay behavior.

gonzbnet.gossip
  Optional WebSocket/libp2p-style gossip. Not required for v1.
```

### 3.3 Dependency rules

Implement these dependency rules explicitly:

```text
scanner requires:
  identity + events + store + pools + optional coverage

manifest_builder requires:
  identity + events + store + local header/scan data

validator requires:
  identity + events + store + NNTP provider access
  It must not require local search/indexing.

health_checker requires:
  validator or manifest_cache + NNTP provider access

consumer/search requires:
  identity + events + store + transport + rbac + aggregator integration
  It must not require scanner.

manifest_cache requires:
  store + manifest resolver
  It must not require scanner.

relay requires:
  identity + events + transport + peers
  It must not require NNTP provider access.

coverage requires:
  identity + events + store + pools
  It should work even if local scanner is disabled, because the node may still display pool coverage.
```

### 3.4 Disabled-module behavior

Codex must implement no-op or disabled behavior instead of hard failures:

```text
scanner disabled:
  Node does not claim scan work and does not emit ReleaseCards from local NNTP scans.
  Node can still receive remote ReleaseCards and search accepted cache.

index_projection disabled:
  Node may scan and publish metadata but does not expose scanned releases through local user search.
  Useful for contribution-only nodes.

manifest_builder disabled:
  Node may publish ReleaseCards without full ResolutionManifests.
  Results should be marked metadata-only unless another trusted node has a manifest.

validator disabled:
  Node does not emit validation attestations.
  Node can still consume other nodes' health attestations.

manifest_cache disabled:
  Node may fetch manifests on demand but should not retain them beyond the request unless temporary cache is required.

relay disabled:
  Node communicates only with configured peers and does not fan out events.

coverage disabled:
  Node may still scan using local settings, but it will not participate in pooled dedup-aware assignment.
```

---

## 4. Node Profile Capability Advertisement

Extend `NodeProfile` with explicit module status, capacity, and privacy-preserving provider scope data.

Example:

```json
{
  "type": "NodeProfile",
  "node_id": "ed25519:abc...",
  "alias": "discord-node-a",
  "software": "GoNZB",
  "software_version": "0.9.0",
  "protocols": ["gonzbnet/1.0"],
  "endpoints": {
    "inbox": "https://node-a.example/gonzbnet/v1/inbox",
    "outbox": "https://node-a.example/gonzbnet/v1/outbox"
  },
  "capabilities": {
    "consumer": true,
    "scanner": true,
    "indexer": false,
    "manifest_builder": true,
    "validator": true,
    "health_checker": true,
    "relay": false,
    "manifest_cache": true,
    "coverage_coordinator": false
  },
  "module_status": {
    "scanner": "enabled",
    "index_projection": "disabled",
    "manifest_builder": "enabled",
    "validator": "enabled",
    "health_checker": "enabled",
    "coverage": "enabled"
  },
  "scanner_capacity": {
    "max_groups": 25,
    "max_articles_per_hour": 250000,
    "max_header_bytes_per_hour": 1073741824,
    "preferred_group_patterns": ["alt.binaries.*"],
    "excluded_group_patterns": [],
    "supports_article_range_scan": true,
    "supports_time_window_scan": true
  },
  "validator_capacity": {
    "max_manifests_per_hour": 500,
    "validation_tiers": ["metadata", "article_stat", "segment_stat", "sample_crc"],
    "supports_yenc_sample_validation": true,
    "supports_par2_validation": false
  },
  "provider_scope": {
    "provider_disclosure": "hash_only",
    "backbone_hash": "sha256:optional-anonymized-label",
    "article_number_scope": "provider_local"
  }
}
```

Privacy requirements:

```text
- Do not share NNTP usernames, passwords, account IDs, server hostnames, or billing/provider account details.
- Provider/backbone data must be optional and anonymized.
- If provider labels are shared, share only a user-configurable hash or coarse operator-defined label.
- Never assume article numbers from different providers are globally comparable.
```

Important NNTP detail:

```text
Newsgroup article numbers are server/provider-local. A claim over article range 1000-2000 on Provider A may not match range 1000-2000 on Provider B. Cross-provider dedup must use Message-ID, subject/file fingerprints, post timestamps, and manifest/release fingerprints. Article-range work claims must be scoped to provider/backbone observation context.
```

---

## 5. Distinguish Scanner, Indexer, and Aggregator

Do not treat scanning as the same thing as indexing.

```text
scanner
  Reads NNTP headers and emits federation events.
  May store temporary scan state.
  May run on a contribution-only node.

indexer
  Maintains local searchable index/cache for local users.
  Existing GoNZB indexer module.
  Can be disabled while scanner remains enabled.

aggregator
  Merges local indexer cache, external Newznab sources, and accepted federated release cards.
  Existing GoNZB aggregator module.
  Can consume federated metadata even if local scanner/indexer is disabled.
```

Supported modes:

```text
contribution-only scanner:
  scanner=true
  index_projection=false
  aggregator_search=false or local UI disabled

validation-only node:
  scanner=false
  indexer=false
  validator=true
  health_checker=true

consumer-only node:
  scanner=false
  validator=false
  aggregator_search=true

full node:
  scanner=true
  indexer=true
  manifest_builder=true
  validator=true
  aggregator_search=true
```

Codex must not assume local indexed visibility just because a ReleaseCard was created locally.

---

## 6. Distributed Coverage Strategy

The pool should avoid every node scraping the same content. The goal is coordinated distributed coverage with intentional overlap.

```text
Coverage = group coverage
         + article-range or time-window coverage
         + provider/backbone diversity
         + manifest-building coverage
         + validation redundancy
         + health recheck coverage
```

The v1 strategy should be:

```text
1. Nodes advertise capabilities and capacity.
2. Nodes publish GroupObservation events.
3. Pool admins or coordinator nodes publish CoveragePlan/CoverageAssignment events.
4. Scanner nodes claim specific work using RangeClaim or TimeWindowClaim leases.
5. Scanner nodes publish RangeComplete or RangeFailed events.
6. Other nodes avoid scanning trusted active/completed claims unless assigned as validators or overlap scanners.
7. Validators intentionally sample/validate some completed work.
8. Coverage dashboard shows gaps, stale claims, duplicate work, and weak validation zones.
```

Start with manual/semi-manual assignment. Do not require fully automatic global scheduling for v1.

---

## 7. Coverage and Dedup Event Types

Add these event types to the GoNZBNet event registry.

### 7.1 ScannerCapacity

Advertises what a node can scan.

```json
{
  "type": "ScannerCapacity",
  "node_id": "ed25519:abc...",
  "pool_id": "pool.discord.private",
  "created_at": "2026-07-07T18:00:00Z",
  "max_groups": 25,
  "max_articles_per_hour": 250000,
  "max_header_bytes_per_hour": 1073741824,
  "preferred_group_patterns": ["alt.binaries.movies*"],
  "excluded_group_patterns": ["*.spam*"],
  "supports_article_range_scan": true,
  "supports_time_window_scan": true,
  "retention_days_observed": 5400
}
```

### 7.2 ValidatorCapacity

Advertises validation ability.

```json
{
  "type": "ValidatorCapacity",
  "node_id": "ed25519:def...",
  "pool_id": "pool.discord.private",
  "created_at": "2026-07-07T18:00:00Z",
  "max_manifests_per_hour": 500,
  "validation_tiers": ["metadata", "article_stat", "segment_stat", "sample_crc"],
  "supports_yenc_sample_validation": true,
  "supports_par2_validation": false
}
```

### 7.3 GroupObservation

Reports what a node sees for a group on its provider/backbone context.

```json
{
  "type": "GroupObservation",
  "node_id": "ed25519:abc...",
  "pool_id": "pool.discord.private",
  "group": "alt.binaries.example",
  "provider_scope_hash": "sha256:optional-provider-or-backbone-label",
  "observed_at": "2026-07-07T18:00:00Z",
  "low_water": 123000,
  "high_water": 456000,
  "estimated_count": 333000,
  "posts_per_hour_estimate": 1200,
  "scan_supported": true,
  "retention_days_observed": 5400
}
```

### 7.4 CoveragePlan

Pool-level plan for who should cover what. A coordinator may propose this, but pool policy decides whether it needs M-of-N signatures.

```json
{
  "type": "CoveragePlan",
  "pool_id": "pool.discord.private",
  "plan_id": "covplan_20260707_001",
  "version": 12,
  "created_at": "2026-07-07T18:00:00Z",
  "created_by_node_id": "ed25519:coordinator...",
  "requires_pool_approval": true,
  "policy": {
    "default_claim_ttl_minutes": 30,
    "min_validator_overlap_percent": 10,
    "trusted_claim_min_score": 0.65,
    "allow_unassigned_scanning": false
  },
  "assignments": [
    {
      "assignment_id": "assign_001",
      "group": "alt.binaries.example",
      "mode": "article_range",
      "primary_nodes": ["ed25519:node-a..."],
      "validator_nodes": ["ed25519:node-b..."],
      "manifest_builder_nodes": ["ed25519:node-c..."],
      "priority": 80,
      "min_redundancy": 2
    }
  ]
}
```

### 7.5 CoverageAssignment

A smaller assignment event, useful when plans are updated incrementally.

```json
{
  "type": "CoverageAssignment",
  "assignment_id": "assign_001",
  "pool_id": "pool.discord.private",
  "group": "alt.binaries.example",
  "mode": "article_range",
  "role": "primary_scanner",
  "assigned_node_id": "ed25519:node-a...",
  "provider_scope_hash": "sha256:optional-provider-scope",
  "range_start": 1200000,
  "range_end": 1210000,
  "window_start": null,
  "window_end": null,
  "priority": 80,
  "created_at": "2026-07-07T18:05:00Z",
  "expires_at": "2026-07-07T19:05:00Z"
}
```

### 7.6 RangeClaim

A lease that says a scanner is actively working a range. Claims are advisory and policy-scoped, not absolute locks.

```json
{
  "type": "RangeClaim",
  "claim_id": "claim_abc123",
  "pool_id": "pool.discord.private",
  "group": "alt.binaries.example",
  "provider_scope_hash": "sha256:optional-provider-scope",
  "claimant_node_id": "ed25519:node-a...",
  "assignment_id": "assign_001",
  "range_start": 1200000,
  "range_end": 1210000,
  "claimed_at": "2026-07-07T18:10:00Z",
  "expires_at": "2026-07-07T18:40:00Z",
  "claim_mode": "primary_scan",
  "expected_checkpoint_interval_seconds": 300
}
```

Range claim rules:

```text
- Only claims from trusted pool members count for dedup decisions.
- Claims must expire automatically.
- Claims must be renewed with progress or they become stale.
- A stale claim should reduce scanner reliability if repeated.
- Admins may override claims.
- Validators may intentionally overlap claimed/completed ranges.
- Claims from low-trust nodes must not block high-priority work.
```

### 7.7 TimeWindowClaim

Use time-window claims when article numbers are not comparable across providers or when the pool prefers time-based scanning.

```json
{
  "type": "TimeWindowClaim",
  "claim_id": "claim_time_abc123",
  "pool_id": "pool.discord.private",
  "group": "alt.binaries.example",
  "provider_scope_hash": "sha256:optional-provider-scope",
  "claimant_node_id": "ed25519:node-a...",
  "window_start": "2026-07-07T12:00:00Z",
  "window_end": "2026-07-07T13:00:00Z",
  "claimed_at": "2026-07-07T18:10:00Z",
  "expires_at": "2026-07-07T18:40:00Z",
  "claim_mode": "primary_scan"
}
```

### 7.8 CoverageCheckpoint

Periodic progress event.

```json
{
  "type": "CoverageCheckpoint",
  "checkpoint_id": "chk_abc123",
  "pool_id": "pool.discord.private",
  "node_id": "ed25519:node-a...",
  "group": "alt.binaries.example",
  "provider_scope_hash": "sha256:optional-provider-scope",
  "claim_id": "claim_abc123",
  "range_start": 1200000,
  "range_current": 1205000,
  "range_end": 1210000,
  "window_start": null,
  "window_end": null,
  "release_cards_emitted": 42,
  "manifests_emitted": 38,
  "errors": 0,
  "checked_at": "2026-07-07T18:20:00Z"
}
```

### 7.9 RangeComplete

Marks a scan range complete.

```json
{
  "type": "RangeComplete",
  "completion_id": "complete_abc123",
  "pool_id": "pool.discord.private",
  "node_id": "ed25519:node-a...",
  "group": "alt.binaries.example",
  "provider_scope_hash": "sha256:optional-provider-scope",
  "claim_id": "claim_abc123",
  "range_start": 1200000,
  "range_end": 1210000,
  "completed_at": "2026-07-07T18:35:00Z",
  "articles_seen": 10001,
  "headers_processed": 9980,
  "release_cards_emitted": 42,
  "manifests_emitted": 38,
  "dedup_candidates_skipped": 12,
  "error_count": 0,
  "range_fingerprint": "sha256:optional-summary-fingerprint"
}
```

### 7.10 RangeFailed

Marks failed work so another node can pick it up.

```json
{
  "type": "RangeFailed",
  "failure_id": "fail_abc123",
  "pool_id": "pool.discord.private",
  "node_id": "ed25519:node-a...",
  "group": "alt.binaries.example",
  "claim_id": "claim_abc123",
  "range_start": 1200000,
  "range_end": 1210000,
  "failed_at": "2026-07-07T18:22:00Z",
  "reason_code": "provider_timeout",
  "retryable": true
}
```

### 7.11 ScannerHeartbeat

Lets the pool know that a scanner is alive and what it is doing.

```json
{
  "type": "ScannerHeartbeat",
  "node_id": "ed25519:node-a...",
  "pool_id": "pool.discord.private",
  "created_at": "2026-07-07T18:25:00Z",
  "active_claims": ["claim_abc123"],
  "queue_depth": 4,
  "current_articles_per_minute": 4100,
  "status": "healthy"
}
```

### 7.12 ArticleAvailabilityAttestation

A validation-only node can emit this without indexing.

```json
{
  "type": "ArticleAvailabilityAttestation",
  "attestation_id": "artatt_abc123",
  "manifest_id": "man_456",
  "release_id": "rel_123",
  "pool_id": "pool.discord.private",
  "author_node_id": "ed25519:validator...",
  "checked_at": "2026-07-07T19:00:00Z",
  "validation_tier": "segment_stat",
  "articles_total": 1200,
  "articles_checked": 1200,
  "articles_available": 1198,
  "missing_articles": 2,
  "sampled": false,
  "provider_scope_hash": "sha256:optional-provider-scope",
  "confidence": 0.92
}
```

### 7.13 ChecksumAttestation

Use this only when the validator is configured to do checksum-capable validation. It must not share payload bytes.

```json
{
  "type": "ChecksumAttestation",
  "attestation_id": "chkatt_abc123",
  "manifest_id": "man_456",
  "release_id": "rel_123",
  "pool_id": "pool.discord.private",
  "author_node_id": "ed25519:validator...",
  "checked_at": "2026-07-07T19:05:00Z",
  "validation_tier": "sample_crc",
  "files_sampled": 3,
  "segments_sampled": 20,
  "checksum_source": "yenc_crc_or_par2_metadata",
  "matched": true,
  "confidence": 0.88
}
```

Validation tiers:

```text
metadata
  Validate schema, release-card/manifest consistency, file counts, sizes, category, group names.

article_stat
  Use message IDs or provider-supported commands to check whether representative articles exist.

segment_stat
  Check all or a large sample of segment message IDs for existence.

sample_crc
  Fetch/decode limited samples to verify checksums if configured.
  Must not share payload bytes.

par2_validation
  Optional heavy validation if the operator explicitly enables it.
  Not required for v1.
```

---

## 8. Dedup Strategy

Dedup should happen at multiple layers.

### 8.1 Work dedup

Prevent nodes from repeatedly scanning the same work:

```text
Use CoveragePlan + CoverageAssignment + RangeClaim + RangeComplete.
Trusted active claims suppress duplicate primary scanning.
Trusted RangeComplete events suppress future primary scans for the same provider-scoped range.
Validators and overlap scanners may intentionally rescan samples.
```

### 8.2 Release dedup

Detect when different nodes found the same release:

```text
Primary keys/signals:
  manifest_id when available
  nzb_sha256 when available
  normalized title
  size_bytes
  group list
  posted_at time bucket
  poster_hash
  subject_fingerprint
  file_set_fingerprint
  message_id_set_hash
  par2 base hash if available
```

Do not rely on title alone.

Recommended release dedup score:

```text
same manifest_id: hard same
same nzb_sha256: hard same
same message_id_set_hash: hard same
same file_set_fingerprint + size within tolerance + posted_at bucket: probable same
same normalized title only: weak same; do not auto-merge unless other signals match
```

### 8.3 Manifest dedup

Two manifests may represent the same release but have different ordering or encoding. Normalize before hashing:

```text
- Sort files by canonical file path/name.
- Sort segments by segment number.
- Normalize message IDs.
- Normalize groups.
- Remove transport-only metadata.
- Hash canonical normalized manifest body.
```

### 8.4 Seen-set summaries, not v1 default

A future optimization may share rolling seen-set summaries using Bloom filters or Cuckoo filters:

```text
SeenSetSummary
  group
  provider_scope_hash
  time_window
  subject_fingerprint_filter
  message_id_filter
```

Do not implement this as a v1 requirement because it can leak more about what a node has seen and adds complexity. For v1, use coverage claims/completions and normal release dedup.

---

## 9. Coverage Scheduler Design

The scheduler may be manual, semi-automatic, or automatic.

### 9.1 v1: manual/semi-automatic

Pool admins assign groups/ranges using UI/API. Nodes can suggest capacity and observations.

```text
Inputs:
  NodeProfile capabilities
  ScannerCapacity
  ValidatorCapacity
  GroupObservation
  existing CoveragePlan
  existing RangeClaim/RangeComplete state
  node trust score

Outputs:
  CoveragePlan
  CoverageAssignment
```

### 9.2 v1.5: local advisory scheduler

Each scanner node can ask:

```text
What should I scan next?
```

The local scheduler should choose work where:

```text
- group is in allowed pool assignments
- no trusted active claim exists
- no trusted completion exists for the same provider-scoped range/window
- backlog is high
- local capacity matches the group
- minimum validation overlap policy is satisfied
```

### 9.3 v2: deterministic distributed scheduling

Optional future improvement:

```text
Use weighted rendezvous hashing over:
  pool_id + group + provider_scope + article_range_bucket/time_window_bucket

Weights come from:
  node scanner capacity
  node trust score
  node health
  pool role approvals
```

This avoids requiring a central coordinator. Do not make this a v1 blocker.

---

## 10. Coverage Scoring

Add a per-group/pool coverage score for admin dashboards and automated assignment.

```text
coverage_score =
  scanner_recentness_score
+ validator_recentness_score
+ manifest_completion_score
+ provider_diversity_score
+ redundancy_score
- backlog_penalty
- stale_claim_penalty
- failed_scan_penalty
- duplicate_work_penalty
```

Suggested bands:

```text
95-100 = excellent
75-94  = good
50-74  = partial
25-49  = weak
0-24   = uncovered
```

Dashboard example:

```text
Group                  Coverage  Primary   Validator  Backlog   Duplicate Work
alt.binaries.example   91        node-a    node-b     2 min     low
alt.binaries.other     48        node-c    none       6 hr      medium
```

---

## 11. Trust and Reputation Effects

Participation roles should contribute to trust separately.

Add per-role reliability metrics:

```text
scanner_reliability
  ReleaseCards produced, dedup correctness, manifest consistency, false/malformed rate.

manifest_reliability
  Manifests served, validation pass rate, failed fetch rate, mismatch rate.

validator_reliability
  Accuracy of article availability/health/checksum attestations versus later consensus.

coverage_reliability
  Claims completed, stale claims, failed ranges, duplicate-work rate.

relay_reliability
  Delivery success, duplicate rate, invalid event forwarding rate.
```

Scoring examples:

```text
Reward:
  completed assigned range
  emitted valid ReleaseCards
  manifest later validated by others
  accurate availability attestation
  useful validation on another node's scan

Penalize:
  stale RangeClaim without progress
  repeated RangeFailed without valid reason
  malformed ReleaseCards
  manifests that fail validation
  false health/checksum attestations
  claiming too much work and blocking coverage
```

Claims should only suppress duplicate scanning when the claimant's trust is above the pool threshold.

---

## 12. PostgreSQL Schema Additions

Add these tables in addition to the existing GoNZBNet schema.

```sql
CREATE TABLE federation_node_capabilities (
  node_id TEXT PRIMARY KEY,
  capabilities JSONB NOT NULL DEFAULT '{}'::jsonb,
  module_status JSONB NOT NULL DEFAULT '{}'::jsonb,
  scanner_capacity JSONB,
  validator_capacity JSONB,
  provider_scope JSONB,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

```sql
CREATE TABLE federation_node_pool_roles (
  pool_id TEXT NOT NULL,
  node_id TEXT NOT NULL,
  role TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'active',
  approved_event_id TEXT,
  revoked_event_id TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (pool_id, node_id, role)
);
```

```sql
CREATE TABLE gonzbnet_group_observations (
  observation_id TEXT PRIMARY KEY,
  pool_id TEXT NOT NULL,
  node_id TEXT NOT NULL,
  group_name TEXT NOT NULL,
  provider_scope_hash TEXT,
  observed_at TIMESTAMPTZ NOT NULL,
  low_water BIGINT,
  high_water BIGINT,
  estimated_count BIGINT,
  posts_per_hour_estimate DOUBLE PRECISION,
  retention_days_observed INTEGER,
  scan_supported BOOLEAN NOT NULL DEFAULT true,
  source_event_id TEXT NOT NULL
);

CREATE INDEX idx_group_observations_pool_group
  ON gonzbnet_group_observations(pool_id, group_name, observed_at DESC);
```

```sql
CREATE TABLE gonzbnet_coverage_plans (
  plan_id TEXT PRIMARY KEY,
  pool_id TEXT NOT NULL,
  version INTEGER NOT NULL,
  created_by_node_id TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  policy JSONB NOT NULL DEFAULT '{}'::jsonb,
  status TEXT NOT NULL DEFAULT 'active',
  source_event_id TEXT NOT NULL
);
```

```sql
CREATE TABLE gonzbnet_coverage_assignments (
  assignment_id TEXT PRIMARY KEY,
  plan_id TEXT,
  pool_id TEXT NOT NULL,
  group_name TEXT NOT NULL,
  mode TEXT NOT NULL,
  role TEXT NOT NULL,
  assigned_node_id TEXT NOT NULL,
  provider_scope_hash TEXT,
  range_start BIGINT,
  range_end BIGINT,
  window_start TIMESTAMPTZ,
  window_end TIMESTAMPTZ,
  priority INTEGER NOT NULL DEFAULT 50,
  status TEXT NOT NULL DEFAULT 'active',
  created_at TIMESTAMPTZ NOT NULL,
  expires_at TIMESTAMPTZ,
  source_event_id TEXT NOT NULL
);

CREATE INDEX idx_coverage_assignments_node
  ON gonzbnet_coverage_assignments(pool_id, assigned_node_id, status);
```

```sql
CREATE TABLE gonzbnet_range_claims (
  claim_id TEXT PRIMARY KEY,
  pool_id TEXT NOT NULL,
  group_name TEXT NOT NULL,
  provider_scope_hash TEXT,
  claimant_node_id TEXT NOT NULL,
  assignment_id TEXT,
  range_start BIGINT,
  range_end BIGINT,
  window_start TIMESTAMPTZ,
  window_end TIMESTAMPTZ,
  claim_mode TEXT NOT NULL,
  claimed_at TIMESTAMPTZ NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  status TEXT NOT NULL DEFAULT 'active',
  last_checkpoint_at TIMESTAMPTZ,
  source_event_id TEXT NOT NULL
);

CREATE INDEX idx_range_claims_active
  ON gonzbnet_range_claims(pool_id, group_name, provider_scope_hash, status, expires_at);
```

```sql
CREATE TABLE gonzbnet_coverage_checkpoints (
  checkpoint_id TEXT PRIMARY KEY,
  pool_id TEXT NOT NULL,
  node_id TEXT NOT NULL,
  group_name TEXT NOT NULL,
  provider_scope_hash TEXT,
  claim_id TEXT,
  range_start BIGINT,
  range_current BIGINT,
  range_end BIGINT,
  window_start TIMESTAMPTZ,
  window_end TIMESTAMPTZ,
  release_cards_emitted INTEGER NOT NULL DEFAULT 0,
  manifests_emitted INTEGER NOT NULL DEFAULT 0,
  errors INTEGER NOT NULL DEFAULT 0,
  checked_at TIMESTAMPTZ NOT NULL,
  source_event_id TEXT NOT NULL
);
```

```sql
CREATE TABLE gonzbnet_range_completions (
  completion_id TEXT PRIMARY KEY,
  pool_id TEXT NOT NULL,
  node_id TEXT NOT NULL,
  group_name TEXT NOT NULL,
  provider_scope_hash TEXT,
  claim_id TEXT,
  range_start BIGINT,
  range_end BIGINT,
  window_start TIMESTAMPTZ,
  window_end TIMESTAMPTZ,
  completed_at TIMESTAMPTZ NOT NULL,
  articles_seen BIGINT,
  headers_processed BIGINT,
  release_cards_emitted INTEGER,
  manifests_emitted INTEGER,
  dedup_candidates_skipped INTEGER,
  error_count INTEGER,
  range_fingerprint TEXT,
  source_event_id TEXT NOT NULL
);

CREATE INDEX idx_range_completions_lookup
  ON gonzbnet_range_completions(pool_id, group_name, provider_scope_hash, completed_at DESC);
```

```sql
CREATE TABLE gonzbnet_scanner_heartbeats (
  heartbeat_id TEXT PRIMARY KEY,
  pool_id TEXT NOT NULL,
  node_id TEXT NOT NULL,
  active_claims JSONB NOT NULL DEFAULT '[]'::jsonb,
  queue_depth INTEGER,
  current_articles_per_minute DOUBLE PRECISION,
  status TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  source_event_id TEXT NOT NULL
);
```

```sql
CREATE TABLE gonzbnet_validation_attestations (
  attestation_id TEXT PRIMARY KEY,
  attestation_type TEXT NOT NULL,
  pool_id TEXT NOT NULL,
  release_id TEXT,
  manifest_id TEXT,
  author_node_id TEXT NOT NULL,
  validation_tier TEXT NOT NULL,
  checked_at TIMESTAMPTZ NOT NULL,
  articles_total INTEGER,
  articles_checked INTEGER,
  articles_available INTEGER,
  missing_articles INTEGER,
  files_sampled INTEGER,
  segments_sampled INTEGER,
  matched BOOLEAN,
  confidence DOUBLE PRECISION NOT NULL DEFAULT 0,
  provider_scope_hash TEXT,
  body JSONB NOT NULL DEFAULT '{}'::jsonb,
  source_event_id TEXT NOT NULL
);

CREATE INDEX idx_validation_attestations_manifest
  ON gonzbnet_validation_attestations(pool_id, manifest_id, checked_at DESC);
```

---

## 13. Federation Endpoint Additions

Coverage and validation events can flow through the normal inbox/outbox. These endpoints are optional convenience APIs for coordination and UI.

```text
GET  /gonzbnet/v1/coverage/groups?pool_id=<pool_id>
  Returns current local projection of group observations and coverage scores.

GET  /gonzbnet/v1/coverage/plan?pool_id=<pool_id>
  Returns active CoveragePlan and assignments visible to the requesting node.

GET  /gonzbnet/v1/coverage/work?pool_id=<pool_id>&role=scanner
  Returns suggested work for the requesting node, based on local policy and assignments.

POST /gonzbnet/v1/coverage/claim
  Accepts a signed RangeClaim or TimeWindowClaim event.

POST /gonzbnet/v1/coverage/checkpoint
  Accepts signed CoverageCheckpoint, RangeComplete, or RangeFailed events.

POST /gonzbnet/v1/validation/request
  Requests validation of a ReleaseCard or ResolutionManifest by a trusted validator node.

GET  /gonzbnet/v1/capabilities/nodes?pool_id=<pool_id>
  Returns known node capabilities visible to the requester.
```

Authorization rules:

```text
- Coverage endpoints require node-to-node authentication.
- A node can only see pool coverage data for pools where it is an active member.
- Work suggestions must respect pool role approvals.
- Validation requests must respect target node ValidatorCapacity and rate limits.
- Local users should access coverage dashboards through local admin UI/RBAC, not remote node APIs.
```

---

## 14. Validation-Only Operation

A validation-only node must be able to contribute without indexing.

Flow:

```text
1. Node joins pool with validator role.
2. Node receives ReleaseCard/ManifestAvailability/ResolutionManifest events through federation.
3. Node selects validation tasks based on capacity, pool priority, and missing attestation gaps.
4. Node checks metadata/article existence/checksum tier using its own provider.
5. Node emits ArticleAvailabilityAttestation, ChecksumAttestation, or HealthAttestation.
6. Node does not project releases into local user search unless aggregator/search is enabled.
```

Validation-only nodes should store only:

```text
- signed event metadata
- manifest IDs being validated
- article availability summaries
- attestation history
- local task state
```

They do not need to store:

```text
- user-facing local index rows
- downloaded payload files
- full article payloads
- local user search cache
```

---

## 15. Scan-Without-Index Operation

A contribution node may scan and publish release metadata without becoming a local indexer.

Flow:

```text
1. scanner=true, index_projection=false.
2. Node receives assigned group/range work.
3. Node scans NNTP headers and detects release candidates.
4. Node emits ReleaseCard events.
5. If manifest_builder=true, node emits/stores ResolutionManifest or ManifestAvailability.
6. Node does not expose these results to local Newznab users unless aggregator/search is enabled.
```

This mode is useful for people who want to help the pool but do not want to run a full searchable indexer.

---

## 16. Dedup-Aware Scanner Behavior

Before scanning, a scanner should consult local projected coverage state.

Pseudo-flow:

```text
candidate_work = scheduler.next_work(pool_id, node_id)

for work in candidate_work:
  if coverage.has_trusted_active_claim(work) and not work.assigned_to_me:
      skip_or_delay(work)
      continue

  if coverage.has_trusted_completion(work) and not work.requires_validation_overlap:
      skip(work)
      continue

  claim = create_signed_range_claim(work)
  publish(claim)

  while work not complete:
      scan_next_batch()
      publish_checkpoint_periodically()
      skip_candidates_already_seen_by_release_dedup()

  publish_range_complete()
```

Do not let dedup prevent validation overlap:

```text
primary scanners avoid duplicate full scanning.
validators intentionally rescan/check samples.
manifest builders may revisit known releases to build missing manifests.
health checkers revisit known manifests over time.
```

---

## 17. RBAC and Pool Role Additions

Local user RBAC remains home-node-only. Add admin permissions for optional modules:

```text
gonzbnet.admin.coverage
  View/edit coverage plans and assignments.

gonzbnet.admin.scanner
  Enable/disable scanner, configure local scan limits.

gonzbnet.admin.validator
  Enable/disable validation module, configure validation tiers.

gonzbnet.admin.scheduler
  Manage work scheduling and claim behavior.

gonzbnet.view.coverage
  View local coverage dashboard.
```

Pool membership approval should include allowed node roles:

```json
{
  "type": "PoolMemberApproved",
  "pool_id": "pool.discord.private",
  "subject_node_id": "ed25519:node-a...",
  "role": "member",
  "allowed_capabilities": [
    "consumer",
    "scanner",
    "manifest_builder",
    "validator"
  ],
  "limits": {
    "max_claims": 4,
    "max_groups": 25,
    "can_serve_manifests": true,
    "can_propose_coverage_plan": false
  },
  "approvals_required": 2,
  "approvals": [
    { "node_id": "ed25519:admin1...", "signature": "..." },
    { "node_id": "ed25519:admin2...", "signature": "..." }
  ]
}
```

Remote nodes authorize contribution actions by node role:

```text
ReleaseCard accepted only if author has scanner or indexer publishing role.
ResolutionManifest accepted only if author has manifest_builder or manifest_cache role.
ArticleAvailabilityAttestation accepted only if author has validator or health_checker role.
RangeClaim accepted only if author has scanner role.
CoveragePlan accepted only if author has coverage_coordinator role or enough admin signatures.
```

---

## 18. Configuration Additions

Add these configuration keys.

```env
# High-level module switches
GONZBNET_CONSUMER_ENABLED=true
GONZBNET_SCANNER_ENABLED=false
GONZBNET_INDEX_PROJECTION_ENABLED=true
GONZBNET_MANIFEST_BUILDER_ENABLED=false
GONZBNET_MANIFEST_CACHE_ENABLED=true
GONZBNET_VALIDATOR_ENABLED=false
GONZBNET_HEALTH_CHECKER_ENABLED=false
GONZBNET_COVERAGE_ENABLED=true
GONZBNET_SCHEDULER_ENABLED=true
GONZBNET_RELAY_ENABLED=false

# Scanner behavior
GONZBNET_SCANNER_PUBLISH_RELEASE_CARDS=true
GONZBNET_SCANNER_PUBLISH_MANIFEST_AVAILABILITY=true
GONZBNET_SCANNER_PROJECT_TO_LOCAL_INDEX=false
GONZBNET_SCANNER_MAX_GROUPS=25
GONZBNET_SCANNER_MAX_ARTICLES_PER_HOUR=250000
GONZBNET_SCANNER_CLAIM_TTL_MINUTES=30
GONZBNET_SCANNER_CHECKPOINT_INTERVAL_SECONDS=300
GONZBNET_SCANNER_RESPECT_REMOTE_CLAIMS=true
GONZBNET_SCANNER_ALLOW_UNASSIGNED_WORK=false

# Coverage/dedup
GONZBNET_COVERAGE_MODE=manual
GONZBNET_COVERAGE_MIN_TRUST_FOR_CLAIM=0.65
GONZBNET_COVERAGE_VALIDATION_OVERLAP_PERCENT=10
GONZBNET_COVERAGE_STALE_CLAIM_PENALTY=true
GONZBNET_COVERAGE_PROVIDER_SCOPE_MODE=hash_only

# Validation
GONZBNET_VALIDATION_TIERS=metadata,article_stat,segment_stat
GONZBNET_VALIDATION_MAX_MANIFESTS_PER_HOUR=500
GONZBNET_VALIDATION_SAMPLE_PERCENT=10
GONZBNET_VALIDATION_ALLOW_SAMPLE_PAYLOAD_FETCH=false
GONZBNET_VALIDATION_ALLOW_PAR2_VALIDATION=false
GONZBNET_VALIDATION_PUBLISH_PROVIDER_SCOPE_HASH=true

# Manifest caching
GONZBNET_MANIFEST_CACHE_MAX_BYTES=10737418240
GONZBNET_MANIFEST_CACHE_TTL_DAYS=90
GONZBNET_MANIFEST_CACHE_SERVE_TO_TRUSTED_POOLS=true
```

---

## 19. Implementation Phases for This Addendum

Add these phases after core signed federation and before advanced gossip.

### Phase A: capability registry and module switches

Implement:

```text
- NodeProfile capability fields.
- federation_node_capabilities table.
- module enable/disable config.
- no-op behavior for disabled modules.
- pool-approved allowed_capabilities.
```

Acceptance criteria:

```text
[ ] A node can advertise scanner=false, validator=true, etc.
[ ] Disabled scanner does not prevent remote search/manifest fetch.
[ ] Disabled index_projection allows scan/publish without local searchable index exposure.
[ ] Pool policy rejects events from nodes lacking the required role.
```

### Phase B: validation-only contribution

Implement:

```text
- ValidatorCapacity event.
- ArticleAvailabilityAttestation.
- ChecksumAttestation schema, but sample checksum validation can be feature-flagged off.
- validation task queue.
- validation scoring projection.
```

Acceptance criteria:

```text
[ ] A validator-only node can validate remote manifests without local indexing.
[ ] A validator-only node emits signed attestations.
[ ] Attestations update availability/trust scoring on consuming nodes.
[ ] No user search/index module is required for validation-only operation.
```

### Phase C: scan-without-index contribution

Implement:

```text
- scanner=true with index_projection=false.
- ReleaseCard publication from scan output.
- optional ManifestAvailability publication.
- local user-facing index remains untouched when projection is disabled.
```

Acceptance criteria:

```text
[ ] A contribution node can scan and publish ReleaseCards.
[ ] Local Newznab search does not show those releases unless projection/search is enabled.
[ ] Remote trusted nodes can receive and use those ReleaseCards.
```

### Phase D: coverage events and manual assignments

Implement:

```text
- ScannerCapacity.
- GroupObservation.
- CoveragePlan.
- CoverageAssignment.
- RangeClaim / TimeWindowClaim.
- CoverageCheckpoint.
- RangeComplete / RangeFailed.
- admin API/UI for manual assignment.
```

Acceptance criteria:

```text
[ ] Pool admins can assign groups/ranges to scanner nodes.
[ ] Scanner nodes claim assigned work.
[ ] Other scanner nodes can see trusted active claims and avoid duplicate work.
[ ] Completed ranges are visible in coverage dashboard.
[ ] Claims expire and stale claims are detected.
```

### Phase E: dedup-aware local scheduler

Implement:

```text
- local work suggestion endpoint.
- claim/completion lookup.
- skip duplicate primary work.
- preserve validation overlap.
- coverage score computation.
```

Acceptance criteria:

```text
[ ] Scanner avoids trusted active/completed work by default.
[ ] Validator can still intentionally overlap completed work.
[ ] Coverage dashboard shows gaps and duplicate work.
[ ] Low-trust claims do not block high-priority assignments.
```

### Phase F: automated coverage improvements

Optional after v1:

```text
- weighted scheduler.
- deterministic distributed assignment.
- rendezvous-hashing assignment.
- seen-set summaries.
- automatic failover for stale claims.
```

---

## 20. Codex Addendum Prompt

Use this prompt together with the original GoNZBNet implementation spec.

```text
Extend GoNZBNet with capability-based optional participation modules and distributed coverage coordination.

Core requirement:
Not every node needs to scan, index, validate, build manifests, relay, cache, or consume. Implement GoNZBNet so each module can be enabled/disabled independently and so other modules continue working when optional modules are disabled.

Add node participation roles:
- consumer
- scanner
- indexer
- manifest_builder
- validator
- health_checker
- relay
- manifest_cache
- coverage_coordinator
- admin

Scanner and indexer are different. A node may scan NNTP headers and publish ReleaseCards without exposing those releases through its local user-facing Newznab index. Implement scanner=true with index_projection=false.

Validation-only operation must be supported. A node may validate remote ReleaseCards/ResolutionManifests using its own NNTP provider and emit signed ArticleAvailabilityAttestation, ChecksumAttestation, or HealthAttestation events without maintaining a local searchable index.

Add capability advertisement to NodeProfile and store it in PostgreSQL. Pool membership approvals must include allowed_capabilities and limits. Event acceptance must check that the author node is approved for the relevant role.

Add coverage/dedup event types:
- ScannerCapacity
- ValidatorCapacity
- GroupObservation
- CoveragePlan
- CoverageAssignment
- RangeClaim
- TimeWindowClaim
- CoverageCheckpoint
- RangeComplete
- RangeFailed
- ScannerHeartbeat
- ArticleAvailabilityAttestation
- ChecksumAttestation

Implement distributed coverage coordination:
- Nodes publish group observations and scanner/validator capacity.
- Pool admins or coordinator nodes publish coverage plans/assignments.
- Scanner nodes claim ranges/time windows using signed leases.
- Other scanner nodes avoid trusted active/completed claims to reduce duplicate scraping.
- Validators and health checkers may intentionally overlap scans for verification.
- Claims are advisory, expire automatically, and only suppress duplicate work when the claimant is trusted enough by pool policy.
- Article ranges are provider-local; cross-provider dedup must use Message-ID, subject/file fingerprints, timestamps, and manifest/release fingerprints.

Add PostgreSQL tables for node capabilities, pool roles, group observations, coverage plans, assignments, range claims, checkpoints, completions, scanner heartbeats, and validation attestations.

Add config flags for scanner, index projection, manifest builder, manifest cache, validator, health checker, coverage, scheduler, and relay. Disabled modules must produce no-op behavior rather than breaking the node.

Add admin UI/API support for:
- viewing node capabilities
- assigning groups/ranges
- viewing active claims
- viewing completed ranges
- viewing coverage score
- identifying stale claims
- viewing validation gaps

Implement in phases:
A. capability registry and module switches
B. validation-only contribution
C. scan-without-index contribution
D. coverage events and manual assignments
E. dedup-aware local scheduler
F. optional automated coverage improvements
```

---

## 21. Final Addendum Checklist

```text
[ ] Optional modules can be enabled/disabled independently.
[ ] Scanner can run without local index projection.
[ ] Validator can run without scanner or indexer.
[ ] Consumer/search can run without scanner or validator.
[ ] NodeProfile advertises capability and capacity.
[ ] Pool approvals include allowed_capabilities.
[ ] Event acceptance checks required node role.
[ ] GroupObservation events populate a pool group catalog.
[ ] CoveragePlan and CoverageAssignment can be created manually.
[ ] RangeClaim/TimeWindowClaim leases prevent duplicate primary scanning.
[ ] Claims expire and stale claims are penalized.
[ ] RangeComplete suppresses duplicate primary work.
[ ] Validators can intentionally overlap completed work.
[ ] ArticleAvailabilityAttestation updates availability scoring.
[ ] ChecksumAttestation is supported as a feature-flagged validation tier.
[ ] Coverage dashboard shows gaps, stale claims, active work, and duplicate work.
[ ] No remote node receives local user identity, API keys, search history, download history, or NNTP credentials.
```
