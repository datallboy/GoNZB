# Receive Body Validation

Inbox, gossip, and manual pull use the same receive ordering:

1. verify the signed event envelope, event ID, body hash, time window, and
   author key;
2. reject unsupported event types;
3. validate the exact body schema, version, and type;
4. reject local user/context fields;
5. run the typed validator and verify author-bound node IDs;
6. apply trust-pool membership, capability, and quorum rules;
7. append the event as accepted and project it.

ReleaseCard validation recomputes `release_id` from its identity core and
rejects malformed titles, groups, IDs, timestamps, counts, and confidence.
Signed malformed bodies are written to `federation_rejected_events` and do not
enter the accepted event log.

Pool member approval events carry `allowed_capabilities` and optional `limits`.
Those fields are covered by each admin approval signature and are projected into
the pool member capability grant. Changing either field invalidates the
approval threshold.

Accepted append and projection are still separate database operations. Typed
validation now runs first, but transactional append/projection remains tracked
as separate completion work for database failures.

