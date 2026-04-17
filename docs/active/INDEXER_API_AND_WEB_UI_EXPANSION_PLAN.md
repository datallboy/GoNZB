# Indexer API And Web UI Expansion Plan

Snapshot date: 2026-04-17

This is Phase 3 of the next indexer era.

The goal is to ship the first user-facing indexer API and web UI experience on top of the stabilized storage model and the hardened release contract from Phase 2.

## Scope

- the first product-facing release list/search/detail API behavior
- the first web UI views for indexer release data
- backend-to-frontend dependency order for implementation
- rollout and validation criteria for the initial end-user indexer experience

## Goals

- expose a safe first release catalog API on top of the hardened Phase 2 release contract
- build the first release list/search/detail UI in the existing React app under `ui/`
- avoid exposing unstable fields too early
- keep ops/debug routes separate from the first product-facing user experience

## Non-Goals

- reopening Phase 1 normalization and storage questions unless a new blocker appears
- turning binary/file inspection views into end-user product pages
- exposing unstable enrichment or provenance fields because they are already available internally
- broad redesign of non-indexer parts of the UI

## Why This Comes Third

The initial user-facing API and UI should sit on a model that has already had:

- storage and identity cleanup where it was worth doing
- release-quality and field-exposure hardening

If Phase 3 starts earlier, the UI and frontend client will couple to internal/debug shapes that Phase 2 is supposed to prevent from becoming product contracts.

## First Safe API Surface

### Product-Facing Release Endpoints

Use the hardened in-place routes from Phase 2:

- `GET /api/v1/indexer/releases`
- `GET /api/v1/indexer/releases/:id`

Initial behavior should support:

- release list
- release search
- release detail

The response shape should be only the approved Phase 2 release contract.

### Ops / Debug Endpoints That Stay Separate

- `GET /api/v1/indexer/overview`
- `GET /api/v1/indexer/stages`
- `GET /api/v1/indexer/runs`
- `GET /api/v1/indexer/binaries/:id`
- `GET /api/v1/indexer/files/:id`

These should remain available for internal/operator use, but the initial UI should not depend on them as its primary data model.

## First Web UI Views

### 1. Release List View

Purpose:

- show recently posted eligible releases
- provide stable summary fields only

Should include:

- title
- posted time
- size
- file count
- completion/readiness summary
- simple stable badges such as PAR2 and NFO presence where approved

### 2. Release Search View

Purpose:

- allow searching the stable release catalog by the hardened title/search path

Should include:

- same stable summary information as list
- empty, loading, and no-results states

### 3. Release Detail View

Purpose:

- show one stable release record without exposing internal provenance or inspect payloads

Should include:

- stable release metadata from Phase 2
- stable file summaries if approved in the Phase 2 contract

Should not include in the initial product phase:

- binary grouping evidence
- inspect payloads
- external-match payload JSON
- raw source/family/debug keys

## Dependency Order

1. Backend release contract narrowing.
   - finalize the Phase 2 release DTOs and filtering behavior first

2. Frontend client and typed models.
   - create UI-facing types that match only the hardened release contract

3. Release list and search UI.
   - consume the stable list/search contract first

4. Release detail UI.
   - build detail only after the list/search contract is stable

5. Optional debug/operator affordances.
   - if debug links are exposed at all, keep them clearly separate from the end-user experience

## Backend And UI Guardrails

- do not render unstable/internal fields into frontend state models
- do not let the UI depend on binary/file debug routes for the primary release experience
- do not expose `release_key`, source/family keys, or provenance internals just because they are still available internally
- keep module boundaries intact:
  - indexer owns PG-backed catalog behavior
  - API stays transport-focused
  - UI consumes API only

## Commit-Sized Execution Order

1. Finalize the Phase 2 release contract in backend transport types.
   - use explicit product release DTOs rather than raw internal store structs

2. Add backend tests for product list/detail/search behavior.
   - stable field contract
   - pagination
   - suppression of seed/test rows
   - suppression of weak fragment rows

3. Add frontend API client/types for the approved release contract.
   - no debug-only fields

4. Build the release list/search view in `ui/`.
   - loading state
   - error state
   - empty state
   - result cards/rows using stable fields only

5. Build the release detail view in `ui/`.
   - stable metadata
   - stable file summary view if approved

6. Validate rollout behavior.
   - confirm public UI and public release routes stay aligned
   - confirm hidden seed/test rows do not appear
   - confirm unstable/internal fields do not leak into network responses used by the UI

## Validation Criteria

- the UI consumes only the hardened Phase 2 release contract
- the first product release list/search/detail experience works without depending on debug routes
- unstable/internal fields do not appear in UI-facing API responses
- seed/test rows do not appear in public UI
- weak fragmentary rows do not appear in public UI
- pagination, search, empty states, and bad-ID behavior are all covered

## Must Be Complete Before Calling The Initial Experience Shippable

- backend list/detail/search routes match the approved Phase 2 contract
- the UI has working release list, search, and detail views
- the UI does not depend on `release_key`, source/family keys, or inspect/debug payloads
- ops/debug endpoints remain separate from the first product-facing release experience
- any additional enrichment exposure is explicitly deferred into the post-initial-expansion backlog
