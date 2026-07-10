# Pending Projections

Accepted federation events are append-only. If a typed projection fails after
acceptance, the event remains durable and a row is recorded in
`federation_pending_projections` with the projection kind, error, attempt count,
and pending status. The sync service replays supported ReleaseCard, validation,
and coverage events during pull-sync startup and marks successful rows
resolved; the event itself is never rewritten.
