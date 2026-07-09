# GoNZBNet Phase 10: WebSocket Gossip And Peer Exchange

## Spec Scope

Phase 10 adds optional WebSocket gossip at `/gonzbnet/v1/ws`, `GossipBatch`,
TTL/fanout limits, event dedupe, peer exchange, rate limiting, and connection
backoff.

## Implementation Plan

1. Add `internal/gonzbnet/gossip` for `GossipBatch`, response types, TTL
   normalization, peer-exchange filtering, and tests.
2. Add config keys:
   - `gonzbnet.websocket_gossip_enabled`
   - `gonzbnet.gossip_interval_minutes`
   - `gonzbnet.gossip_batch_size`
   - `gonzbnet.gossip_ttl`
   - `gonzbnet.gossip_fanout`
   - `gonzbnet.peer_exchange_enabled`
3. Add `GET /gonzbnet/v1/ws` using signed node request authentication.
4. Process inbound full signed events in `GossipBatch.events` with the existing
   inbox validation/projection path. Existing event IDs dedupe through the local
   event store.
5. Return accepted/duplicate/rejected statuses plus optional peer URLs when peer
   exchange is enabled.
6. Add a periodic runtime gossip pass that sends accepted, undelivered,
   non-local events to enabled peers over WebSocket with TTL clamped by config.
7. Update node profile capabilities and endpoint metadata.
8. Add unit tests for TTL clamping, peer-exchange disabled behavior, and
   WebSocket batch processing where feasible.

## Out Of Scope

- Live query over WebSocket.
- Long-lived subscription state or per-event real-time streaming.
- Relay mode beyond direct configured peers.
