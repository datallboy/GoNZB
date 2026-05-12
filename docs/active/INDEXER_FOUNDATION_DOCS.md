# Indexer Foundation Docs

Snapshot date: 2026-05-12

This file continues the prior indexer foundation doc and keeps the indexer docs organized as execution focus moves beyond the completed performance and process-execution phase.

We currently have several planning and reference documents. This file defines which ones are the current execution guides, which older docs remain useful as reference, and which docs should not drive the current backlog.

## Current Status

- Phase 1 and Phase 2 of the earlier next-phase docs are done
- the stabilization, schema, and runtime pass is done
- the backlog burn-down performance pass and assemble/release refinement loop are done
- the first API/UI expansion phase is done
- the process execution and performance sprint is complete and archived
- the backlog burn-down follow-up sprint is complete and archived
- the assemble lane split, release fragment selection, and storage retention planning docs have been archived as completed/reference work
- the active indexer execution focus now centers on the grouping model re-evaluation for obfuscated posts

## Current Active Docs

### `docs/active/INDEXER_FOUNDATION_DOCS.md`

Use for:

- the active foundation index for current indexer work
- deciding which docs should drive execution right now
- keeping the current sprint pointed at the right workstream

### `docs/active/INDEXER_GROUPING_MODEL_REEVALUATION_PLAN.md`

Use for:

- the active grouping-model follow-up after clean-database release validation
- deciding how XOVER subject set tokens, yEnc file counts, and yEnc part counts should shape binaries and release-family keys
- separating provisional obfuscated file-set identity from releasable release-family identity
- planning schema/runtime changes for identity strength and readiness buckets

### `docs/archive/completed/indexer/RUNTIME_SETTINGS_AND_CONTROL_PLANE_PLAN.md`

Use for:

- the completed runtime settings ownership split
- deciding whether a setting belongs in bootstrap YAML or SQLite runtime state
- control-plane UI and unified admin navigation work
- first-run setup flow and module capability/readiness behavior

## Current Execution Focus

The current focus is grouping correctness for obfuscated posts.

Primary workstream:

- separate provisional obfuscated file-set identity from releasable release-family identity
- use subject set tokens, yEnc file count, and yEnc part count intentionally
- keep yEnc recovery as a promotion path from weak/provisional groups into stronger archive/media/PAR families
- prevent small opaque `misc` releases from forming solely because they are `100%` complete

Recent completed focus:

- assemble backlog burn-down and schema simplification
- assemble throughput and safe concurrency
- release query/write batching
- inspect archive/media concurrency with database reservations
- deferring cross-process topology until measurements justify it
- release fragment selection and weak-candidate cooldowns
- assemble lane A / lane B runtime split
- initial yEnc recovery stage wiring

### `docs/archive/completed/indexer/INDEXER_DATABASE_STORAGE_RETENTION_AND_OFFLOAD_PLAN.md`

Use for:

- the completed database space-reduction and retention planning notes
- table/index size findings from the live dev database
- maintenance configurability and frontend reporting requirements
- NZB blob-cache/offload planning
- evaluating whether `release_file_articles` can be consolidated into `binary_parts`

### `docs/archive/completed/indexer/INDEXER_ASSEMBLE_RELEASE_QUEUE_AND_LANE_SPLIT_EVALUATION.md`

Use for:

- the completed assemble/release stabilization sprint
- measured before/after live backlog benchmarks
- assemble lane A and lane B runtime-control rationale
- the release queue coordination baseline

### `docs/archive/completed/indexer/INDEXER_RELEASE_FRAGMENT_SELECTION_PLAN.md`

Use for:

- the completed release-fragment selection sprint
- why release skipped most candidates as fragments
- which fragment checks moved into summary-time candidate selection
- the baseline for richer release-family readiness summaries before the grouping model re-evaluation

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

1. Open a new focused doc in `docs/active/` before starting a new indexer execution sprint.
2. Treat `docs/active/INDEXER_DATABASE_STORAGE_RETENTION_AND_OFFLOAD_PLAN.md` as the current active checklist for storage and retention work.
3. Keep the single-binary modular-monolith architecture as the default unless new evidence proves it is the primary bottleneck.
4. Treat multi-worker and multi-process scaling as runtime-topology options, not as a mandate to split the codebase into separate products.
5. Keep measured bottlenecks ahead of speculative architectural change.
6. Keep milestone docs as context, not as the active source of truth when a more focused active execution doc exists.
