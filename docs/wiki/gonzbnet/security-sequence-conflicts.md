# Security: Sequence Conflicts

GoNZBNet keeps a v1 uniqueness constraint on `(author_node_id, sequence)` in
`federation_events`. Before appending an accepted event, the PostgreSQL store
checks whether a different event already exists for the same author sequence.

Remote conflicts are rejected into `federation_rejected_events` with reason
`sequence_conflict` and are not projected. The public inbox maps the rejection
to the stable `sequence_conflict` item code.

The implementation does not store alternate fork branches yet. Future pool
checkpoint or witness logic can add fork-resolution policy on top of this
dead-letter evidence.
