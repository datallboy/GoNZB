# GoNZB Docs

This directory contains a mix of product docs, operator references, and internal development notes.

## Start Here

If you are trying to understand or run GoNZB, start with:

- [README](../README.md)
- [Architecture](./ARCHITECTURE.md)
- [Indexer Wiki](./wiki/indexer/README.md)
- [GoNZBNet Wiki](./wiki/gonzbnet/README.md)

## Public And Operator-Facing Docs

- [Architecture](./ARCHITECTURE.md)
  - high-level module layout, ownership boundaries, and runtime model
- [Indexer Wiki](./wiki/indexer/README.md)
  - maintained stage ownership, flow, schema, partition, retention, release, and operations reference
- [GoNZBNet Wiki](./wiki/gonzbnet/README.md)
  - maintained architecture, configuration, protocol, operations, development, and E2E reference
- [Production Readiness](./PRODUCTION_READINESS.md)
  - current release gates, verified checks, deployment recommendation, and usability backlog
- [GoNZBNet Implementation Specification](./GoNZBNet_Codex_Implementation_Spec.md)
  - original design reference; the current-state wiki and code take precedence

## Internal Development Docs

These are mainly planning, implementation-history, or agent/developer references rather than end-user documentation:

- `docs/archive/completed/indexer/`
  - completed implementation plans and historical decision records
- `docs/archive/development/indexer/`
  - archived tuning notes, validation queries, superseded root indexer docs,
    sprint plans, and other developer-only references
