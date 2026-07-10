# GoNZBNet Truthful Capability Advertisement

Status: complete

## Spec Scope

Node profiles and `/caps` must describe behavior this node can execute now.
Optional module configuration must not imply support for an unimplemented event
path, compression codec, validation tier, or publication workflow.

## Implementation Plan

1. Advertise only `none` compression until request/response gzip or zstd
   handling exists.
2. Remove signed `NodeProfile` from accepted event types while retaining the
   public node-profile resource; retain `ResolutionManifest` because the signed
   manifest request/response path verifies and stores it.
3. Gate ReleaseCard and HealthAttestation production capabilities on their
   actual publisher switches as well as the owning module.
4. Do not advertise local manifest building until a builder exists; keep its
   configured module status visible separately.
5. Advertise only the structural metadata validator tier and no sample/PAR2
   support until NNTP-backed validation is implemented.
6. Omit the WebSocket endpoint when gossip is disabled and add profile/caps
   regression tests.

## Out Of Scope

- Implementing compression codecs, local manifest building, or NNTP validation.
- Removing receive event types that already have typed validation and
  projection paths.

## Implemented

- `/caps` advertises `jcs-json`, `none` compression, and only signed event types
  accepted by normal receive or the dedicated manifest path.
- ReleaseCard and HealthAttestation production flags require both their owner
  module and output worker switch.
- Manifest cache support remains advertised; local manifest building remains
  visible as configured module status but is not claimed as a working
  capability yet.
- Validator capacity reports only structural `metadata` validation and no
  sample-payload or PAR2 support.
- Disabled WebSocket gossip no longer emits a WebSocket endpoint.
- Manifest verification rejects compression other than `none` and encrypted
  manifests until those representations are implemented.
