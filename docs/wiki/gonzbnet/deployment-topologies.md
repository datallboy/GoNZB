# Deployment Recommendations

The best default is **one GoNZB node per operator or meaningful operational
boundary**, with all locally useful roles enabled in that instance. GoNZBNet is
a modular monolith, not a set of microservices that should automatically be
split into one process per role.

A second node is worthwhile when it creates something the first node cannot:

- a different operator or trust identity;
- a different NNTP provider/backbone and therefore independent evidence;
- a different host, location, storage system, or network failure domain;
- a separate public exposure/security boundary;
- a workload that genuinely needs independent scaling or maintenance.

If none of those changes, splitting roles usually adds databases, identities,
keys, pool memberships, synchronization, monitoring, and failure modes without
improving federation quality.

## One Person Running Indexer, Aggregator, And GoNZBNet

Run **one all-in-one GoNZB instance**. This is the recommended production
layout for a normal single-operator installation, not merely a development
shortcut.

```text
Users -> one GoNZB API/UI process -> one PostgreSQL store
                  |
                  +-- aggregator consumer/index projection/cache
                  +-- local indexer scanner/publisher/manifest builder
                  +-- optional local validation and health checks
                  +-- NNTP provider(s)
```

Recommended roles and controls:

- Enable consumer, index projection, manifest cache, and the aggregator
  GoNZBNet source when federated releases should appear to local users.
- Enable scanner, manifest builder, release-card publication, and manifest
  availability when this local indexer should contribute its releases.
- Local validator and health roles are useful for operational checking, but a
  node validating through its own provider is not independent corroboration.
- Leave coverage and scheduler off when this is the only scanner. The normal
  indexer already chooses its local scrape work; federation coverage exists to
  coordinate multiple scanners.
- Leave relay, peer exchange, and synchronization off until there is another
  node to contact.

Do not create three containers called scanner, validator, and consumer merely
to separate role names. They would be three federation identities requiring
their own key directories, runtime state, memberships, and normally their own
PostgreSQL/storage ownership. On one host and one NNTP provider they still share
the important failure and evidence boundaries.

### When This Person Should Add A Second Node

Add one only for a concrete purpose:

- **Independent validation:** run a small validator/health node against a
  different NNTP provider/backbone. A separate VPS or home location is better;
  a second container is still useful if the provider viewpoint is different,
  but it does not add host availability.
- **Public edge isolation:** keep the indexer/publisher private and place the
  consumer/cache/optional relay on a separately secured host or VM exposed to
  users and peers.
- **Capacity isolation:** move NNTP-heavy validation or scanning to hardware
  with its own CPU, memory, disk, and provider quotas after measurements show
  contention.
- **Availability:** place a consumer/cache on another physical or cloud failure
  domain so the catalog remains available during maintenance or a host outage.

## What Containers, VMs, And Separate Servers Actually Provide

| Placement | What improves | What does not improve |
| --- | --- | --- |
| Same process | Simplest configuration and data flow; roles share local state without federation hops. | No role-level process isolation or independent availability. |
| Separate containers on one host | Resource limits, restart/deploy isolation, and a possible network-exposure boundary. | Host, disk, power, kernel, network, and usually NNTP-provider independence. It also creates extra node identities and sync work. |
| Separate VMs on one physical server | Stronger OS/network isolation and clearer resource allocation than containers. | Physical host/power/storage failure independence; evidence remains correlated if the provider is the same. |
| Separate servers or locations | Real host/network availability and useful security isolation; can add provider diversity. | Independent evidence only if provider/backbone or data source also differs. |
| Separate operators | Independent trust, policy, administration, and failure decisions—the strongest federation benefit. | Nothing automatically; operators still need distinct providers/locations for technical diversity. |

Use separate processes because a workload or security boundary requires it,
not because GoNZBNet exposes separate role switches.

## A Group Of Friends

The recommended layout is **one node per participating person/location**, not
several role-specific nodes per person. Each person's node should remain useful
to that person and enable only the work their resources support.

For a small pool:

- every person who searches locally can run consumer, index projection, and
  manifest cache;
- people with indexers can also run scanner/publisher/manifest builder;
- choose two or three members with different NNTP providers/backbones for
  validator and health work;
- enable federation coverage only when two or more scanners intentionally
  divide the same pool's scrape space;
- designate one or two capable members for coverage coordination rather than
  enabling the scheduler everywhere;
- use pull/push between stable peer URLs; add relay or gossip only when
  reachability or peer count calls for it.

It is fine for each friend to run an all-in-one node. The federation benefit
comes from different operators, locations, providers, and retained copies—not
from forcing roles apart inside each person's deployment.

## A Large Distributed Pool

At larger scale, specialized nodes become useful because work and exposure are
no longer uniform. A practical pool may contain:

| Node class | Typical roles | Scaling reason |
| --- | --- | --- |
| Member/home nodes | consumer, index projection, small manifest cache; optional local publisher | Keeps searches and grabs local and gives each operator autonomy. |
| Scanner/publisher nodes | scanner, manifest builder, release/manifest publication | Scale by assigned groups/article ranges and provider quota. Respect remote claims. |
| Validator/health nodes | validator, health checker, health publication | Scale across genuinely distinct NNTP backbones and locations for evidence diversity. |
| Consumer/cache edges | consumer, index projection, larger manifest cache, aggregator source | Scale user-facing catalog and manifest resolution independently from NNTP scanning. |
| Relays | relay, synchronization, optional peer exchange/gossip | Solve reachability and propagation for authorized events; do not need indexer or validator privileges. |
| Coverage coordinators | coverage and scheduler | Maintain assignments and reassign stale claims; use a small redundant/admin-controlled set, not every node. |

Specialization solves real problems here: scanners scale by work partition,
validators scale by independent viewpoints, caches scale read traffic, relays
scale connectivity, and coordinators keep ownership unambiguous. It still does
not require one role per node. A powerful publisher may also validate, and an
edge may also relay, when those combinations share the same sensible boundary.

## Decision Rule

Start with one node. Split a role only if the new node changes at least one of
these answers:

1. Who controls and is trusted to operate it?
2. Which NNTP or source-data viewpoint does it observe?
3. Which host, storage, network, or location can fail independently?
4. Which endpoints and secrets must be exposed?
5. Which workload must scale or restart independently?

If every answer stays the same, keep the role in the existing node. If one or
more answers materially change, a separate node can provide a real benefit.
