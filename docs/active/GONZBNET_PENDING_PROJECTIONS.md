# GoNZBNet Pending Projections

Status: complete

Migration `021_gonzbnet_pending_projections` adds durable pending state for
accepted events whose typed projection fails. Rows retain the event ID, event
type, projection kind, attempt count, last error, timestamps, and resolved
status. The sync service replays pending events from the immutable event log
and marks rows resolved after successful ReleaseCard, validation, or coverage
projection. Pull-sync startup performs an initial retry pass without deleting
or rewriting the append-only event.
