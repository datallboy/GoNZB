# Stale Claim Reassignment

GoNZBNet can create local signed replacement assignments for stale article
range and time-window claims when automatic coverage mode is enabled.

The reassigner:

- reads stale `range` claims that have valid article ranges and no outcome;
- reads stale `time_window` claims that have valid time windows and no outcome;
- chooses eligible scanner nodes with deterministic weighted rendezvous;
- excludes the stale claimant from replacement selection;
- signs and appends `CoverageAssignment` events with the local node identity;
- projects the assignments through the existing coverage projection path.

The runtime worker runs only when:

- `modules.gonzbnet.enabled=true`
- `gonzbnet.coverage_enabled=true`
- `gonzbnet.scheduler_enabled=true`
- `gonzbnet.coverage_mode=automatic`

`manual` and `scheduler` modes remain review-only. Time-window replacement
assignments are executed by the scanner after local resolution to article
ranges.
