# Indexer Foundation Docs

Snapshot date: 2026-04-20

This file exists to keep the indexer docs organized while the indexer is in a feature-freeze stabilization and cleanup phase ahead of v1-facing work.

We currently have several planning/reference documents. This file defines which ones are active execution guides, which ones were completed and archived, and which ones are design or reference material.

## Current Status

- Phase 1 and Phase 2 of the next-phase docs are complete
- the current active execution focus is a dedicated stabilization, schema-cleanup, and maintenance pass
- Phase 3 API/UI work remains deferred until this stabilization pass is complete

## Current Active Docs

### `docs/active/INDEXER_NEXT_PHASE_ROADMAP.md`

Use for:

- the top-level sequence for the next indexer era
- the handoff from completed stabilization into the next feature phase
- phase boundaries and go/no-go rules between phases

### `docs/active/INDEXER_DB_STABILIZATION_PHASE_PLAN.md`

Use for:

- the top-level execution plan for the current stabilization pass
- measured storage/query baseline and acceptance criteria
- migration/reset strategy and rollout order

### `docs/active/INDEXER_HEADER_STORAGE_AND_RETENTION_PLAN.md`

Use for:

- raw-header schema cleanup
- payload split, retention, and backfill-until-date behavior
- article-header index decisions and poster-map removal

### `docs/active/INDEXER_QUERY_AND_RUNTIME_CLEANUP_PLAN.md`

Use for:

- assembly/release hot query cleanup
- dirty-family queue behavior
- stale operational data maintenance and purge policy

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
2. Start current indexer work from `docs/active/INDEXER_DB_STABILIZATION_PHASE_PLAN.md`, then use the header-storage and query/runtime cleanup docs for subsystem details.
3. Treat Phase 1 and Phase 2 docs as completed/archive material, not the active backlog.
4. Do not begin API/UI expansion work until the current stabilization docs say the feature freeze is complete.
5. Keep milestone docs as context, not as the active source of truth for current execution.
6. Avoid creating new plan docs unless they clearly define the next bounded phase of work.
