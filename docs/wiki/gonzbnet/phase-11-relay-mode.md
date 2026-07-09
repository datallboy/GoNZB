# Phase 11 Relay Mode Controls

Phase 11 keeps relay behavior inside the modular monolith. The standalone
`gonzbnet-relay` process described in the spec is deferred because the current
project constraint is to avoid splitting GoNZBNet into microservices.

## Public Federation Isolation

`gonzbnet.http_enabled` now controls the public federation HTTP surface:

- `/.well-known/gonzbnet`
- `/gonzbnet/v1/node`
- `/gonzbnet/v1/caps`
- `/gonzbnet/v1/handshake`
- `/gonzbnet/v1/outbox`
- `/gonzbnet/v1/events/:event_id`
- `/gonzbnet/v1/inbox`
- `/gonzbnet/v1/manifests/:manifest_id/request`
- `/gonzbnet/v1/manifests/:manifest_id`
- `/gonzbnet/v1/ws`

When disabled, local admin APIs remain available. This lets the main app consume
or process shared-database federation state without exposing a public federation
port.

## Relay Capability

`gonzbnet.relay_enabled` advertises `relay_mode` in the node profile. It does
not create a separate process; it marks the node as willing to behave as a relay
within the current runtime.

`gonzbnet.relay_api_key` is reserved for a future narrow internal API if relay
extraction becomes necessary. It is not used by the monolith runtime.

## Runtime Shape

Peer pull, push, and WebSocket gossip remain controlled by their own settings:

- `gonzbnet.pull_sync_enabled`
- `gonzbnet.push_sync_enabled`
- `gonzbnet.websocket_gossip_enabled`

Those workers can continue to write accepted events and projections to the
shared PostgreSQL store independently of public endpoint exposure.

## Deferred

The following remain future work:

- a standalone `gonzbnet-relay` binary;
- a relay-to-main internal API;
- relay API-key enforcement between separate processes;
- public federation endpoint hosting in a separate deployment unit.
