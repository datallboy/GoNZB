# GoNZBNet Event Chain Continuity

Status: complete

## Spec Scope

Each author maintains a signed event chain. Sequence numbers increase by one,
`previous_event_id` identifies the prior author event, partial-sync gaps are
allowed and tracked, and conflicting known links are treated as forks.

## Implementation Plan

1. Validate positive event sequences as part of envelope verification.
2. Serialize PostgreSQL append decisions per author and validate the immediate
   known predecessor and successor before insertion.
3. Allow out-of-order partial sync, persist unresolved sequence gaps, and clear
   them when the missing link arrives.
4. Persist fork diagnostics and the conflicting raw signed event, retain
   receive-path evidence in the rejected log, and mark the author node `forked`
   without overwriting blocked/local node state.
5. Return stable `sequence_conflict` or `fork_detected` receive errors and add
   pure chain-validation tests plus PostgreSQL migration coverage.
6. Update the maintained wiki and completion audit after the full suite passes.

## Out Of Scope

- Automatic fork resolution or trust-pool consensus over competing branches.
- Replacing the current rejected-event evidence store with a queryable alternate
  accepted branch.
- Making event append and every event-type projection one database transaction.

## Implemented

- Event creation and verification reject non-positive sequences.
- Accepted PostgreSQL appends use an author-scoped transaction lock and inspect
  the immediate predecessor, same-sequence event, successor, and named previous
  reference.
- Unknown predecessors are accepted as partial-sync gaps in
  `federation_event_chain_issues`; inserting a matching predecessor resolves the
  successor's gap.
- Same-sequence conflicts and known link mismatches persist raw signed fork
  evidence, preserve blocked/local status, and otherwise mark the author node
  `forked`.
- Inbox and pull map chain errors to `sequence_conflict` or `fork_detected` and
  dead-letter the received event without projection.
- Pure tests cover chain decisions. A PostgreSQL integration test covers the
  migration, gap lifecycle, fork evidence, and node status.
