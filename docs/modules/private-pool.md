# Private pool quickstart

This guide creates a small invitation-only GoNZBNet pool over a private routed
network. Complete a normal GoNZB installation first and verify that local
search and Newznab access work.

A WireGuard, Tailscale, Headscale, or similar overlay is the simplest way to
give nodes stable private addresses without exposing the GoNZB administration
interface publicly. GoNZBNet's experimental ICE traversal is opt-in and should
not delay establishing and validating the first pool over an overlay.

## Prepare each node

Each participant needs:

- a persistent PostgreSQL database;
- a persistent GoNZB store containing that node's identity key;
- a unique hostname reachable by the intended peers;
- HTTPS for peer URLs, except for deliberately enabled private-network tests;
- an administrator who controls that node's membership and capabilities.

Enable GoNZBNet and set the advertised address to the federation URL reachable
by other members. Use a synthetic shape such as:

```yaml
gonzbnet:
  enabled: true
  advertise_url: https://node.example.invalid/gonzbnet/v1
```

Do not copy node keys between installations. Do not commit databases, stores,
credentials, or pool invitations.

## Start with a simple role layout

For every node that should search the shared catalog, enable the consumer,
index projection, manifest cache, aggregator source, and pull synchronization.

On nodes with a healthy local indexer, add the publication roles so locally
formed releases can be contributed. Add validator or health roles only on
nodes that provide an independent NNTP viewpoint.

Leave coordinated coverage, gossip, peer exchange, push, and relay disabled
until the basic consumer and publication paths work and the pool has a clear
reason to use them.

## Create and join the pool

On the first node:

1. Open **GoNZBNet > Pools**.
2. Create a private pool.
3. Define conservative membership and capability rules.
4. Create a signed invitation for the next participant.

On a joining node:

1. Open **GoNZBNet > Pools** and submit the invitation.
2. Verify the displayed pool and inviter identities.
3. Complete administrator approval when required by the pool policy.
4. Add a reachable existing member as the first peer if it was not learned
   through admission.

Repeat for each node. Never clone an existing member's database or identity to
create another member.

## Verify useful exchange

Use **GoNZBNet > Overview**, **Pools**, **Roles**, and **Activity** to confirm:

- the expected members, names, addresses, and roles appear;
- pull synchronization succeeds without repeated TLS or authentication errors;
- a release contributed by one node appears in another node's local catalog;
- requesting that release resolves a usable NZB;
- publication, validation, and health work comes only from the intended nodes;
- cache and evidence activity stays within the configured limits.

Do not expand the pool until these checks remain healthy across restarts.

## Operate the pool safely

- Keep `/setup`, the administration API, WebUI, PostgreSQL, stores, and NNTP
  credentials private.
- Use invitations and least-privilege capability grants.
- Back up every node identity separately from replaceable caches.
- Review membership changes, rejected data, synchronization failures, and
  unusual request volume.
- Revoke lost or retired identities rather than reusing them.
- Treat every member with manifest access as trusted to retain the information
  it receives.

See [Networking and exposure](networking.md) for reachability options and the
[GoNZBNet overview](gonzbnet.md) for role guidance.
