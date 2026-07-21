# GoNZBNet Direct Discovery, Pool Admission, and Multi-Pool Isolation

Status: completed and verified on 2026-07-13

Primary protocol reference: `docs/GoNZBNet_Codex_Implementation_Spec.md`

This document defines the next focused GoNZBNet work. It replaces completed
phase and hardening instructions that previously lived in `docs/active`.
Completed behavior remains documented under `docs/wiki/gonzbnet`.

## Objective

Allow a GoNZBNet node to start as an independent node without configured seed
peers, contact any reachable member of a pool by HTTPS address or signed
invitation, request admission, collect the pool's required administrator
approvals, verify the replicated pool trust state, and begin pool-scoped
synchronization.

A node may belong to multiple pools. Membership, capabilities, metadata,
moderation, manifests, synchronization, and local RBAC access must remain
isolated by pool.

## Product Direction

The implementation is multi-pool-correct internally and single-pool-first in
the administrator experience. Multi-pool support is a protocol and
authorization requirement, not an invitation to expose every protocol control
in the normal setup flow.

The primary administration workflow contains only:

```text
Create Pool
Join Pool
Approve or Reject Join Request
View Pool Status
```

Identity fingerprints, genesis events, checkpoints, approval fragments,
delivery cursors, and trust scores remain available under advanced diagnostics.
They are verified automatically and are not required knowledge for normal pool
operation.

Default pool behavior is intentionally small-deployment friendly:

```text
one initial administrator
membership approval threshold 1
unlisted visibility
approval-required admission
automatic synchronization after approval
```

Administrators may later add administrators and increase quorum thresholds
without replacing the pool identity. Multi-administrator quorum is an advanced
robustness feature, not a prerequisite for creating a usable pool.

## Confirmed Existing Foundation

The current implementation already provides:

- persistent Ed25519 node identity and deterministic node IDs;
- signed events with `pool_ids`, visibility, sequence, and predecessor fields;
- `trust_pools` keyed by `pool_id`;
- `pool_members` keyed by `(pool_id, node_id, role)`;
- pool-scoped release sources, manifests, health, coverage, reputation,
  moderation, peer cursors, and local RBAC grants;
- signed `PoolGenesis`, `PoolJoinRequest`, and aggregate
  `PoolMemberApproved` event types;
- HTTPS discovery through `/.well-known/gonzbnet`, node profiles,
  capabilities, and signed handshakes;
- outbound filtering based on the destination node's active pool membership;
- local-cache Newznab search rather than live cross-node user searches.

The schema can represent one node in multiple pools today. Runtime behavior is
not fully multi-pool because publishers, scanner coordination, reassignment,
and several admin defaults use one configured `local_pool_id`. ReleaseCard
projection also currently selects only the first event pool.

The existing E2E harness configures pool membership directly in each database.
It does not prove pre-membership request delivery, distributed approval
collection, trust-bundle verification, or automatic activation of a joining
node.

## Protocol Decisions

### Standalone Startup

Enabling GoNZBNet creates or loads the local node identity but does not require
a seed URL, peer, or pool. No external network request is required merely to
start the module.

### First Contact

First contact accepts:

- an HTTPS hostname;
- an HTTPS hostname and port;
- a complete HTTPS URL;
- a signed pool invitation containing endpoints and identity fingerprints.

A hostname is normalized through `/.well-known/gonzbnet`, which provides the
actual protocol base path. HTTP remains limited to explicit insecure
development mode on loopback or private networks.

A public key or node ID alone cannot locate a node without an existing
node-to-endpoint mapping. It may be used as an expected identity fingerprint
when contacting an address. A node that already has pool peers may ask those
peers for a known endpoint for a node ID, subject to pool visibility rules.

### Node Visibility

Discovery visibility and pool governance are separate settings:

- `private`: no directory or peer-exchange advertisement; direct invitation
  or explicit address only;
- `unlisted`: reachable by explicit address but not advertised;
- `pool`: endpoint may be exchanged only among active members of a shared
  pool;
- `public`: eligible for optional public discovery mechanisms.

The initial implementation covers `private`, `unlisted`, and `pool`.
A global public directory, DHT, mDNS, NAT traversal, and built-in public
rendezvous infrastructure remain out of scope.

Pool creation, admission relay, and unsolicited join requests are independent
controls. A node can participate in pools without creating pools or acting as
an admission relay.

