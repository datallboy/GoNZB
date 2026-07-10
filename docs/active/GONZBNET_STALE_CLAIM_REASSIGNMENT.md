# GoNZBNet Stale Claim Reassignment Cleanup

This cleanup narrows the remaining automatic failover boundary for scanner
coverage work.

## Scope

- Add a one-shot stale claim reassigner.
- Select stale `range` claims with valid article ranges and no recorded outcome.
- Select stale `time_window` claims with valid time windows and no recorded
  outcome.
- Choose replacement scanner nodes deterministically with the existing weighted
  rendezvous helper, excluding the stale claimant.
- Publish signed `CoverageAssignment` events for replacement range or
  time-window work.
- Wire the runtime worker only when GoNZBNet coverage and scheduler are enabled
  and `gonzbnet.coverage_mode=automatic`.

## Boundary

- `manual` and `scheduler` coverage modes remain review-only.
- Reassignment is local and signed by the local node; peers still validate it
  through normal trust-pool and capability checks.
