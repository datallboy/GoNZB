# Phase M: Source And Health Diagnostics

Phase M exposes local read-only diagnostics for source provenance, manifest
resolution sources, health attestations, and node reputation. It does not add
new federation messages or mutate trust state.

## Admin API

The admin API adds these local endpoints under `/api/v1/admin/gonzbnet`:

- `GET /diagnostics/release-sources`
- `GET /diagnostics/manifest-sources`
- `GET /diagnostics/health`
- `GET /diagnostics/reputation`

The release, manifest, and health endpoints accept `pool_id` and `limit`.
Reputation diagnostics accept `limit`.

These endpoints are registered with the existing GoNZBNet admin diagnostics
route group and require local admin authorization. They expose node IDs and
event IDs only; they do not expose local usernames, API keys, search history,
grab history, or download history.

## Store Queries

The pgindex diagnostics store reads from existing projection tables:

- `federated_release_sources`
- `federated_release_cards`
- `federated_manifest_sources`
- `health_attestations`
- `reputation_events`
- `federation_nodes`

No migration is required for this phase because the data already exists from
earlier phases.

## Admin UI

`/admin/gonzbnet` now includes compact read-only tables for:

- release source diagnostics, including source node, source event, pool,
  trust, availability, manifest confidence, and resolvability
- manifest source diagnostics, including advertised state, failure count,
  average latency, trust, and last success/failure timestamps
- health attestations, including author node, article counts, observed
  retention, repair confidence, confidence, and availability
- reputation diagnostics, including event-linked trust deltas and current local
  trust score

The UI fetches only local admin API data and keeps search/grab behavior
unchanged.
