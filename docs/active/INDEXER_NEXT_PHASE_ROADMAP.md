# Indexer Next Phase Roadmap

Snapshot date: 2026-04-20

This is the roadmap and sequencing reference for the next indexer era.

Use this together with `docs/active/INDEXER_DB_STABILIZATION_PHASE_PLAN.md`.

Completed baseline:

- `docs/archive/completed/indexer/INDEXER_STABILIZATION_WORKLIST.md`
- `docs/archive/completed/indexer/INDEXER_RELEASE_FORMATION_SNAPSHOT_AND_PLAN.md`
- `docs/archive/completed/indexer/INDEXER_SCHEMA_TARGET.md`

Current reference docs:

- `docs/active/INDEXER_FOUNDATION_DOCS.md`
- `docs/INDEXER_HOW_IT_WORKS.md`
- `docs/INDEXER_TEST_QUERIES.md`
- `docs/ARCHITECTURE.md`

## Why A New Phase Exists

The stabilization phase is complete and remains signed off.

That work made the indexer backend trustworthy enough to continue, but it did not make the current storage shape, release contract, or API/UI surface ready to harden directly into product-facing behavior.

The next phase exists to avoid freezing current internal/debug shapes into long-lived API and UI contracts.

Since the original Phase 1 and Phase 2 execution work is complete, the active short-term focus is now a stabilization and cleanup pass before Phase 3 starts.

## Current Baseline

The following should be treated as complete unless a new issue is discovered:

- `binaries.posted_at` population and timing repair
- `release_files.posted_at` population and timing repair
- blank release identity repair
- `binary_grouping_evidence` side-table move
- redundant descending `article_headers` index removal
- stabilization validation and sign-off

Current realities that matter for sequencing:

- `article_headers` is the largest table and remains a raw high-volume fact table
- some release-family and source keys are still low-quality fallback strings for fragmentary rows
- synthetic seed/test-style release rows exist in the dev DB
- `internal/store/pgindex/repository.go` is still very large and mixes multiple storage/read-model concerns
- the current `/api/v1/indexer/*` release/file/binary endpoints are closer to internal inspection/debug surfaces than a stable product contract

## Phase Sequence

### Current Stabilization Phase

Purpose:

- finish the storage/query/runtime cleanup that remains after completed Phase 1 and Phase 2 work
- reduce DB/write pressure and stale operational data before API/UI expansion resumes

Primary documents:

- `docs/active/INDEXER_DB_STABILIZATION_PHASE_PLAN.md`
- `docs/active/INDEXER_HEADER_STORAGE_AND_RETENTION_PLAN.md`
- `docs/active/INDEXER_QUERY_AND_RUNTIME_CLEANUP_PLAN.md`

### Phase 3: Indexer API And Web UI Expansion Plan

Purpose:

- build the first user-facing indexer overview/list/detail/search experience on the hardened model from Phase 2

Primary document:

- `docs/active/INDEXER_API_AND_WEB_UI_EXPANSION_PLAN.md`

## Phase Boundary Rules

### Phase 1 Boundary

Phase 1 may:

- change storage shape where there is a measured or clearly justified practical win
- retire remaining legacy identity compatibility where it blocks hardening
- split store/query responsibilities so later API work does not depend on `repository.go` as one monolith

Phase 1 should not:

- reopen signed-off stabilization work without a new issue
- normalize raw high-cardinality article fields for theoretical purity
- drift into UI work

### Phase 2 Boundary

Phase 2 may:

- decide which release rows are eligible for initial public exposure
- define the minimum stable release contract for list/detail/search
- harden current indexer release routes in place

Phase 2 should not:

- restart large storage debates that belong in Phase 1
- expose internal/debug fields just because they already exist in current DTOs
- build major UI features

### Phase 3 Boundary

Phase 3 may:

- implement backend contract changes needed for first product-facing release views
- build the first indexer web UI views against the hardened release contract

Phase 3 should not:

- reopen Phase 1 storage questions unless a new blocker appears
- make the UI depend on unstable/internal release fields
- turn binary/file inspection routes into product views by accident

## Go / No-Go Gates

Do not start Phase 3 until:

- the current stabilization docs are complete and signed off
- the DB cleanup, header retention, and maintenance changes are implemented and validated
- backfill-until-date behavior is working and restart-safe

## Working Assumptions For This Next Phase

1. The current `/api/v1/indexer/*` release routes will be hardened in place rather than replaced with a second public namespace.
2. Synthetic seed/test-style rows should be hidden from initial public-facing list/search/detail surfaces.
3. `release_key`, `source_release_key`, and `release_family_key` should be treated as internal or debug identity, not user-facing product identity.
4. `article_headers` should stay a raw high-volume fact table. Retention, partitioning, and index discipline should be considered before lookup-table normalization of raw fields like subject, message-id, or xref.

## Deferred Explicitly Until After Initial API/UI Expansion

These may be valuable later, but they should not be stuffed into Phases 1 or 2 unless they become a concrete blocker:

- broad enrichment-schema redesign beyond what is needed to harden the initial release contract
- product-facing exposure of external metadata and provenance payloads
- redesign of inspect/debug binary and file explorer surfaces into end-user experiences
- ambitious `article_headers` normalization not driven by measured storage or hot-path pressure
- replacing `release_file_articles` with on-demand derivation unless current measurements show it is a practical blocker
