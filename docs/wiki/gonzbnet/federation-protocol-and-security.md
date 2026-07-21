# Federation Protocol And Security

## Protocol Surface

The default protocol base is `/gonzbnet/v1`. `/.well-known/gonzbnet` advertises
the base URL and supported specification version.

Public discovery routes:

- `GET /.well-known/gonzbnet`
- `GET /gonzbnet/v1/node`
- `GET /gonzbnet/v1/caps`
- `POST /gonzbnet/v1/handshake`
- `GET /gonzbnet/v1/pools` subject to pool visibility and invitation rules

Node-authenticated federation routes include:

- outbox, direct event, inbox, and bounded event-batch exchange;
- manifest request and retrieval;
- pool membership and checkpoint reads;
- coverage plans, work, claims, and checkpoints;
- validation requests, node capabilities, and optional peer exchange;
- WebSocket gossip when enabled.

Admission has a deliberately narrow candidate-authenticated surface for join
submission, approval/rejection fragments, and status polling. It does not grant
normal pool outbox access before final membership is verified.

Local administration uses `/api/v1/admin/gonzbnet`. These routes use GoNZB's
local session/API-key authentication, granular RBAC, CSRF protection, and audit
logging. Local credentials never authenticate federation requests.

## Identity, Canonicalization, And Signatures

Each node has a persistent Ed25519 key. Its deterministic node ID binds protocol
identity to the public key. RFC 8785 JSON Canonicalization Scheme bytes are the
single basis for body hashes, stable IDs, event IDs, request signatures,
approval fragments, and checkpoints.

Raw JSON is checked for duplicate object names before decoding, including names
that become equal after escape processing. Signed event verification then:

1. canonicalizes the raw body and verifies `body_hash`;
2. canonicalizes the unsigned envelope and verifies `event_id`;
3. resolves the author's known public key;
4. verifies the Ed25519 signature;
5. validates typed body and envelope agreement.

This ordering prevents alternate JSON encodings or ambiguous duplicate fields
from changing the meaning of signed data.

## Signed Requests And Replay Protection

Protected HTTP requests sign the method, route, time, nonce, and relevant body
or query material with the node identity. The receiver enforces timestamp
tolerance, maximum event age, nonce lifetime, body limits, and request rate.
Nonces prevent a valid signed request from being replayed within its acceptance
window.

Discovery metadata remains public, but event streams, pool membership, pool
checkpoints, manifests, coverage mutation, and optional peer exchange require
the appropriate authenticated node and pool relationship.

## Event Log And Chain Continuity

Every event carries a positive author sequence and the previous event ID.
PostgreSQL serializes append decisions per author and checks same-sequence
conflicts plus known predecessor and successor links.

Out-of-order delivery is allowed. A missing predecessor opens a sequence-gap
diagnostic that closes when the correct event arrives. A conflicting sequence
or known link mismatch is retained as fork evidence, recorded in rejected-event
diagnostics, and excluded from typed projections. The canonical accepted branch
remains append-only; fork resolution is an operator decision.

Duplicate delivery is idempotent. Accepted append and required projection are
one transaction, so a projection failure cannot leave a newly accepted event
without its corresponding state.

## Pools And Authorization

A pool is bound to `(pool_id, genesis_event_id)`. Its signed genesis defines
initial administrators, witnesses, policy, visibility, admission mode, and
thresholds. A different genesis cannot reuse an existing pool ID.

Protocol-v1 protected events target exactly one pool. Receive and delivery
paths enforce:

- known pool identity;
- active author and destination membership;
- allowed event types and required capability grants;
- local block, moderation, tombstone, and minimum-trust policy;
- pool-specific role access for local search, get, and manifest resolution.

Membership in one pool grants nothing in another. Multi-pool publication signs
a separate event per pool. Stable content IDs may be shared, but event evidence,
source provenance, authorization, and delivery remain pool-scoped.

## Discovery And Admission

First contact uses an explicit HTTPS address or signed `gonzbnet://` invitation:

1. The candidate verifies well-known metadata, node profile, capabilities, and
   the pool's signed genesis.
2. It signs a `PoolJoinRequest` and submits it to the contacted member.
3. Members distribute the request to active pool administrators.
4. Administrators sign canonical approval or rejection fragments.
5. The relay aggregates the configured independent threshold into a final
   signed governance event.
6. The candidate polls admission status, verifies the complete trust bundle,
   projects its membership, and learns approved member endpoints.

The relay has no approval authority merely because it transported the request.
Duplicate join and approval operations are idempotent. Private pool descriptors
are revealed only by a valid, unexpired invitation whose signer is still an
active administrator and whose relay URL matches the contacted node.

A revoked member loses ordinary pool access. It may retrieve only the signed
revocation addressed to itself so it can converge on its state.

## Synchronization And Delivery

Pull performs discovery and handshake, then signs an outbox request and feeds
the complete eligible event stream through normal receive validation. Push and
WebSocket gossip use the destination identity learned from its signed profile.

Every delivery query intersects event visibility with the destination node's
active memberships. A configured peer is only a transport destination; it is
not automatically trusted and cannot receive pools it has not joined. Delivery
cursors make repeated synchronization incremental and idempotent.

## Release And Manifest Security

Release identities and manifest identities are recomputed from canonical
content during typed validation. Cards and manifests reject local-only fields,
invalid source/pool relationships, malformed message IDs, negative sizes, and
unsupported policy values.

Manifest resolution authorizes the local role before cache access. Remote fetch
authenticates only the home node, verifies the signed manifest response, and
applies configured response-size and timeout limits. `ManifestAvailability`
statements update only their matching source, pool, release, and manifest.

Local Newznab API keys and sessions are not included in federation events,
requests, or logs. The E2E suite explicitly checks that a generated local API
token does not appear in any node's event store or logs.

## Event Families

The implementation accepts and projects these broad families:

- node profile, capability, and key/governance state;
- pool genesis, join, approval, rejection, revocation, policy, and checkpoint;
- ReleaseCard, ResolutionManifest, and ManifestAvailability;
- validation result, health attestation, trust attestation, and tombstone;
- scanner capacity, heartbeat, group observation, coverage plan, assignment,
  claim, completion, failure, and checkpoint;
- scan output and related release-source evidence.

The typed validator is the authoritative list for accepted fields. Adding an
event requires coordinated validation, authorization, storage projection,
delivery, diagnostics, and tests; documenting a type alone does not enable it.

## Privacy Boundary

Federation may contain release metadata, article Message-IDs, signed node and
pool governance, capability, provider-scope hashes, and operational evidence.
It must not contain local usernames, session cookies, API keys, searches,
result selections, grabs, download history, Usenet credentials, or raw provider
account identity.

Search always uses the home node's local federated projection. Live remote
querying and user-context forwarding are rejected by configuration validation.

## Transport Limits

Production peers use HTTPS. Plain HTTP is limited to loopback/private test use
when explicitly allowed. The HTTP layer enforces configured maximum event,
manifest, and batch sizes, request rate, manifest fetch timeout, timestamp
tolerance, maximum event age, and nonce TTL before expensive processing.

Peer TLS trust still belongs to the deployment: use valid public certificates
or an appropriately managed private CA. `allow_insecure_peer_http` is not a
substitute for production certificate configuration.
