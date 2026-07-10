# Capability Profile Alignment

NodeProfile distinguishes configured module status from behavior the node can
execute now:

- `release_cards` requires scanner mode and the ReleaseCard publisher switch.
- `resolution_manifests` currently follows manifest cache capability. A
  configured manifest builder remains visible in `module_status` but is not
  advertised as implemented until local building exists.
- `health_attestations` requires the health checker and health publisher switch.
- validator capacity currently advertises only structural `metadata` checks;
  NNTP sample and PAR2 booleans remain false.
- the WebSocket endpoint is present only when gossip is enabled.

This lets validator-only, scanner-only, relay, and consumer-only nodes advertise
their real participation shape without implying support for modules that are
disabled locally.

Profile capacity cleanup adds module status plus scanner and validator capacity
blocks to the same NodeProfile document.

`GET /gonzbnet/v1/caps` advertises only `jcs-json` with `none` compression.
Signed `NodeProfile` is not listed because profiles are fetched through the
public node resource, not accepted as events. `ResolutionManifest` remains in
the event list because the dedicated signed manifest response path verifies,
stores, and projects it.

ResolutionManifest validation rejects gzip/zstd labels and encrypted manifests
until decoding support exists. This prevents a peer from claiming a
representation that local NZB generation would interpret as plain manifest
data.
