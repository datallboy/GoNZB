# Phase F Automated Coverage Improvements

Phase F adds optional foundations for automated coverage planning. Later cleanup
adds an automatic stale range reassignment writer for automatic mode.

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
and time-window assignments assigned to the local node.

## Stale Range Reassignment

The stale-claim reassignment cleanup adds a signed local assignment writer for
expired article range claims. It runs automatically only when
`gonzbnet.coverage_mode=automatic` and `gonzbnet.scheduler_enabled=true`, and it
can also be triggered through
`POST /api/v1/admin/gonzbnet/coverage/stale-reassignments`.

Replacement scanner nodes are chosen with the same deterministic weighted
rendezvous helper and the stale claimant is excluded from selection.

## Boundary

Manual and scheduler modes remain review-gated. Time-window assignment execution
is implemented in the scrape loop by resolving windows to article ranges. Stale
time-window failover uses the same signed replacement assignment flow as stale
article ranges.
