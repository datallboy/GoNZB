# Indexer Foundation Docs

Snapshot date: 2026-04-22

This file exists to keep the indexer docs organized while the indexer is between the completed stabilization pass and the deferred Phase 3 API/UI work.

We currently have several planning/reference documents. This file defines which ones are active execution guides, which ones were completed and archived, and which ones are design or reference material.

## Current Status

- Phase 1 and Phase 2 of the next-phase docs are complete
- the stabilization/schema/runtime pass is mostly complete and has been moved to completed-history docs
- the current active execution focus is now the backlog burn-down performance pass that follows the initial refinement implementation:
  - assemble backlog reduction
  - release queue throughput and queue quality
  - PostgreSQL, query, and selector efficiency for partially completed records
- Phase 3 API/UI work remains deferred until this refinement loop is complete

## Current Active Docs

### `docs/active/INDEXER_NEXT_PHASE_ROADMAP.md`

Use for:

- the top-level sequence for the next indexer era
- the handoff from completed stabilization into the current refinement loop and then the next feature phase
- phase boundaries and go/no-go rules between phases

### `docs/active/INDEXER_BACKLOG_BURNDOWN_PERFORMANCE_PLAN.md`

Use for:

- the current active backlog
- backlog burn-down performance work across assemble, release, and PostgreSQL
- the current execution order for selector, queue, and runtime throughput work
- the evidence needed before the refinement loop can be considered complete

### `docs/active/INDEXER_ASSEMBLE_AND_RELEASE_REFINEMENT_PLAN.md`

Use for:

- the baseline refinement plan implemented on 2026-04-21
- the already-landed assemble/release behavior changes that this new plan builds on
- the original refinement-phase exit criteria that still need live sign-off

### `docs/active/INDEXER_API_AND_WEB_UI_EXPANSION_PLAN.md`

Use for:

- Phase 3 execution planning
- backend contract rollout for initial user-facing indexer release surfaces
- first indexer web UI views built on the hardened release contract

## Archived Completed Indexer Docs

### `docs/archive/completed/indexer/INDEXER_STABILIZATION_WORKLIST.md`

Use for:

- the completed stabilization backlog
- the validation history and sign-off for that phase
- the final checklist of what was done and why

### `docs/archive/completed/indexer/INDEXER_DB_STABILIZATION_PHASE_PLAN.md`

Use for:

- the completed top-level stabilization execution plan
- measured storage/query baseline and acceptance criteria from that phase
- migration/reset strategy and rollout order that were implemented

### `docs/archive/completed/indexer/INDEXER_HEADER_STORAGE_AND_RETENTION_PLAN.md`

Use for:

- the completed raw-header storage cleanup plan
- payload split, retention, and backfill-until-date design history
- article-header index decisions and poster-map removal history

### `docs/archive/completed/indexer/INDEXER_QUERY_AND_RUNTIME_CLEANUP_PLAN.md`

Use for:

- the completed assembly/release hot-query cleanup plan
- dirty-family queue baseline design
- stale operational data maintenance and purge policy history

### `docs/archive/completed/indexer/INDEXER_RELEASE_FORMATION_SNAPSHOT_AND_PLAN.md`

Use for:

- the stabilized release-formation model from that phase
- the final release-identity and clustering notes
- historical reference while planning the next release-quality iteration

### `docs/archive/completed/indexer/INDEXER_SCHEMA_TARGET.md`

Use for:

- the stabilized schema target from that phase
- side-table / hot-row boundary decisions that were signed off
- historical reference while planning future normalization work

### `docs/archive/completed/indexer/INDEXER_NORMALIZATION_AND_STORAGE_PLAN.md`

Use for:

- completed Phase 1 execution history
- historical storage and repository-boundary decisions before the current stabilization pass

### `docs/archive/completed/indexer/RELEASE_QUALITY_AND_API_SURFACE_HARDENING_PLAN.md`

Use for:

- completed Phase 2 execution history
- historical release-contract hardening decisions before the current stabilization pass

## Current Reference Docs

### `docs/INDEXER_HOW_IT_WORKS.md`

Use for:

- quick terminology
- a short explanation of the current pipeline objects

### `docs/ARCHITECTURE.md`

Use for:

- module-level boundaries
- how the indexer fits into the larger app architecture

### `docs/INDEXER_TEST_QUERIES.md`

Use for:

- validation queries and run commands while executing the next phase
- checking release visibility, catalog quality, and stage behavior after changes

## Broader Context Docs

### `docs/archive/INDEXER_BACKEND_MILESTONES.md`

Use for:

- milestone history
- broader backlog context
- future work ideas

Do not use this as the day-to-day stabilization execution guide.

### `docs/archive/INDEXER_UI_API_ROADMAP.md`

Use for:

- future API/UI planning

Do not let this drive schema expansion before the stabilization docs say the foundation is ready.

## Guideline Rules

1. Do not treat the completed docs under `docs/archive/completed/indexer/` as an active backlog.
2. Start current indexer performance work from `docs/active/INDEXER_BACKLOG_BURNDOWN_PERFORMANCE_PLAN.md`.
3. Use the archived stabilization docs for reference/history, not as the active backlog.
4. Treat Phase 1 and Phase 2 docs as completed/archive material, not the active backlog.
5. Do not begin API/UI expansion work until the backlog burn-down performance plan and refinement exit criteria are signed off.
6. Keep milestone docs as context, not as the active source of truth for current execution.
7. The backlog burn-down performance plan is the current bounded execution doc for active indexer throughput work.
