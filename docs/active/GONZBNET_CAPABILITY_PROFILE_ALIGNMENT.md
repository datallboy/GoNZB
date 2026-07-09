# GoNZBNet Capability Profile Alignment

## Scope

This cleanup aligns public NodeProfile capability booleans with optional module
configuration.

## Implementation

- `release_cards` is advertised only when scanner or indexer capability is
  enabled.
- `resolution_manifests` is advertised only when manifest builder or manifest
  cache capability is enabled.
- `health_attestations` is advertised only when health checker or validator
  capability is enabled.
- Consumer-only nodes can advertise `consumer=true` without also advertising
  contribution capabilities.

## Out Of Scope

- Changing the `/caps` compatibility event-type list.
- Runtime mutation of GoNZBNet module configuration.
