# GoNZBNet Stale Claim Reassignment Cleanup

This cleanup narrows the remaining automatic failover boundary for article
range scanner work.

## Scope

- Add a one-shot stale range claim reassigner.
- Select only stale `range` claims with valid article ranges and no recorded
  outcome.
- Choose replacement scanner nodes deterministically with the existing weighted
  rendezvous helper, excluding the stale claimant.
- Publish signed `CoverageAssignment` events for replacement range work.
- Wire the runtime worker only when GoNZBNet coverage and scheduler are enabled
  and `gonzbnet.coverage_mode=automatic`.

## Boundary

- `manual` and `scheduler` coverage modes remain review-only.
- Time-window stale claims remain visible in diagnostics but are not reassigned
  by this cleanup.
- Reassignment is local and signed by the local node; peers still validate it
  through normal trust-pool and capability checks.
