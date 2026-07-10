# Signed Pool Reads And Complete Pull

GoNZBNet keeps discovery metadata public while requiring node authentication
for federation data. `/.well-known/gonzbnet`, `/node`, and `/caps` remain public.
The following reads require the standard signed node request:

- `/gonzbnet/v1/outbox`
- `/gonzbnet/v1/events/:event_id`
- `/gonzbnet/v1/pools/:pool_id/members`
- `/gonzbnet/v1/pools/:pool_id/checkpoint`
- `/gonzbnet/v1/peers` when peer exchange is enabled

Outbox rows are filtered to public events and events for pools where the
requesting node has active membership. Direct event reads apply the same rule.
Pool member and checkpoint reads require active membership in the pool named by
the route. Peer exchange requires active membership in at least one local pool.

Pull sync performs discovery and handshake first, then signs the outbox GET with
the local node identity. It requests the complete accepted event stream rather
than filtering to ReleaseCards. The normal verification, typed-body validation,
pool authorization, append, and projection paths handle each received event.

Push and WebSocket gossip use the destination node ID learned from its signed
profile. Delivery selection applies the same public-or-shared-pool visibility
rule, so a configured peer cannot receive events from pools it has not joined.

These paths authenticate federation nodes only. Local usernames, API keys,
searches, grabs, and download history are not included in requests or events.
