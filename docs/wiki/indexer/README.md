# Indexer Wiki

This directory is the maintained reference for indexer design and operations.
Prefer these focused pages over the older root-level indexer documents.

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
- [Operations Playbook](./operations-playbook.md): practical checks and common
  commands.

`docs/active/2026-06-25-indexer-stabilization-source-of-truth.md` remains the
only active sprint plan. The wiki is the durable reference.
