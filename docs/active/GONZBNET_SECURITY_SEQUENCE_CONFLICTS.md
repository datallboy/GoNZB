# GoNZBNet Security Sequence Conflicts

## Scope

This cleanup aligns v1 append-only event storage with the spec requirement that
conflicting `(author_node_id, sequence)` events are rejected into the
dead-letter store.

## Implementation

- Detect an existing event for the same author node and sequence before
  inserting into `federation_events`.
- Return a typed `pgindex.ErrFederationSequenceConflict` from the store.
- Public inbox and pull sync record remote conflicts in
  `federation_rejected_events` with reason `sequence_conflict`.
- Conflicting events are not projected into release, health, validation, pool,
  or coverage tables.

## Out Of Scope

- Storing alternate fork branches.
- Pool-level fork resolution or witness checkpoint selection.
