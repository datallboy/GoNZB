# GoNZBNet Addendum Phase B: Validation-Only Contribution

## Spec Scope

Add validator-only contribution support:

- `ValidatorCapacity` event.
- `ArticleAvailabilityAttestation` event.
- `ChecksumAttestation` schema, with checksum validation feature-flagged off by
  default.
- Validation task queue.
- Validation scoring projection.

## Implementation Plan

1. Add event schemas under a new `internal/gonzbnet/validation` package:
   `ValidatorCapacity`, `ArticleAvailabilityAttestation`, and
   `ChecksumAttestation`.
2. Add migration `012_gonzbnet_validation.up.sql` for:
   - `federation_validation_tasks`
   - `article_availability_attestations`
   - `checksum_attestations`
   - validation score fields on `federated_release_sources`
3. Enqueue validation tasks when accepted `ResolutionManifest` events are
   projected.
4. Add store methods to claim validation tasks, project availability/checksum
   attestations, and recompute validation-aware release/source scores.
5. Add validator publishing methods that can run with GoNZBNet and PG enabled,
   without requiring the indexer module or local search projection.
6. Gate the worker behind `gonzbnet.validator_enabled`; keep checksum validation
   behind `gonzbnet.checksum_validation_enabled` defaulting false.
7. Extend node profiles, capability policy, and pool defaults for validation
   event types.

## Assumptions

- Phase B implements validation from locally cached signed manifests and their
  article lists. It does not add NNTP article probing yet; availability is
  marked `unverified` with low confidence unless future provider checks are
  added.
- The validation worker can run on a consumer-only/indexer-disabled node as long
  as GoNZBNet and PG storage are available.
- Existing HealthAttestation behavior remains intact for backward compatibility.

## Out Of Scope

- Live NNTP article existence checks.
- PAR2/NFO checksum extraction or payload download.
- UI controls for validation queue inspection.
