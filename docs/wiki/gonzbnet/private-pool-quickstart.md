# Private Pool Quickstart

This guide creates a small, invitation-only GoNZBNet pool whose nodes are
reachable over a private routed network. It avoids exposing the GoNZB admin UI
or federation API directly to the public internet.

Use a private WireGuard, Tailscale, or Headscale network for the first pool.
GoNZBNet's experimental native traversal is deliberately not required here;
validate admission, pull synchronization, manifest resolution, revocation,
restart behavior, and backups over the overlay before enabling advanced
transports. A public reverse proxy is also possible, but is not required.

## 1. Prepare Each Node

Each participant needs:

- a persistent PostgreSQL database;
- a persistent GoNZB `store` directory, including the node identity key;
- a unique hostname on the private network;
- HTTPS for non-loopback peer URLs, unless every participant deliberately
  enables the private-network HTTP exception.

Start GoNZB normally, enable the GoNZBNet module, and set `advertise_url` to the
address other members can reach. The URL includes the protocol base path:

```yaml
gonzbnet:
  enabled: true
  advertise_url: https://gonzb-alice.example.ts.net/gonzbnet/v1
```

For Tailscale, `tailscale serve --bg 8080` can publish a local GoNZB listener
to the tailnet with HTTPS. Use the resulting tailnet URL as `advertise_url`.
Tailscale HTTPS hostnames are published in certificate-transparency logs, so
choose the machine name accordingly.

Do not commit `store`, database data, API keys, pool invitations, or node keys.

## 2. Choose A Small-Pool Role Layout

Start with one GoNZB process per person or location. Splitting roles into
multiple containers on the same host does not add an independent NNTP
viewpoint and usually adds operational complexity.

- Every searching node: consumer, index projection, manifest cache, aggregator
  GoNZBNet source, and pull synchronization.
- A node with a local indexer: also scanner, manifest builder, release
  publication, and availability publication.
- A node with an independent NNTP provider or backbone: optionally validator
  and health checker.
- One stable node may relay signed events when the pool grows. A relay does not
  tunnel HTTP requests to an otherwise unreachable publisher.

Enable push synchronization only after pull synchronization works. Leave
gossip, peer exchange, and coordinated coverage off until the pool has a clear
need for them.

## 3. Create And Join The Pool

On the first node:

1. Open **GoNZBNet > Pools**.
2. Create a private pool and grant only the capabilities its members require.
3. Create a signed invitation for the next member.

On each joining node:

1. Open **GoNZBNet > Pools** and submit the invitation.
2. Confirm the displayed pool and inviter identities before joining.
3. Have the pool administrator approve the pending membership when the pool's
   admission policy requires approval.
4. Add at least one existing member's private-network URL as a manual peer.

Repeat this process rather than copying databases or node keys. Each node must
have its own identity.

## 4. Verify Exchange

Use **GoNZBNet > Overview** and **Activity** to verify:

- the expected members, node names, peer URLs, and granted roles appear;
- pull synchronization succeeds without repeated authentication or TLS errors;
- published release cards reach another node's projected catalog;
- a remote release can be resolved into an NZB;
- validator and health activity comes from the expected independent nodes;
- direct binary evidence, if enabled, records peer hits and avoided BODY
  requests without exposing Message-IDs in the UI.

The setting `manifest_cache_serve_to_trusted_pools` controls whether a node may
re-serve a manifest authored by another node. Turning it off still permits a
publisher to serve manifests it authored itself.

## 5. Keep The Boundary Small

- Keep `/setup`, `/api/v1/admin/*`, the WebUI, PostgreSQL, and NNTP credentials
  private.
- If using a public reverse proxy, expose only `/.well-known/gonzbnet` and
  `/gonzbnet/v1/*`, require HTTPS, and configure trusted proxy addresses.
- Use private-pool invitations and least-privilege capability grants.
- Back up each node identity key separately from replaceable caches.
- Review rejected events, quarantines, sync failures, and membership changes.
- Revoke a lost or retired node instead of reusing its identity.
- Test PostgreSQL restoration and node-identity restoration separately, then
  perform a member-revocation drill before expanding the pool.

See [Networking And Exposure](./networking-and-exposure.md) for the reachability
model and [Administration And Operations](./administration-and-operations.md)
for routine maintenance.
