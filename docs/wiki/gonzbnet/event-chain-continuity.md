# Event Chain Continuity

Every accepted GoNZBNet event belongs to its author's signed append-only chain.
The event envelope carries a positive `sequence` and a `previous_event_id`; the
first event uses sequence 1 and has no predecessor.

PostgreSQL serializes append decisions per author. Before inserting an accepted
event, it checks:

- whether another event already occupies the sequence;
- whether a known sequence-minus-one event matches `previous_event_id`;
- whether a named known previous event belongs to the author and expected
  sequence;
- whether an already-known sequence-plus-one event points back to the incoming
  event.

Federation sync can deliver events out of order. A missing immediate predecessor
does not reject an otherwise valid event. It creates an open `sequence_gap` row
in `federation_event_chain_issues`. When the matching predecessor later arrives,
the transaction resolves the successor's gap.

A same-sequence conflict returns `sequence_conflict`. Any known link mismatch
returns `fork_detected`. The issue row retains the complete conflicting signed
event JSON. HTTP inbox and pull also copy the event to the rejected-event log,
so existing admin event diagnostics expose the receive failure. Non-local,
non-blocked authors are marked `forked`; later profile handshakes do not clear
that state. An operator can explicitly return the node to `known` after review.

GoNZBNet keeps one accepted canonical branch in `federation_events`. Alternate
fork evidence is retained but not projected into search, trust, health,
coverage, or manifest state. Automatic fork resolution remains out of scope.
