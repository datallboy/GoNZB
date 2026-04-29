# Indexer Foundation Docs

Snapshot date: 2026-04-29

This file continues the prior indexer foundation doc and keeps the indexer docs organized as execution focus moves into the next performance and process-execution phase.

We currently have several planning and reference documents. This file defines which ones are the current execution guides, which older docs remain useful as reference, and which docs should not drive the current backlog.

## Current Status

- Phase 1 and Phase 2 of the earlier next-phase docs are done
- the stabilization, schema, and runtime pass is done
- the backlog burn-down performance pass and assemble/release refinement loop are done
- the first API/UI expansion phase is done
- the active indexer execution focus is now process execution and performance, with primary emphasis on assemble throughput and safe concurrency

## Current Active Docs

### `docs/active/INDEXER_FOUNDATION_DOCS.md`

Use for:

- the active foundation index for current indexer work
- deciding which docs should drive execution right now
- keeping the current sprint pointed at the right workstream

### `docs/active/INDEXER_PROCESS_EXECUTION_AND_PERFORMANCE_SPRINT.md`

Use for:

- the current performance sprint backlog
- milestone-by-milestone execution tracking
- baseline measurements and acceptance criteria
- assemble-first concurrency and execution-model decisions

## Current Execution Focus

The current focus is performance and execution-model hardening for the indexer runtime, with primary emphasis on:

- assemble throughput
- assemble candidate selection quality, especially path A
- safe concurrency for assemble and release stages
- optional multi-worker and multi-process execution without abandoning the single-binary architecture

## Process Execution And Performance Sprint

The next active workstream is an indexer performance sprint.

Current direction:

- keep the single-binary modular-monolith architecture as the default
- make the existing indexer `concurrency` settings real, starting with `assemble`
- use goroutine-based worker fan-out inside the current process before introducing cross-process worker scaling
- make concurrency database-safe through explicit claiming or leasing rather than optimistic in-memory coordination
- evaluate `release` multi-worker concurrency during the same sprint, but keep the main implementation focus on `assemble`

Current known baseline:

- pending unassembled headers are in the `18M+` range on the live dev database
- recent successful `assemble` runs are still roughly `34s` to `39s` for `2500`-header batches
- recent successful `release` runs are usually sub-`5s`
- current path A selection contributes only a tiny share of recent assemble batches, so it needs reevaluation

Use `docs/active/INDEXER_PROCESS_EXECUTION_AND_PERFORMANCE_SPRINT.md` as the day-to-day execution guide for:

- instrumentation and baseline capture
- path A selection redesign
- assemble worker concurrency
- assemble write-path and refresh-path scaling
- release concurrency evaluation
- optional cross-process worker topology

## Prior Execution And Reference Docs

### `docs/archive/completed/indexer/INDEXER_ASSEMBLE_AND_RELEASE_REFINEMENT_PLAN.md`

Use for:

- the completed refinement baseline and execution history
- the landed assemble and release prioritization, cooldown, and observability changes
- understanding the current path A and dirty-family baseline before changing it again

### `docs/archive/completed/indexer/INDEXER_BACKLOG_BURNDOWN_PERFORMANCE_PLAN.md`

Use for:

- the completed backlog burn-down performance pass
- PostgreSQL and runtime tuning history
- prior throughput and queue-quality validation evidence

### `docs/archive/completed/indexer/INDEXER_RELEASE_FORMATION_SNAPSHOT_AND_PLAN.md`

Use for:

- the stabilized release-formation model from the previous phase
- release identity and clustering notes
- reference while evaluating release multi-worker safety

### `docs/archive/completed/indexer/INDEXER_SCHEMA_TARGET.md`

Use for:

- the stabilized schema target from the previous phase
- side-table and hot-row boundary decisions
- reference if claim or lease state requires schema changes

### `docs/archive/completed/indexer/INDEXER_STABILIZATION_WORKLIST.md`

Use for:

- the final stabilization checklist and sign-off history
- understanding which foundation problems are already solved

## Current Reference Docs

### `docs/INDEXER_HOW_IT_WORKS.md`

Use for:

- quick terminology
- the current stage-by-stage pipeline objects and flow

### `docs/ARCHITECTURE.md`

Use for:

- module-level boundaries
- how the indexer fits into the larger app architecture
- the current modular-monolith execution model

### `docs/INDEXER_TEST_QUERIES.md`

Use for:

- validation queries and run commands while executing the current sprint
- checking stage behavior, backlog movement, and release visibility after changes

### `docs/INDEXER_POSTGRES_RUNTIME_TUNING.md`

Use for:

- the durable PostgreSQL and runtime tuning reference for the indexer
- the developer-laptop baseline and validation evidence
- tiered operator guidance for dev, lower-end self-hosted, and production systems

## Broader Context Docs

### `docs/archive/INDEXER_BACKEND_MILESTONES.md`

Use for:

- milestone history
- broader backlog context
- future work ideas

Do not use this as the day-to-day execution guide for the current sprint.

### `docs/archive/INDEXER_UI_API_ROADMAP.md`

Use for:

- future API and UI planning

Do not let this drive the current process-execution sprint.

## Guideline Rules

1. Treat `docs/active/INDEXER_PROCESS_EXECUTION_AND_PERFORMANCE_SPRINT.md` as the active backlog for the current sprint.
2. Use prior completed docs as reference when they help current work, but not as the active checklist.
3. Keep the single-binary modular-monolith architecture as the default unless new evidence proves it is the primary bottleneck.
4. Treat multi-worker and multi-process scaling as runtime-topology options, not as a mandate to split the codebase into separate products.
5. Keep assemble performance and safe concurrency ahead of broader architectural change.
6. Keep milestone docs as context, not as the active source of truth when a more focused active execution doc exists.
