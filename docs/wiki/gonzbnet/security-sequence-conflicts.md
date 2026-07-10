# Security: Sequence Conflicts

GoNZBNet keeps a v1 uniqueness constraint on `(author_node_id, sequence)` in
`federation_events`. Author-scoped append transactions also validate known
predecessor and successor links and track partial-sync gaps.

Remote conflicts are rejected into `federation_rejected_events` with reason
`sequence_conflict` and are not projected. The public inbox maps the rejection
to the stable `sequence_conflict` item code.

Conflicting signed event JSON is retained in
`federation_event_chain_issues`, and receive paths also retain dead-letter
evidence. The alternate branch is not accepted or projected. See
[Event Chain Continuity](./event-chain-continuity.md) for gap and fork behavior.
