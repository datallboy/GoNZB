# Indexer Foundation Docs

Snapshot date: 2026-04-22

This file exists to keep the indexer docs organized while the indexer moves from the completed refinement loop into the active Phase 3 API/UI work.

We currently have several planning/reference documents. This file defines which ones are active execution guides, which ones were completed and archived, and which ones are design or reference material.

## Current Status

- Phase 1 and Phase 2 of the next-phase docs are complete
- the stabilization/schema/runtime pass is mostly complete and has been moved to completed-history docs
- the backlog burn-down performance pass and the assemble/release refinement loop were completed and signed off on `2026-04-22`
- Phase 3 API/UI expansion work was completed and signed off on `2026-04-28`
- the active indexer execution focus should now move to post-Phase-3 follow-up workstreams rather than the original API/UI expansion backlog

## Current Active Docs

### `docs/active/INDEXER_NEXT_PHASE_ROADMAP.md`

Use for:

- the top-level sequence for the next indexer era
- the handoff from completed stabilization into the current refinement loop and then the next feature phase
- phase boundaries and go/no-go rules between phases

### `docs/active/INDEXER_API_AND_WEB_UI_EXPANSION_PLAN.md`

Use for:

- completed Phase 3 execution record
- final backend contract rollout decisions for initial user-facing indexer release surfaces
- final sign-off record for the first indexer web UI and admin UI phase

### `docs/active/NEWSNAB_CATEGORY_NORMALIZATION_PLAN.md`

Use for:

- canonical Newsnab category normalization across indexer and aggregator boundaries
- release-category storage, formation, and reform decisions
- public browse/category filtering sign-off and Newznab compatibility alignment

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

### `docs/archive/completed/indexer/INDEXER_ASSEMBLE_AND_RELEASE_REFINEMENT_PLAN.md`

Use for:

- the completed refinement baseline and execution history
- the landed assemble/release prioritization, cooldown, and observability changes
- the live sign-off record that cleared the Phase 3 gate on `2026-04-22`

### `docs/archive/completed/indexer/INDEXER_BACKLOG_BURNDOWN_PERFORMANCE_PLAN.md`

Use for:

- the completed backlog burn-down performance pass
- WorkStreams 1 through 4 validation evidence
- the final throughput and queue-quality sign-off that moved Phase 3 back to the active track

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

### `docs/INDEXER_POSTGRES_RUNTIME_TUNING.md`

Use for:

- the durable PostgreSQL/runtime tuning reference for the indexer
- the current developer-laptop baseline and validation evidence
- tiered operator guidance for dev, lower-end self-hosted, and production systems

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
2. Start current indexer feature work from `docs/active/INDEXER_API_AND_WEB_UI_EXPANSION_PLAN.md`.
3. Use the archived stabilization docs for reference/history, not as the active backlog.
4. Treat Phase 1 and Phase 2 docs as completed/archive material, not the active backlog.
5. Treat the backlog burn-down performance plan and refinement exit criteria as completed/sign-off history, not active blockers.
6. Keep milestone docs as context, not as the active source of truth for current execution.
7. The API/UI expansion plan is the current bounded execution doc for active indexer feature work unless a more focused active execution doc exists for a substream such as category normalization.