The normal UI presents `unlisted` as the default and describes `private` only
when an administrator explicitly restricts reachability. The `pool` visibility
mode is automatic when members exchange reachable endpoints inside an active
pool. Raw visibility settings belong in advanced configuration.

### Pool Identity

A pool is a replicated trust domain, not a server and not a shared private key.

The authoritative identity is the immutable pair:

```text
(pool_id, genesis_event_id)
```

The canonical `PoolGenesis` hash is the pool fingerprint. Display names and
human-readable pool IDs are not independently authoritative. A node must reject
a different genesis event that attempts to reuse an already-bound `pool_id`.

Invitations and admission responses include both the pool ID and genesis
fingerprint.

### Admission Through Any Member

A candidate may contact any active member that advertises admission relay
support. The contacted member does not become the pool owner and does not gain
approval authority.

The admission flow is:

1. Candidate and relay complete a signed node handshake.
2. Relay returns the selected pool's genesis, policy, current checkpoint,
   administrator identities, and sufficient signed history to verify them.
3. Candidate verifies or explicitly pins the pool fingerprint.
4. Candidate signs a `PoolJoinRequest`.
5. Relay accepts the request through a narrowly scoped pre-membership admission
   path and distributes it to current pool administrators.
6. Authorized administrators produce canonical approval signatures.
7. The relay, or another active member with the same approval fragments,
   assembles the existing aggregate `PoolMemberApproved` event.
8. All existing members validate and project the approval.
9. Candidate receives the aggregate approval and current trust bundle through
   the admission path, validates them locally, and becomes active.
10. Normal inbox, outbox, push, pull, and gossip authorization begins only
    after activation.

Any active member may relay a request when policy permits. Only nodes that are
active administrators in the referenced pool may approve it. The genesis
author has no permanent owner privilege unless it remains an administrator
under current pool governance.

Approval fragments are signed protocol messages associated with the join
request. They are not accepted metadata events by themselves. The final
`PoolMemberApproved` event remains the append-only governance record required
by the current specification.

### Multiple Pools

A single node identity may have independent memberships and roles in any number
of pools. Capabilities are granted per membership, even though the public node
profile advertises the node's overall technical capabilities.

For example:

```text
Pool P1: A, B, C
Pool P2: C, D
```

C can synchronize P1 with A and B and P2 with D. A and B cannot receive,
request, or search P2 metadata merely because C belongs to both pools. D cannot
receive P1 metadata.

For protocol v1, each protected event must target exactly one pool. Publishing
the same locally owned release to two pools creates two signed events with the
same stable release and manifest IDs but different pool scope. This avoids
ambiguous projection and authorization behavior while preserving the envelope's
`pool_ids` field for compatibility.

A bridge node must never automatically cross-post, relay, resolve, or serve
metadata from one pool into another. In particular:

- a ReleaseCard received in P1 is not republished into P2;
- a manifest cached under P1 is not served to P2 members;
- P1 health attestations, coverage state, trust, and tombstones remain in P1;
- peer exchange reveals only endpoints allowed by the shared pool;
- local Newznab results may combine pools only according to local RBAC grants.

An authorized member can always copy information outside the software, so the
protocol prevents accidental and unauthorized software bridging rather than
claiming cryptographic control over a malicious operator.

The first usable release does not require administrators to configure every
module separately for every pool. A joining node proposes capabilities from
its enabled local modules, and the approval screen applies a recommended grant.
Advanced administrators can reduce that grant. A node can therefore be an
indexer in one pool and a consumer in another without making the common
single-pool setup verbose.

### Event Chain Privacy

The current event chain is global per author. A member receiving only one of
C's pools may observe sequence gaps and opaque predecessor event IDs from C's
other pool. It must not be able to fetch those predecessor events without
membership in their pool.

For protocol v1, sequence gaps caused by pool filtering are expected and do not
authorize predecessor retrieval. Tests must prove cross-pool event lookup is
denied. Pool-scoped chains can be considered in a future protocol version if
hiding event counts and opaque predecessor references becomes a requirement.

## Required Implementation Work

Implement the work in this order. Each step must preserve the simple setup
workflow even when the underlying protocol supports stronger governance.

