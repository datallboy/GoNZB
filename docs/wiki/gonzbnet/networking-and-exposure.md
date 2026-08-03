# Networking And Exposure

GoNZBNet peers communicate over authenticated HTTP endpoints. The module does
not currently implement UPnP, NAT-PMP, PCP, STUN/TURN, ICE, DHT discovery, or
connection hole punching. An advertised peer URL therefore must already be
reachable from the nodes that use it.

Reachable does not mean public. A WireGuard, Tailscale, or Headscale network
gives nodes stable private addresses across ordinary home NAT and is the
recommended setup for a private pool.

## Which Roles Need Inbound Reachability?

| Behavior | Inbound peer reachability |
| --- | --- |
| Pull-only consumer | Not required when it only initiates pulls and manifest fetches. |
| Push destination | Required from the pushing peers. |
| Publisher and manifest source | Required so consumers can request locally authored manifests. |
| Binary-evidence source | Required so authorized peers can query evidence. |
| Relay | Required from nodes that send events or admission fragments through it. |
| Validator that only pulls and publishes outward | Usually not required unless peers also pull from it. |

A relay forwards eligible signed events and admission material. It is not a
reverse tunnel, and it does not proxy manifest resolution or binary-evidence
queries to an unreachable node. Resolution manifests are fetched on demand
from their source endpoint rather than being included in general event relay.

## Recommended Private-Pool Boundary

Run GoNZB on a private overlay and advertise its overlay HTTPS URL. Keep the
WebUI, setup endpoints, admin API, PostgreSQL, and NNTP credentials reachable
only by administrators. Pool membership and protocol signatures are still
required; the private network is an additional boundary, not a replacement for
application authorization.

When a public endpoint is unavoidable, put GoNZB behind a hardened HTTPS
reverse proxy and expose only:

- `/.well-known/gonzbnet`
- `/gonzbnet/v1/*`

Do not publish `/setup`, `/api/v1/admin/*`, PostgreSQL, or an unrestricted
WebUI. Configure GoNZB's trusted-proxy list so forwarded client addresses are
accepted only from the proxy.

## Why BitTorrent Appears To Work Differently

BitTorrent peers can use tracker introductions, a distributed hash table,
UPnP/NAT-PMP port mappings, hole punching, and relay extensions depending on
the clients and networks involved. GoNZBNet deliberately has none of those
discovery or traversal mechanisms today. Its trust-pool model uses explicit,
authenticated peers, so private routing or an operator-managed public endpoint
is required.

See [Private Pool Quickstart](./private-pool-quickstart.md) for a deployment
walkthrough.
