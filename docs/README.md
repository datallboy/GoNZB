# GoNZB Docs

This directory contains a mix of product docs, operator references, and internal development notes.

## Start Here

If you are trying to understand or run GoNZB, start with:

- [README](../README.md)
- [Architecture](./ARCHITECTURE.md)
- [Indexer How It Works](./INDEXER_HOW_IT_WORKS.md)
- [Indexer Performance Tuning](./INDEXER_PERFORMANCE_TUNING.md)
- [Indexer Storage Retention And Purge Map](./INDEXER_STORAGE_RETENTION_AND_PURGE.md)

## Public And Operator-Facing Docs

- [Architecture](./ARCHITECTURE.md)
  - high-level module layout, ownership boundaries, and runtime model
- [Indexer How It Works](./INDEXER_HOW_IT_WORKS.md)
  - detailed indexer pipeline reference for operators and engineering readers
- [Indexer Performance Tuning](./INDEXER_PERFORMANCE_TUNING.md)
  - indexer performance audit methodology, live baseline notes, and tuning guidance
- [Indexer Storage Retention And Purge Map](./INDEXER_STORAGE_RETENTION_AND_PURGE.md)
  - table/job ownership, cleanup risk classes, live storage audit findings, and purge tradeoffs

## Internal Development Docs

These are mainly planning, implementation-history, or agent/developer references rather than end-user documentation:

- `docs/archive/completed/indexer/`
  - completed implementation plans and historical decision records
- `docs/archive/development/indexer/`
  - archived tuning notes, validation queries, and other developer-only references