1. Direct address and invitation contact with no startup seed requirement.
2. One-admin, threshold-one pool creation and admission end to end.
3. Strict pool isolation and two-pool tests.
4. Multi-admin approval aggregation using the same UI workflow.
5. Advanced visibility, capability, and governance controls.

### Configuration and Profiles

- Make configured peers optional for standalone startup.
- Replace singular publishing assumptions with explicit enabled pool
  selection.
- Add node visibility and admission controls.
- Advertise admission relay support and joinable public pools truthfully.
- Preserve the current local-only authentication and runtime-settings model.
- Provide a compatibility path for `local_pool_id` while migrating workers to
  a pool list or store-driven active-membership selection.

Proposed settings:

```yaml
gonzbnet:
  visibility: unlisted
  allow_pool_creation: true
  allow_join_requests: false
  admission_relay_enabled: false
  publish_pool_ids: []
```

These are advanced/bootstrap settings. Creating, joining, and approving pools
must be possible from the local admin UI without hand-editing YAML. An empty
`publish_pool_ids` value means the runtime derives eligible pools from active
local memberships and per-pool capability grants; it does not mean publish to
unknown or unapproved pools.

Exact names should follow existing runtime settings conventions and must be
editable through the existing local admin settings path where appropriate.

### Pool Locator and Invitation Types

Define strongly typed protocol objects for:

- normalized node locator;
- expected node identity fingerprint;
- signed pool invitation;
- admission pool descriptor;
- verifiable pool trust bundle;
- admission status response;
- administrator approval fragment.

Invitation signatures, expiration, endpoint validation, and canonical encoding
must use existing GoNZBNet identity and canonical JSON packages.

### Admission Persistence

Add persistent state for:

- pending admission requests;
- approval fragments keyed by pool, proposal, and administrator;
- contacted relay and candidate endpoint;
- status, expiration, rejection reason, and finalized approval event;
- idempotent retry and replay protection.

The append-only federation log remains authoritative for finalized
`PoolJoinRequest` and `PoolMemberApproved` events. Pending admission state is
operational state and must not silently grant membership.

### Admission Transport

Add a narrow node-to-node admission API that permits only:

- handshake and candidate profile verification;
- pool descriptor and trust-bundle retrieval;
- signed join request submission;
- signed approval-fragment exchange between current administrators;
- candidate polling or retrieval of the finalized approval.

Admission endpoints must not expose normal pool outbox events to a candidate.
They require body limits, timestamp windows, nonce/replay protection, rate
limits, TLS policy, and normalized rejection responses.

Normal inbox authorization must not be broadly weakened to accommodate
candidates. The pre-membership exception applies only to valid admission
objects and the referenced pool.

### Pool Governance Validation

- Bind every local `pool_id` permanently to one genesis event.
- Verify administrator authority against the accepted pool history.
- Verify unique approval signers and the configured membership threshold.
- Canonically order approval fragments before final event construction.
- Make finalization idempotent when more than one relay attempts it.
- Deliver the finalized approval and trust bundle to the candidate before
  requiring normal membership authorization.
- Ensure revocation takes effect independently in every pool.

### Multi-Pool Runtime

- Select active pools from stored local memberships instead of assuming one
  `local_pool_id`.
- Make publisher, manifest builder/cache, validator, scanner, coverage,
  scheduler, reassignment, health, and moderation operations explicitly
  pool-scoped.
- Emit separate events per pool.
- Project and authorize every event against its one v1 pool.
- Track peer synchronization and delivery independently per shared pool.
- Never infer access from membership in any unrelated pool.
- Reject unknown pools rather than treating an event with no recognized pool
  as authorized.
- Preserve per-pool source provenance when identical release or manifest IDs
  appear in several pools.

### Local Admin API and UI

Add workflows to:

- connect to a node by address and optional expected fingerprint;
- inspect and confirm the remote identity;
- inspect joinable pools and their genesis fingerprints;
- create or import a signed invitation;
- submit and monitor a join request;
- review pending requests on any admission relay;
- sign an approval as a locally authorized pool administrator;
- show collected approvals and threshold progress;
- list all local pool memberships, roles, capabilities, peers, and status;
- leave, disable, or revoke membership in one pool without affecting others.

No local user identity or session data is included in federation messages.

The default pool list should summarize only the information needed to operate
the node:

```text
pool name
membership status
local role/capabilities
member count
connection/synchronization health
pending join requests
```

