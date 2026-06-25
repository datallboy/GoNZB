# GoNZB Docs

This directory contains a mix of product docs, operator references, and internal development notes.

## Start Here

If you are trying to understand or run GoNZB, start with:

- [README](../README.md)
- [Architecture](./ARCHITECTURE.md)
- [Indexer Wiki](./wiki/indexer/README.md)
- [Indexer Performance Tuning](./INDEXER_PERFORMANCE_TUNING.md)

## Public And Operator-Facing Docs

- [Architecture](./ARCHITECTURE.md)
  - high-level module layout, ownership boundaries, and runtime model
- [Indexer How It Works](./INDEXER_HOW_IT_WORKS.md)
  - compatibility entry point for the focused indexer wiki
- [Indexer Current Schema And System Interactions](./INDEXER_CURRENT_SCHEMA_AND_SYSTEM_INTERACTIONS.md)
  - compatibility entry point for the focused indexer wiki
- [Indexer Wiki](./wiki/indexer/README.md)
  - maintained stage ownership, flow, schema, partition, retention, release, and operations reference
- [Indexer Performance Tuning](./INDEXER_PERFORMANCE_TUNING.md)
  - indexer performance audit methodology, live baseline notes, and tuning guidance
- [Indexer Storage Retention And Purge Map](./INDEXER_STORAGE_RETENTION_AND_PURGE.md)
  - compatibility entry point for the focused indexer wiki

## Internal Development Docs

These are mainly planning, implementation-history, or agent/developer references rather than end-user documentation:

- `docs/archive/completed/indexer/`
  - completed implementation plans and historical decision records
- `docs/archive/development/indexer/`
  - archived tuning notes, validation queries, and other developer-only references
- `docs/active/`
  - the single active sprint execution plan; archived active plans are historical only
