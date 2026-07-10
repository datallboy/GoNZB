# GoNZBNet Pending Projections

Status: in progress

Migration `021_gonzbnet_pending_projections` adds durable pending state for
accepted events whose typed projection fails. Rows retain the event ID, event
type, projection kind, attempt count, last error, timestamps, and resolved
status. This supports retry workers and admin diagnostics without deleting or
rewriting the append-only event.
