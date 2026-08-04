# GoNZBNet

GoNZBNet lets trusted GoNZB nodes share signed release information inside a
pool. A node can discover releases contributed by other members, resolve their
NZBs, report health, and reduce duplicated indexing work without sharing local
accounts or provider credentials.

GoNZBNet is optional. A local indexer and Newznab endpoint work without it.
Finish the ordinary GoNZB setup before creating or joining a pool.

## What is shared

Depending on pool policy and the roles granted to a node, GoNZBNet can exchange:

- signed release descriptions and NZB manifests;
- availability and validation results;
- pool membership and node capability information;
- coordination information for participating indexers;
- narrowly requested evidence that helps complete already identified work.

It does not federate local users, API keys, searches, grabs, external download
activity, NNTP credentials, or raw provider account identities.

Pool membership is a trust relationship. An authorized member may receive
release manifests and article identifiers required to use them. Invite only
operators you trust and remove lost or retired nodes promptly.

## Choose roles by outcome

Do not enable every role. Start with the roles needed for the node's purpose.

### Find and use releases

Most nodes begin as consumers. Enable the consumer, index projection, manifest
cache, pull synchronization, and aggregator GoNZBNet source. Together they let
the node receive approved release data, show it in local searches, and resolve
an NZB when a local user requests it.

### Contribute local releases

A node with a working local indexer can additionally publish releases and
their manifests. Scanner and publication roles should be enabled only after
the local indexer is reliably forming releases.

### Validate pool data

Validation and health roles check whether shared information remains useful.
They are most valuable on nodes with a genuinely independent NNTP provider or
network viewpoint. Running another validator against the same host and
provider does not create independent evidence.

### Coordinate several indexers

Coverage and scheduler roles help a pool deliberately divide work among
multiple participating scanners. They are unnecessary for a single scanner
and should be introduced only when pool administrators have agreed on how work
will be assigned.

### Improve transport

Push, gossip, peer exchange, and relay features can improve propagation in a
larger pool. Begin with pull synchronization. A relay moves authorized signed
data; it is not a reverse tunnel for an unreachable publisher.

## Recommended layouts

For one person, use one GoNZB node containing the locally useful roles. A
second container on the same host does not add an independent provider,
operator, storage system, or failure domain.

For a small private group, use approximately one node per operator or
location. Each participant may run an all-in-one node. Add validation only
where provider viewpoints differ, and add a stable relay only when peer
connectivity or pool size justifies it.

For a larger pool, specialization can help: indexing nodes contribute data,
independent validators check it, consumer/cache nodes serve local users, and a
small administrator-controlled set coordinates shared work. Specialize for a
measured resource, trust, or exposure boundary—not merely because roles have
different names.

## Settings that matter first

Configure these groups in order through **Admin > Settings > GoNZBNet**:

1. **Identity and address:** persistent key storage, node name, and an address
   reachable by the peers that need to contact it.
2. **Pool membership:** private invitations, administrator approval, and only
   the capabilities required by the node.
3. **Consumer path:** pull synchronization, local projection, manifest cache,
   and the aggregator source.
4. **Contribution path:** publication and scanner roles only when the local
   indexer is healthy.
5. **Validation and coordination:** enable only where independent evidence or
   multiple scanners make the work meaningful.
6. **Advanced transport:** push, gossip, peer exchange, and relay after the
   basic pull path is proven.

Keep default rate, cache, and transport limits until dashboard evidence shows
a real constraint. Changing every advanced setting at once makes failures much
harder to diagnose.

## Understanding the dashboard

The GoNZBNet dashboard answers four operator questions:

- **Who is connected?** Pools and members show node names, addresses, status,
  and granted roles.
- **Can this node find releases?** Consumer and manifest views show received
  catalog data, synchronization, cache activity, and resolution results.
- **Is this node contributing?** Publisher and scanner views show recent tasks
  and outcomes from local releases.
- **Is pool reporting useful?** Validation, health, and coordination views show
  completed work, empty polls, failures, and stale activity.

Warnings about missing capabilities are expected when a role is intentionally
disabled or the node has not joined a pool. Treat repeated authentication,
signature, manifest, or synchronization failures as actionable.

## Security boundary

- Prefer an invitation-only pool over public discovery.
- Keep the admin UI, setup route, database, and provider credentials private.
- Use HTTPS for peer traffic and preserve the node identity key across restarts.
- Grant the smallest useful capability set.
- Review membership changes, rejected data, unusual request rates, and stale
  peers.
- Assume every authorized recipient can retain information it legitimately
  receives.

Continue with the [Private pool quickstart](private-pool.md) and
[Networking and exposure](networking.md).
