# Phase F Automated Coverage Improvements

Phase F adds optional foundations for automated coverage planning. It does not
enable an automatic assignment writer.

## Weighted Rendezvous Helpers

`internal/gonzbnet/coverage` now includes pure weighted rendezvous helpers:

- deterministic assignment for the same work/node inputs
- node weights for capacity-aware selection
- seen-set exclusion so work already seen by a node can be routed elsewhere

These helpers are independent of database state and are unit-tested.

## Read-Only Plans

`GET /api/v1/admin/gonzbnet/coverage/plan` returns a read-only scheduler plan
containing:

- dedup-aware suggestions
- stale claims
- scheduler mode

The endpoint does not create signed assignments. Operators or future automation
can review the plan first, then create assignments through the Phase D admin
assignment endpoint. Scanner coordination cleanup can consume existing range
assignments assigned to the local node.

## Boundary

Automatic failover remains review-gated. Stale claims are surfaced, but GoNZBNet
does not yet mint replacement `CoverageAssignment` events without an explicit
local action.
