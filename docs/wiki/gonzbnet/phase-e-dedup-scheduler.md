# Phase E Dedup-Aware Local Scheduler

Phase E adds local scheduler read paths over signed coverage projections. It
does not create assignments automatically; it helps scanner nodes decide which
assigned work to claim.

## Suggestions

`GET /api/v1/admin/gonzbnet/coverage/suggestions` returns assignments that are
safe to claim locally.

Query parameters:

- `pool_id`
- `node_id`
- `mode`: `scanner` or `validator`
- `limit`
- `min_blocking_trust`

Scanner mode skips assignments blocked by trusted active claims or completed
ranges. Validator mode intentionally allows overlap with completed ranges so
nodes can recheck/validate work.

Claims from nodes with `federation_nodes.local_trust_score` below
`min_blocking_trust` do not block suggestions. This keeps low-trust claims from
preventing higher-priority local work.

## Dashboard

The coverage dashboard now includes:

- `gaps`: assigned work without active claims or completion.
- `duplicates`: active duplicate range claims.
- `coverage_score`: completed assignments divided by total assignments.

The dashboard still reads only local projections of signed federation events; it
does not live-query peers.
