# Phase B Validation-Only Contribution

Phase B lets a GoNZBNet node contribute validation metadata without running the
local Usenet indexer or exposing local search.

## Event Types

`internal/gonzbnet/validation` defines:

- `ValidatorCapacity`
- `ArticleAvailabilityAttestation`
- `ChecksumAttestation`

All three are signed node-to-node events. Pool policy requires the `validator`
capability before these events are accepted.

## Validation Queue

Migration `012_gonzbnet_validation.up.sql` adds
`federation_validation_tasks`. When a verified `ResolutionManifest` is cached,
`StoreResolutionManifest` enqueues one task for the manifest and pool.

The validator worker claims pending tasks with `FOR UPDATE SKIP LOCKED`, reads
the cached manifest, and emits an `ArticleAvailabilityAttestation`. Current
Phase B validation is structural only: it counts manifest segments and marks the
result `unverified` with low confidence. This requires no indexer, search
module, or user context.

Checksum attestation schema and projection are present, but checksum emission is
controlled by `gonzbnet.checksum_validation_enabled`, which defaults to false.

## Scoring

Accepted article availability attestations are stored in
`article_availability_attestations`; checksum attestations are stored in
`checksum_attestations`. Projection updates validation fields on
`federated_release_sources` and feeds the existing release-card score
recomputation through availability score inputs.

## Config

- `gonzbnet.validator_enabled`
- `gonzbnet.validation_batch_size`
- `gonzbnet.validation_interval_minutes`
- `gonzbnet.checksum_validation_enabled`

`validator_enabled` starts the worker from the GoNZBNet modular-monolith runtime.
It does not require `modules.usenet_indexer.enabled` or
`gonzbnet.index_projection_enabled`.
