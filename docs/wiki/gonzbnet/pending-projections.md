# Pending Projections

Accepted federation events are append-only. If a typed projection fails after
acceptance, the event remains durable and a row is recorded in
`federation_pending_projections` with the projection kind, error, attempt count,
and pending status. Future retry/admin workflows can replay the event and mark
the row resolved; the event itself is never rewritten.
