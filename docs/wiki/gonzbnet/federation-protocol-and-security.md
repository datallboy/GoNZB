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
- bounded yEnc and missing-segment evidence queries;
- WebSocket gossip when enabled.

Admission has a deliberately narrow candidate-authenticated surface for join
submission, approval/rejection fragments, and status polling. It does not grant
normal pool outbox access before final membership is verified.

Handshake requests are signed and must carry the supported protocol version, a
16-byte random nonce, and a creation time inside the configured tolerance.
Identity registration and nonce consumption commit atomically, so a replay or
persistence failure cannot leave half of a handshake stored. Private-node or
private-pool admission also requires the signed invitation to accompany the
join submission; knowing a hidden endpoint is not an invitation.

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

These signatures authenticate the requesting node and protect request
integrity; they are not PGP-style encryption. HTTPS supplies confidentiality
and server transport authentication. A `ManifestRequest` is signed by the
requesting node, binds its declared `requesting_node_id` to that signature, and
names the exact manifest, release, and pool. The response echoes the random
request ID so a valid response cannot be substituted between concurrent grabs.

Discovery metadata remains public, but event streams, pool membership, pool
checkpoints, manifests, coverage mutation, and optional peer exchange require
the appropriate authenticated node and pool relationship.

Binary evidence routes are `POST /evidence/yenc/query` and
`POST /evidence/segments/query`. In addition to signed-request and replay
checks, both require an active pool membership carrying
`binary_evidence_exchange`, an enabled pool policy, and the serving node's
local opt-in. Responses are canonicalized Ed25519-signed bundles bound to the
pool, request ID, recipient, source node, creation time, and expiry. A responder
serves only evidence acquired locally through XOVER/BODY; imported evidence is
not relayed. Missing-segment requests require at least one exact Message-ID
anchor, and the responder serves a portable match ID only when it resolves to a
single local binary.

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

Resolution manifests use their dedicated authenticated fetch endpoint and are
not part of the general relay event stream. Relaying a release card therefore
does not make an unreachable manifest source reachable. Binary-evidence queries
also require direct reachability to the selected authorized peer.

## Release And Manifest Security

Release identities and manifest identities are recomputed from canonical
content during typed validation. Cards and manifests reject local-only fields,
invalid source/pool relationships, malformed message IDs, negative sizes, and
unsupported policy values.

Manifest resolution authorizes the local role before cache access. The home
aggregator selects an eligible advertised source, sends the signed
`ManifestRequest`, verifies the returned `ResolutionManifest` event, and then
generates the NZB locally. A peer does not return an unsigned pre-generated NZB.
Resolution requires all of these bindings to agree:

- response request ID;
- requested, advertised, and signed release and manifest IDs;
- selected pool and the signed event's single pool;
- selected serving node, signed author, current membership/capabilities, node
  block/fork state, publication state, trust threshold, and tombstones.

The complete typed event-body validator runs before the manifest is cached.
The cache record is also required to match its stored signed source event.
PostgreSQL NZB bytes are hashed on every read; a mismatch is regenerated
deterministically from the verified signed manifest or failed closed when that
provenance is unavailable. The optional filesystem NZB cache is checked against
the PostgreSQL checksum before use. These checks are local and add no federation
messages or background polling.

`ManifestAvailability` statements update only their matching source, pool,
release, and manifest. Search, get, and cache queries apply current source
eligibility at read time, so blocking a node, revoking its membership,
withdrawing its source, disabling a pool, or activating a relevant tombstone
suppresses it without rewriting signed history. Search/get also compare the
projected ReleaseCard metadata with its accepted signed source event; a locally
modified title or detail is excluded and reported as a projection mismatch.

Signatures establish who published exact bytes; they do not establish that a
title is truthful, Message-IDs reference desirable content, or articles are
malware-free. A malicious authorized publisher can sign internally consistent
poison. Pool capability grants, independent validation, trust scores,
moderation, author-scoped withdrawal, member revocation, and local blocking are
the policy controls for that threat.

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
result selections, grabs, external download-client activity, Usenet
credentials, or raw provider account identity.

Search always uses the home node's local federated projection. Live remote
querying and user-context forwarding are rejected by configuration validation.
Missing-part lists and Message-ID evidence use targeted authenticated requests;
they are not published into the append-only event stream or normal logs.

## Transport Limits

Production peers use HTTPS. Plain HTTP is limited to loopback/private test use
when explicitly allowed. The HTTP layer enforces configured maximum event,
manifest, and batch sizes, request rate, manifest fetch timeout, timestamp
tolerance, maximum event age, and nonce TTL before expensive processing.

Peer TLS trust still belongs to the deployment: use valid public certificates
or an appropriately managed private CA. `allow_insecure_peer_http` is not a
substitute for production certificate configuration.

All federation routes—including discovery and handshake—share the source-IP
flood limiter before an Authorization header is trusted. Signed request
verification still identifies the node for authorization and audit. WebSocket
gossip verifies that request before upgrading, caps each message using the
configured event/batch limits, applies read/write deadlines, and enforces a
per-connection message budget.
