# GoNZBNet Coverage Wire Alignment

Status: complete

## Spec Scope

Coverage coordination events must use the addendum field names and semantics so
independent nodes agree on capacities, observations, plans, assignments, claims,
progress, outcomes, and heartbeats.

## Implementation Plan

1. Align active scanner-written events first: assignment, range/time-window
   claims, completion/failure outcomes, and their author bindings.
2. Align scanner capacity, heartbeat, group observation, and checkpoint bodies,
   including provider scope and progress/capacity fields.
3. Replace the narrow single-range CoveragePlan body with the specified
   versioned policy and nested assignment descriptors; project descriptors into
   existing assignment rows for scheduler reads.
4. Add migration columns needed for provider scope, mode/role, expiry, progress,
   and outcome metrics while retaining body JSON as the complete projection.
5. Update local admin, reassignment, and scrape-coordinator writers and strict
   body validation.
6. Add deterministic body tests, runtime writer tests, and a disposable
   PostgreSQL migration/projection test before closing the wire-conformance
   audit item.

## Compatibility

- Existing relational columns remain available for dashboard and scheduler
  queries and are backfilled where field semantics map directly.
- Signed bodies remain schema version `1.0`; pre-alignment bodies are rejected
  by strict receive validation instead of being interpreted with ambiguous
  semantics.
- Article numbers remain provider-local and provider scope is included in the
  signed coordination body.

## Implemented So Far

- `CoverageAssignment` now signs `mode`, `role`, provider scope, and
  `expires_at`; internal plan linkage is no longer emitted as a non-spec field.
- Range and time-window claims sign `claimant_node_id`, provider scope, and
  claim mode. Range claims include checkpoint cadence.
- Completion and failure events use `completion_id` / `failure_id`, spec metric
  names, `reason_code`, and `retryable`.
- Admin, reassignment, and scrape-coordinator writers populate the aligned
  fields. Remote outcomes derive assignment linkage from the stored claim.
- Migrations 019 and 020 project aligned active-event, capacity, observation,
  checkpoint, and nested-plan fields into relational columns.

The wire alignment is complete. Periodic production of capacity, heartbeat,
observation, and checkpoint events from scanner execution remains a separate
contribution-behavior item in the completion audit.
