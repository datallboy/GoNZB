# GoNZBNet networking and exposure

GoNZBNet peers use authenticated federation requests over HTTPS or the
experimental `gonzb+ice` transport. The ICE transport performs STUN-assisted
NAT traversal and can fall back to an operator-provided TURN service; it is
hard-disabled by default and does not provide a general-purpose tunnel.

Reachable does not mean public. A private routed overlay such as WireGuard,
Tailscale, or Headscale remains the recommended boundary for the first private
pool.

## Who needs inbound reachability?

- A pull-only consumer can initiate its own synchronization and usually does
  not need arbitrary inbound Internet access.
- A publisher must be reachable by consumers that resolve manifests directly
  from it.
- A push destination, evidence source, or relay must be reachable by the peers
  using that function.
- A node may use different internal and advertised addresses when a reverse
  proxy provides the reachable HTTPS endpoint.

A relay forwards authorized signed data. It does not make an otherwise
unreachable manifest or evidence source reachable.

## Recommended private boundary

Place every member on the same private overlay and advertise its overlay HTTPS
address. Pool authentication remains required; the overlay is an additional
network boundary, not a replacement for membership checks.

Keep these private:

- `/setup` and the WebUI;
- `/api/v1/admin/*`;
- PostgreSQL;
- the GoNZB store and node key;
- NNTP and external-service credentials.

If a public reverse proxy is unavoidable, expose only the GoNZBNet discovery
and federation routes needed by peers, require HTTPS, restrict other routes,
and configure trusted proxy addresses correctly. Do not expose the database or
an unrestricted administration interface.

## Why this differs from BitTorrent

BitTorrent clients may combine trackers, distributed discovery, automatic port
mapping, hole punching, and relay mechanisms. GoNZBNet deliberately uses
explicit trusted peers. Its optional ICE module is only a transport between
authenticated GoNZBNet nodes: it does not add distributed discovery, UPnP,
NAT-PMP, PCP, TUN, SOCKS, or arbitrary proxying.

Continue with the [Private pool quickstart](private-pool.md).
