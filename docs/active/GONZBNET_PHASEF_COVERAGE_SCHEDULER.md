# GoNZBNet Addendum Phase F: Automated Coverage Improvements

## Spec Scope

Optional post-v1 improvements:

- weighted scheduler
- deterministic distributed assignment
- rendezvous-hashing assignment
- seen-set summaries
- automatic failover for stale claims

## Implementation Plan

1. Add deterministic weighted rendezvous helpers in `internal/gonzbnet/coverage`.
2. Keep helpers pure and unit-tested so future automated assignment code can use
   them without database coupling.
3. Add read-only scheduler planning DTOs in the coverage store that combine
   stale claims and suggestion rows, but do not automatically write signed
   assignments.
4. Document that actual automatic failover is intentionally not enabled until
   operators can review generated plans.

## Out Of Scope

- Background worker that creates assignments automatically.
- Cross-node consensus on assignment plans.
- UI controls for accepting generated plans.
