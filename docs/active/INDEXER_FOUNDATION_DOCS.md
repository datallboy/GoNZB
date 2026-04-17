# Indexer Foundation Docs

Snapshot date: 2026-04-17

This file exists to keep the indexer docs organized after the stabilization phase completed.

We currently have several planning/reference documents. This file defines which ones are active execution guides, which ones were completed and archived, and which ones are design or reference material.

## Current Status

- the 2026 indexer stabilization phase is complete
- the former active execution docs for that phase now live under:
  - `docs/archive/completed/indexer/`
- there is no active indexer stabilization backlog in `docs/active/` right now
- start a new active plan only when we intentionally begin the next indexer phase

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

## Current Reference Docs

### `docs/INDEXER_HOW_IT_WORKS.md`

Use for:

- quick terminology
- a short explanation of the current pipeline objects

### `docs/ARCHITECTURE.md`

Use for:

- module-level boundaries
- how the indexer fits into the larger app architecture

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
2. When starting the next indexer phase, create a new active plan in `docs/active/` instead of reopening the completed stabilization worklist.
3. Keep milestone docs as context, not as the active source of truth for current execution.
4. Avoid creating new plan docs unless they clearly define the next bounded phase of work.
