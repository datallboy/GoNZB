# Indexer Foundation Docs

Snapshot date: 2026-04-16

This file exists to keep the active indexer docs organized.

We currently have several planning/reference documents. This file defines which ones are active execution guides and which ones are design or reference material.

## Use These As The Active Execution Docs

### 1. `docs/active/INDEXER_STABILIZATION_WORKLIST.md`

Use for:

- the current stabilization backlog
- sequencing work into reviewable chunks
- validation rules
- repair/rebuild process

This is the primary execution-plan document right now.

### 2. `docs/active/INDEXER_RELEASE_FORMATION_SNAPSHOT_AND_PLAN.md`

Use for:

- the target release-formation behavior
- the rules we want the release pipeline to follow
- comparing the current implementation against the intended design

This is the primary release-design document right now.

## Use These As End-State Reference Docs

### `docs/active/INDEXER_SCHEMA_TARGET.md`

Use for:

- the intended stable schema shape
- table boundaries
- what belongs in hot rows vs side tables

### `docs/INDEXER_HOW_IT_WORKS.md`

Use for:

- quick terminology
- a short explanation of the current pipeline objects

### `docs/ARCHITECTURE.md`

Use for:

- module-level boundaries
- how the indexer fits into the larger app architecture

## Use These As Broader Context, Not The Active Backlog

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

1. When doing stabilization work, update `docs/active/INDEXER_STABILIZATION_WORKLIST.md`.
2. When changing the intended release model, update `docs/active/INDEXER_RELEASE_FORMATION_SNAPSHOT_AND_PLAN.md`.
3. When changing the intended schema end state, update `docs/active/INDEXER_SCHEMA_TARGET.md`.
4. Keep milestone docs as context, not as the active source of truth for current execution.
5. Avoid creating new "plan" docs unless they clearly replace or narrow one of the active docs above.
