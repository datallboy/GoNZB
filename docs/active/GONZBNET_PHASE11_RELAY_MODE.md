# GoNZBNet Phase 11: Optional Relay Mode

## Spec Scope

Phase 11 describes an optional relay process with shared database/internal API,
relay API key, and public federation endpoints isolated from the main app.

## Constraint

The project requirement for the current implementation is still modular
monolith first, not microservices. A standalone `gonzbnet-relay` process is
therefore deferred. This phase implements the safe relay-ready controls inside
the existing application.

## Phase 11 Plan

1. Add config keys:
   - `gonzbnet.relay_enabled`
   - `gonzbnet.relay_api_key`
2. Honor existing `gonzbnet.http_enabled` by disabling public federation
   endpoints (`/.well-known/gonzbnet` and `/gonzbnet/v1/*`) while leaving local
   admin APIs available.
3. Advertise `relay_mode` in the node profile when relay mode is enabled.
4. Keep peer sync/gossip runtime independent from public HTTP endpoint exposure
   so a node can consume/relay through configured peers while the main app has
   no public federation port.
5. Document the operational split and the deferred standalone process boundary.

## Out Of Scope

- A separate `gonzbnet-relay` binary/process.
- A narrow relay-to-main internal API.
- Relay API key enforcement between separate processes.
