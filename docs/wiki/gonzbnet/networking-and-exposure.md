# Networking And Exposure

GoNZBNet peers normally communicate over authenticated HTTPS endpoints. An
experimental, opt-in `gonzb+ice` transport can gather host, STUN-derived, and
TURN-relayed candidates and carry the same signed HTTP requests over reliable,
ordered WebRTC DataChannels. UPnP, NAT-PMP, PCP, DHT discovery, and a
general-purpose tunnel are intentionally not included.

Reachable does not mean public. A WireGuard, Tailscale, or Headscale network
gives nodes stable private addresses across ordinary home NAT and remains the
recommended setup for a first private pool. Native traversal is not a
prerequisite for pool operation.

## Experimental Native Traversal

A traversal locator has this form:

```text
gonzb+ice://node_<identity>@connect.example/gonzbnet/v1
```

The coordinator authenticates enrolled node identities and routes signed,
two-minute signaling envelopes. ICE prefers direct UDP and may fall back to
coturn over UDP, TCP, or TLS. The exact SDP—including DTLS fingerprints—is
hashed and Ed25519-signed by the persistent GoNZB identity. The coordinator
can observe node IDs, public addresses, timing, connection duration, and byte
counts or deny service, but it cannot decrypt GoNZBNet requests, events,
manifests, evidence, or credentials.

Inbound traversal is restricted to `/.well-known/gonzbnet` and the configured
GoNZBNet protocol base. `/setup`, `/api`, `/api/v1/admin`, the WebUI, NNTP,
filesystem paths, and arbitrary proxy destinations are rejected before the
application handler. Existing request signatures and pool authorization remain
authoritative; coordinator enrollment never grants pool membership.

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
the clients and networks involved. GoNZBNet keeps explicit authenticated peers
and does not add public DHT discovery or automatic router port mappings. ICE is
an optional transport for known node identities, not a public discovery layer.

See [Private Pool Quickstart](./private-pool-quickstart.md) for a deployment
walkthrough.
