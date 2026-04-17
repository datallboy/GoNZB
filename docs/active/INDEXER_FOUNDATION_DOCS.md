# Indexer Foundation Docs

Snapshot date: 2026-04-17

This file exists to keep the indexer docs organized after the stabilization phase completed and the next indexer phase was planned.

We currently have several planning/reference documents. This file defines which ones are active execution guides, which ones were completed and archived, and which ones are design or reference material.

## Current Status

- the 2026 indexer stabilization phase is complete
- the former active execution docs for that phase now live under:
  - `docs/archive/completed/indexer/`
- the next active indexer phase is now organized under `docs/active/`
- that next phase is sequenced as:
  - normalization and storage work first
  - release-quality and API-surface hardening second
  - API and web UI expansion third

## Current Active Docs

### `docs/active/INDEXER_NEXT_PHASE_ROADMAP.md`

Use for:

- the top-level sequence for the next indexer era
- the handoff from completed stabilization into the next feature phase
- phase boundaries and go/no-go rules between phases

### `docs/active/INDEXER_NORMALIZATION_AND_STORAGE_PLAN.md`

Use for:

- Phase 1 execution planning
- storage-shape, identity, and repository-boundary cleanup that should happen before API/UI work hardens current shapes
- classifying what is worth doing now vs later vs not now

### `docs/active/RELEASE_QUALITY_AND_API_SURFACE_HARDENING_PLAN.md`

Use for:

- Phase 2 execution planning
- deciding the minimum stable release contract for initial public-facing list/detail/search work
- defining which release fields stay internal/debug-only

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
2. Start current indexer work from `docs/active/INDEXER_NEXT_PHASE_ROADMAP.md` and then from the current phase doc, not from the archived stabilization docs.
3. Do not begin API/UI expansion work until the roadmap says Phase 1 and Phase 2 are sufficiently complete.
4. Keep milestone docs as context, not as the active source of truth for current execution.
5. Avoid creating new plan docs unless they clearly define the next bounded phase of work.