The normal Join Pool form accepts one value: an invitation or node address.
When an address advertises more than one joinable pool, the administrator picks
the pool from a list. Expected fingerprints, trust history, and raw signed
objects are optional advanced details.

### E2E Harness

Extend the harness with Node D and stop directly inserting D's membership.

Required scenarios:

1. D starts with no configured peers and a distinct persistent identity.
2. D contacts B by explicit address and requests membership in P1.
3. The configured P1 administrator threshold approves D.
4. A, B, C, and D converge on the same signed membership event.
5. D receives P1 ReleaseCards and resolves a manifest through local Newznab.
6. D creates P2 and invites C.
7. C becomes active in both P1 and P2 with different capabilities.
8. A and B cannot read, search, fetch, or receive P2 events.
9. D cannot read, search, fetch, or receive P1 events that are not addressed to
   its P1 membership.
10. Removing C from P2 does not change C's P1 membership.
11. Duplicate requests, approvals, and retries remain idempotent.
12. Restarting every node preserves identity, memberships, pending admission,
    and synchronization cursors.
13. Node-local users, API keys, searches, grabs, and download history never
    appear in federation requests, events, or logs.

## Security Invariants

- Federation authenticates nodes, never remote users.
- Pool discovery does not grant pool membership.
- Pool membership in one pool grants no rights in another.
- A relay cannot approve unless it is an authorized pool administrator.
- A pool has no shared private signing key.
- Candidates cannot access protected outboxes before approval.
- Unknown or conflicting pool genesis state fails closed.
- SSRF protections apply when resolving administrator-supplied node addresses.
- HTTPS certificate validation remains enabled outside explicit local
  development mode.
- Search remains local-cache-first.
- Manifest requests contain node and manifest context only, never local user
  context.

## Completion Criteria

This work is complete when the four-node E2E scenarios pass without direct
database membership setup, all unit and PostgreSQL federation tests pass, the
admin UI can perform the full admission workflow, and the implemented behavior
is documented under `docs/wiki/gonzbnet`.

A first-time administrator must be able to complete the following without
editing YAML, entering peer records, understanding event types, or manually
copying public keys:

```text
enable GoNZBNet
create a pool or paste an invitation/address
approve a pending node
observe synchronized releases
```

## Explicitly Deferred

The following are not required by this active work:

- a global public node directory;
- built-in public rendezvous infrastructure;
- DHT discovery, mDNS, or automatic NAT traversal;
- linked, nested, or inherited pools;
- automatic cross-pool metadata bridges;
- public-key-only first-contact discovery without a known endpoint;
- pool-scoped replacement for the v1 global author event chain;
- a separate relay microservice.

Any protocol-level departure from the primary implementation specification,
including immutable genesis binding and v1 single-pool event enforcement, must
be reflected in the specification once implementation confirms the final wire
shape.

## Completion Record

Implemented:

- seedless startup, direct-address discovery, and signed invitations;
- private, unlisted, and shared-pool endpoint visibility;
- one-admin and deterministic multi-admin approval fragments, signed rejection,
  idempotent finalization, trust-bundle delivery, and candidate polling;
- approved-member endpoint exchange without a global peer directory;
- dynamic, capability-driven multi-pool workers and one-pool protocol-v1 event
  enforcement;
- per-pool ReleaseCard publication, source provenance, RBAC, manifest access,
  event delivery, and independent revocation;
- local admin API and UI workflows for create, join, invite, approve, reject,
  refresh, inspect, and revoke;
- four-node harness coverage for relayed admission, private invitation,
  distinct per-pool capabilities, restart persistence, exact approval-event
  convergence, isolation, revocation delivery, local Newznab search, signed
  manifest resolution, and cache reuse.

Validation:

- `go test ./...`;
- `npm --prefix ui run build`;
- fresh migration and federation integration tests against the disposable
  `gonzbnet_codex_test` database in `gonzb-postgres`;
- live four-process admission, release search/get, manifest-cache reuse, and
  private-invitation checks against isolated PostgreSQL databases in
  `gonzb-postgres`;
- `sh -n scripts/gonzbnet_e2e.sh` and `git diff --check`.

Broader specification work that is not part of this completed admission plan
is tracked in `docs/wiki/gonzbnet/implementation-status.md`.
