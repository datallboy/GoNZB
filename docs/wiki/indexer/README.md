# Indexer Wiki

This directory is the maintained reference for indexer design and operations.
The older root-level indexer documents were archived during the v0.8.0
closeout; use these focused pages as the source of truth.

## Pages

- [Stage Ownership](./stage-ownership.md): stage-owned tables, allowed writes,
  and forbidden write-backs.
- [Stage Flow](./stage-flow.md): scrape, assemble, yEnc recovery, release
  refresh, release formation, and inspection data flow.
- [Schema And Partitions](./schema-and-partitions.md): table groups, daily
  partition keys, and query-shape rules.
- [Retention](./retention.md): partition drop rules, blockers, and purge order.
- [Release Formation](./release-formation.md): summary refresh and release
  candidate contracts.
- [Binary Grouping Evidence](./binary-grouping-evidence.md): evidence
  priority for subject-derived, yEnc-derived, and weak binary grouping.
- [yEnc Recovery Queueing](./yenc-recovery-queueing.md): how recovery work is
  admitted, prioritized, selected, grouped, and capped.
- [Operations Playbook](./operations-playbook.md): practical checks and common
  commands.

Archived sprint plans and handoff notes are historical context only. If a
future plan conflicts with this wiki, update the wiki or the plan before
changing code.
