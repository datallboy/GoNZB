# GoNZB Docs

This directory contains a mix of product docs, operator references, and internal development notes.

## Start Here

If you are trying to understand or run GoNZB, start with:

- [README](../README.md)
- [Architecture](./ARCHITECTURE.md)
- [Indexer Wiki](./wiki/indexer/README.md)

## Public And Operator-Facing Docs

- [Architecture](./ARCHITECTURE.md)
  - high-level module layout, ownership boundaries, and runtime model
- [Indexer Wiki](./wiki/indexer/README.md)
  - maintained stage ownership, flow, schema, partition, retention, release, and operations reference

## Internal Development Docs

These are mainly planning, implementation-history, or agent/developer references rather than end-user documentation:

- `docs/archive/completed/indexer/`
  - completed implementation plans and historical decision records
- `docs/archive/development/indexer/`
  - archived tuning notes, validation queries, superseded root indexer docs,
    sprint plans, and other developer-only references
