# Indexer Foundation Docs

Snapshot date: 2026-05-14

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
- the assemble lane split, release fragment selection, and storage retention planning docs have been archived as completed/reference work
- the grouping-model re-evaluation sprint is complete and ready to archive
- the database growth trim sprint is complete from a code and schema standpoint
- the obfuscated-payload hardening sprint is active for downloader import/extraction hardening, yEnc/PAR2 backlog visibility, and recovered-identity grouping follow-up
- remaining database-growth follow-up is operational reclaim plus longer-run post-merge measurement on `dv`

## Current Active Docs

### `docs/active/INDEXER_FOUNDATION_DOCS.md`

Use for:

- the active foundation index for current indexer work
- deciding which docs should drive execution right now
- keeping the current sprint pointed at the right workstream

### `docs/active/INDEXER_OBFUSCATED_PAYLOAD_FINDINGS.md`

Use for:

- current findings from obfuscated header patterns and legacy NZB import encoding
- downloader parser hardening follow-up for legacy XML charset declarations
- downloader post-process hardening for extensionless archive payloads
- baseline, audit findings, and sign-off tracking for the current obfuscated-payload sprint
- evaluating cross-newsgroup release grouping only when recovered identity evidence is strong

### `docs/INDEXER_CURRENT_SCHEMA_AND_SYSTEM_INTERACTIONS.md`

Use for:

- the living whole-system schema map for the current indexer
- determining which tables are canonical versus derived before cleanup
- tracing how ingest, assemble, recovery, release, inspect, and maintenance interact across the current schema

## Recently Completed Sprint Docs

### `docs/archive/completed/indexer/2026-05-14-indexer-database-growth-trim/INDEXER_DATABASE_GROWTH_TRIM_PLAN.md`

Use for:

- the completed storage-trim and retention-reduction sprint closeout
- the execution history for the database growth trim workstream
- the resolved versus deferred outcomes from that sprint

### `docs/archive/completed/indexer/2026-05-14-indexer-database-growth-trim/INDEXER_DATABASE_SCHEMA_AUDIT.md`

Use for:

- the completed live schema and column-usage audit for the database growth trim sprint
- documenting which hot-table columns were proven canonical, derived, debug-only, or drop candidates during that sprint
- reconciling Docker Postgres schema truth with active migrations and current code usage at sprint close

### `docs/archive/completed/indexer/RUNTIME_SETTINGS_AND_CONTROL_PLANE_PLAN.md`

Use for:

- the completed runtime settings ownership split
- deciding whether a setting belongs in bootstrap YAML or SQLite runtime state
- control-plane UI and unified admin navigation work
- first-run setup flow and module capability/readiness behavior

## Current Execution Focus

The just-completed focus was database growth trimming and retention reduction after the grouping sprint proved the yEnc/PAR2 promotion path was working.

Primary active workstream:

- harden downloader handling for legacy-encoded and extensionless obfuscated payloads
- make yEnc recovery and PAR2 backlog visibility exact enough for capacity tuning
- audit release grouping boundaries before adding bounded cross-group recovered-identity promotion

Completed database-growth workstream:

- reduce ingest and audit-table growth
- distinguish canonical rows from debug/audit retention
- preserve the landed grouping/release gains while making long-running supervisor operation sustainable

Current remaining database-growth follow-up:

- free root-volume space so the documented `VACUUM FULL` reclaim pass can run
- rerun sustained ingest measurements on `dv` after merge

Recent completed focus:

- assemble backlog burn-down and schema simplification
- assemble throughput and safe concurrency
- release query/write batching
- inspect archive/media concurrency with database reservations
- deferring cross-process topology until measurements justify it
- release fragment selection and weak-candidate cooldowns
- assemble lane A / lane B runtime split
- initial yEnc recovery stage wiring
- grouping-model re-evaluation, PAR2 target persistence, and recovery-driven promotion

### `docs/archive/completed/indexer/INDEXER_DATABASE_STORAGE_RETENTION_AND_OFFLOAD_PLAN.md`

Use for:

- the completed database space-reduction and retention planning notes
- table/index size findings from the live dev database
- maintenance configurability and frontend reporting requirements
- NZB blob-cache/offload planning
- evaluating whether `release_file_articles` can be consolidated into `binary_parts`

### `docs/archive/completed/indexer/INDEXER_GROUPING_MODEL_REEVALUATION_PLAN.md`

Use for:

- the completed grouping-model sprint
- yEnc recovery and PAR2 target persistence decisions
- the live validation evidence that actionable archive/contextual families improved before storage trimming became the next blocker

### `docs/archive/completed/indexer/INDEXER_SCHEMA_AND_SERVICE_DATAFLOW.md`

Use for:

- the completed schema/dataflow reference map for current table ownership
- understanding which `binary_*` tables are canonical identity tables versus inspection/evidence tables during the storage-trim sprint

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
2. Treat `docs/INDEXER_CURRENT_SCHEMA_AND_SYSTEM_INTERACTIONS.md` as the living system map. Use the archived growth-trim sprint docs under `docs/archive/completed/indexer/2026-05-14-indexer-database-growth-trim/` when you need the completed audit or execution history.
3. Keep the single-binary modular-monolith architecture as the default unless new evidence proves it is the primary bottleneck.
4. Treat multi-worker and multi-process scaling as runtime-topology options, not as a mandate to split the codebase into separate products.
5. Keep measured bottlenecks ahead of speculative architectural change.
6. Keep milestone docs as context, not as the active source of truth when a more focused active execution doc exists.
