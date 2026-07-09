# Phase 10 WebSocket Gossip

Phase 10 adds an optional WebSocket transport for signed event gossip. It is
disabled by default and does not implement live federated user search.

## Endpoint

The endpoint is:

```text
GET /gonzbnet/v1/ws
```

The WebSocket upgrade request must use normal GoNZBNet signed node request
authentication. The request authenticates the remote node, not a local user.

If `gonzbnet.websocket_gossip_enabled` is false, the endpoint returns 404.

## Batch Format

`internal/gonzbnet/gossip` defines `GossipBatch` and `GossipResponse`.

The implementation sends full signed events in `events` so the receiver can
verify and project them without performing a live event lookup. Received events
are processed through the same validation/projection path as the HTTP inbox:

- invalid signatures or unauthorized pool events are rejected;
- existing event IDs return duplicate;
- accepted events are appended once to the local append-only event store;
- local-only events are never selected for outbound gossip.

## TTL, Fanout, And Backoff

Inbound TTL is clamped to `gonzbnet.gossip_ttl`, and responses return the
forward TTL decremented by one. A TTL of zero is not forwarded.

Outbound gossip runs as a periodic pass when
`gonzbnet.websocket_gossip_enabled` is true. It sends up to
`gonzbnet.gossip_batch_size` accepted, undelivered, non-local events to up to
`gonzbnet.gossip_fanout` enabled peers.

Peers in `error` status are skipped until a bounded backoff window has elapsed
based on their failure count.

## Peer Exchange

Peer exchange is controlled separately by `gonzbnet.peer_exchange_enabled`.

When disabled, inbound peer URLs are ignored and responses include no peers.
When enabled, received peer URLs are normalized, deduplicated, capped by fanout,
and stored as enabled federation peers.

## Config

- `gonzbnet.websocket_gossip_enabled`
- `gonzbnet.gossip_interval_minutes`
- `gonzbnet.gossip_batch_size`
- `gonzbnet.gossip_ttl`
- `gonzbnet.gossip_fanout`
- `gonzbnet.peer_exchange_enabled`

## Limits

This phase does not add relay mode, WebSocket subscriptions, or live federated
query. Gossip is a transport for already signed federation events.
