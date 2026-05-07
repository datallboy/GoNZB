# Indexer Foundation Docs

Snapshot date: 2026-04-29

This is an internal execution and planning index for ongoing indexer work. It is not intended to be an end-user documentation entrypoint.

This file continues the prior indexer foundation doc and keeps the indexer docs organized as execution focus moves beyond the completed performance and process-execution phase.

We currently have several planning and reference documents. This file defines which ones are the current execution guides, which older docs remain useful as reference, and which docs should not drive the current backlog.

## Current Status

- Phase 1 and Phase 2 of the earlier next-phase docs are done
- the stabilization, schema, and runtime pass is done
- the backlog burn-down performance pass and assemble/release refinement loop are done
- the first API/UI expansion phase is done
- the process execution and performance sprint is complete and archived
- the backlog burn-down follow-up sprint is complete and archived
- the active indexer execution focus now centers on storage retention, maintenance/reporting, and later operational tuning work

## Current Active Docs

### `docs/active/INDEXER_FOUNDATION_DOCS.md`

Use for:

- the active foundation index for current indexer work
- deciding which docs should drive execution right now
- keeping the current sprint pointed at the right workstream

### `docs/active/INDEXER_DATABASE_STORAGE_RETENTION_AND_OFFLOAD_PLAN.md`

Use for:

- the active database space-reduction and retention backlog
- table/index size findings from the live dev database
- maintenance configurability and frontend reporting requirements
- NZB blob-cache/offload planning
- evaluating whether `release_file_articles` can be consolidated into `binary_parts`

### `docs/archive/completed/indexer/RUNTIME_SETTINGS_AND_CONTROL_PLANE_PLAN.md`

Use for:

- the completed runtime settings ownership split
- deciding whether a setting belongs in bootstrap YAML or SQLite runtime state
- control-plane UI and unified admin navigation work
- first-run setup flow and module capability/readiness behavior

## Current Execution Focus

The current focus is database storage retention and reclaim planning.

Primary workstream:

- make maintenance retention windows configurable
- add maintenance reporting and admin UI controls
- reduce `article_header_ingest_payloads` retention safely
- evaluate longer-term NZB blob caching/offload so article mappings can eventually be pruned more aggressively

Recent completed focus:

- assemble backlog burn-down and schema simplification
- assemble throughput and safe concurrency
- release query/write batching
- inspect archive/media concurrency with database reservations
- deferring cross-process topology until measurements justify it

### `docs/archive/completed/indexer/INDEXER_BACKLOG_BURNDOWN_AND_SCHEMA_SIMPLIFICATION_PLAN.md`

Use for:

- the completed backlog burn-down follow-up sprint after the assemble selector rewrite
- assemble hot-path payload reduction, pending-count removal, and yEnc guardrails
- `release_file_articles` consolidation into `binary_parts`
- inspection, enrichment, scrape, and release write batching
- inspect media probe-path reduction and dashboard backlog/throughput expansion

## Prior Execution And Reference Docs

### `docs/archive/completed/indexer/INDEXER_PROCESS_EXECUTION_AND_PERFORMANCE_SPRINT.md`

Use for:

- the completed process-execution and performance sprint history
- assemble/release/inspect concurrency decisions
- measured baseline and throughput comparisons from the 2026-04-29 sprint
- reasoning for deferring cross-process assemble/release worker topology

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

### `docs/archive/development/indexer/INDEXER_TEST_QUERIES.md`

Use for:

- validation queries and run commands while executing the current sprint
- checking stage behavior, backlog movement, and release visibility after changes

### `docs/archive/development/indexer/INDEXER_POSTGRES_RUNTIME_TUNING.md`

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

1. Open a new focused doc in `docs/active/` before starting a new indexer execution sprint.
2. Treat `docs/active/INDEXER_DATABASE_STORAGE_RETENTION_AND_OFFLOAD_PLAN.md` as the current active checklist for storage and retention work.
3. Keep the single-binary modular-monolith architecture as the default unless new evidence proves it is the primary bottleneck.
4. Treat multi-worker and multi-process scaling as runtime-topology options, not as a mandate to split the codebase into separate products.
5. Keep measured bottlenecks ahead of speculative architectural change.
6. Keep milestone docs as context, not as the active source of truth when a more focused active execution doc exists.
