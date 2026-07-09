# Phase I Admin Diagnostics

Phase I adds read-only local diagnostics for the GoNZBNet federation control
plane. These diagnostics satisfy the v1 checklist item for peer, event, pool,
and validation visibility without adding remote federation endpoints.

## Admin API

The following local endpoints are registered under
`/api/v1/admin/gonzbnet/diagnostics/*` and use the existing GoNZBNet admin RBAC
group:

- `GET /diagnostics/peers`
- `GET /diagnostics/events`
- `GET /diagnostics/rejected-events`
- `GET /diagnostics/deliveries`
- `GET /diagnostics/validation-tasks`

Pool and member diagnostics remain available through the Phase 6 pool admin
endpoints.

## Data Sources

The diagnostics read existing PostgreSQL state:

- `federation_peers`
- `federation_peer_cursors`
- `federation_peer_deliveries`
- `federation_events`
- `federation_rejected_events`
- `federation_validation_tasks`

No migration is required. The queries are capped by a `limit` parameter with a
maximum of 500 rows per endpoint.

## WebUI

The GoNZBNet admin page now includes diagnostics panels for:

- peer status, cursor, failures, and sync timestamps
- accepted event log status and projection status
- rejected event reasons
- peer delivery attempts and errors
- validation task status, due times, attempts, and errors

The page continues to use local RBAC and does not expose local usernames, API
keys, search history, grab history, or download history.
