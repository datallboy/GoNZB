# Capability Profile Alignment

NodeProfile capability booleans now reflect the configured optional modules:

- `release_cards` follows scanner or indexer capability.
- `resolution_manifests` follows manifest builder or manifest cache capability.
- `health_attestations` follows health checker or validator capability.

This lets validator-only, scanner-only, relay, and consumer-only nodes advertise
their real participation shape without implying support for modules that are
disabled locally.

Profile capacity cleanup adds module status plus scanner and validator capacity
blocks to the same NodeProfile document.
