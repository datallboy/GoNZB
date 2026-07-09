# GoNZBNet Phase 8: Health Attestations And Scoring

## Spec Scope

Phase 8 adds signed `HealthAttestation` events, optional local health
attestation publishing, availability score aggregation, reputation adjustments
from health accuracy, and search ranking integration.

## Current Codebase Context

- Signed federation events are created and verified in `internal/gonzbnet/events`.
- ReleaseCard publishing already uses `internal/gonzbnet/publisher`.
- Inbound events are accepted and projected by
  `internal/api/controllers/gonzbnet.go`.
- Federated search reads `federated_release_cards` and
  `federated_release_sources` in
  `internal/store/pgindex/federation_releasecard_store.go`.
- Existing score columns already include `availability_score`,
  `manifest_confidence_score`, and `trust_score`.

## Phase 8 Plan

1. Add `internal/gonzbnet/health` with the typed `HealthAttestation` schema,
   validation, scoring helpers, and deterministic body hashing.
2. Add PostgreSQL migration `009_gonzbnet_health_attestations.up.sql` with
   `health_attestations`, `reputation_events`, and indexes.
3. Add pgindex projection methods that insert health attestations, recompute
   per-source and per-release availability scores, and apply bounded node trust
   deltas for bad/strong claims.
4. Extend inbox projection so accepted `HealthAttestation` events update local
   health and score projections.
5. Add optional local health publishing to the existing GoNZBNet publisher
   service. The first implementation derives complete/incomplete status from
   local indexed release availability and manifest presence; it does not perform
   live remote article checks yet.
6. Add config keys for enabling local health attestations and controlling
   interval/batch size.
7. Update federated search ranking so health-adjusted availability and trust are
   part of ordering rather than only posted date.
8. Add unit tests for health validation, publishing complete/incomplete
   attestations, projection scoring, and search ranking SQL behavior where
   feasible without a live PostgreSQL instance.

Later cleanup:

- `GONZBNET_TRUST_ATTESTATIONS.md` adds signed `TrustAttestation` events as
  bounded, auditable reputation inputs.

## Out Of Scope

- Live NNTP STAT/HEAD sampling against remote manifests.
- Quorum consensus, pool-wide health schedules, and background validation
  assignment.
- UI for viewing health history or trust changes.
