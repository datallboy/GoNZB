# Phase 8 Health Attestations

Phase 8 adds signed health observations for federated releases and folds those
signals into local availability and ranking. Health events are still node-level
federation data; they do not include local users, API keys, searches, grabs, or
download history.

## Event Model

`internal/gonzbnet/health` defines `HealthAttestation` with body schema
`gonzbnet.HealthAttestation/1.0`.

Supported statuses are:

- `unknown`
- `complete`
- `incomplete`
- `missing`
- `repairable`
- `unverified`
- `provider_limited`

Validation rejects missing release IDs, unknown statuses, impossible article
counts, confidence values outside `0..1`, and `checked_at` values too far in the
future.

The default provider scope keeps `provider_backbone_hash` unset. Provider
account identity, NNTP credentials, and server credentials are never included in
health events.

## Storage And Projection

Migration `009_gonzbnet_health_attestations.up.sql` adds:

- `health_attestations`: accepted health observations with the signed event ID,
  release/manifest IDs, pool, article counts, confidence, method, and computed
  availability score.
- `reputation_events`: local trust deltas caused by accepted health claims.

The migration also expands default pool accepted event types to include
`HealthAttestation`.

Accepted inbox health events are projected after signature verification and pool
authorization. Projection inserts the attestation, averages recent health scores
for the release/pool, updates `federated_release_sources.availability_score`,
and recomputes the aggregate `federated_release_cards.best_score`.

## Local Publishing

`gonzbnet.health_attestations_enabled` starts an optional local health publisher
using the existing GoNZBNet publisher service.

The first implementation derives health from local indexed release metadata:

- complete when local article counts match expected part counts;
- incomplete when some expected articles are missing;
- missing when expected articles exist but none are locally available;
- repairable when incomplete local data has PAR2 repair evidence.

This phase does not perform live NNTP `STAT`/`HEAD` sampling for remote
manifests.

Config keys:

- `gonzbnet.health_attestations_enabled`
- `gonzbnet.health_attestations_batch_size`
- `gonzbnet.health_attestations_interval_minutes`

## Reputation And Ranking

Accepted health claims can adjust local node trust:

- strong complete claims with matching complete counts receive a small positive
  delta;
- complete claims that still report missing articles receive a false-positive
  penalty;
- missing claims that report available articles receive a smaller penalty.

Later trust-attestation cleanup adds signed `TrustAttestation` events as another
auditable reputation input. They are bounded, pool-authorized, and applied only
to local `federation_nodes.local_trust_score`.

Federated search now ranks results with the Phase 8 scoring formula:

```text
0.35 * node_trust_score
+ 0.25 * manifest_confidence_score
+ 0.25 * availability_score
+ 0.10 * quorum_score
+ 0.05 * freshness_score
```

Scores are clamped to `0..1`. Search still uses the local federated cache; it
does not live-broadcast user searches.
