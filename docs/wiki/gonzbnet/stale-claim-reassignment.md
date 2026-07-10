# Stale Claim Reassignment

GoNZBNet can create local signed replacement assignments for stale article
range claims when automatic coverage mode is enabled.

The reassigner:

- reads stale `range` claims that have valid article ranges and no outcome;
- chooses eligible scanner nodes with deterministic weighted rendezvous;
- excludes the stale claimant from replacement selection;
- signs and appends `CoverageAssignment` events with the local node identity;
- projects the assignments through the existing coverage projection path.

The runtime worker runs only when:

- `modules.gonzbnet.enabled=true`
- `gonzbnet.coverage_enabled=true`
- `gonzbnet.scheduler_enabled=true`
- `gonzbnet.coverage_mode=automatic`

`manual` and `scheduler` modes remain review-only. Time-window claims are still
visible in diagnostics and scheduler plans, but this cleanup only reassigns
article ranges because the current scrape loop consumes article-number ranges.
