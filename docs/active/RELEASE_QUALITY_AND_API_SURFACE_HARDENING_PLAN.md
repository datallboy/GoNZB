# Release Quality And API Surface Hardening Plan

Snapshot date: 2026-04-17

This is Phase 2 of the next indexer era.

The goal is to make sure the first public-facing release API and UI depend only on stable release semantics and not on current debug, provenance, or enrichment internals.

## Scope

- release visibility and suppression rules for initial public-facing list/detail/search
- defining the minimum stable release contract
- hardening the current `/api/v1/indexer/releases` routes in place
- separating product release behavior from internal binary/file/debug behavior

## Goals

- decide whether stabilized release formation is good enough for initial API/UI once weak rows are filtered
- define the minimum stable release contract field-by-field
- keep unstable fields and weak rows out of the first product-facing contract
- prevent current store/query shapes from leaking directly into product behavior

## Non-Goals

- reopening Phase 1 storage debates unless a new blocker appears
- exposing every existing release column because it already exists in storage
- promoting binary/file inspection routes into product endpoints
- broad UI implementation work

## Why This Comes In Phase 2

Phase 1 should settle the storage and identity boundaries that are worth fixing first.

Once that is done, Phase 2 can harden a stable release contract without freezing internal fields, fragment rows, or debug payloads into long-lived product behavior.

This phase comes before UI work so the frontend is built on a deliberate contract instead of on whatever the current internal DTOs happen to return.

## Current Risks This Phase Must Solve

- the current `/api/v1/indexer/releases` responses still expose fields such as:
  - `release_key`
  - `deobfuscated_title`
  - `matched_media_title`
  - external IDs and external media type
  - season and episode fields
  - title provenance fields
- current release detail also returns enrichment and match payloads that are useful for internal inspection but are not part of a minimal stable public release contract
- current binary and file detail routes expose grouping evidence and inspect/debug details that are not product-facing
- fragmentary releases and synthetic seed/test rows can exist in the catalog and must not leak into initial user-facing surfaces

## Core Answers

### Is Release Formation Solid Enough For Initial API/UI?

Yes, with a boundary.

The stabilized release formation is good enough for initial API/UI if Phase 2 adds explicit release eligibility and suppression rules and keeps unstable/internal fields out of the first public contract.

This phase should not restart release-formation redesign unless a new defect is found. It should define which formed rows are eligible for public exposure.

### How Should Incomplete Fragmentary Releases Be Treated?

Initial public-facing list/search/detail should suppress weak fragmentary rows rather than expose them with caveats.

Phase 2 should define a release eligibility policy using current stable signals such as:

- completion and readiness metrics
- file composition signals
- confidence and identity-status signals
- absence of seed/test classification

The exact threshold values should be chosen in implementation, but the policy boundary should be:

- suppress rows that are too fragmentary or too weak to represent a stable release object
- allow internal/debug surfaces to continue inspecting those rows if needed

Current hardened implementation for public `/api/v1/indexer/releases` list/detail/search:

- require non-empty `search_title`
- reject placeholder titles where trimmed lowercase `title` is `unknown-release`
- require `match_confidence >= 0.55`
- require `completion_pct >= 50`
- require `identity_status` in `identified` or `probable`
- require either:
  - `expected_file_count <= 1`
  - or `file_count >= 2`
- suppress rows where `search_title` or `group_name` matches seed/test-style naming

### How Should Short Or Opaque Release/Source/Family Keys Be Treated?

They stay internal.

- do not use them as display titles
- do not use them as user-facing identifiers
- do not expose them as part of the minimum stable public contract

### Should `release_key` Be Kept Internal?

Yes.

`release_key` should remain compatibility/debug-only. It should not be treated as product identity for initial public list/detail/search behavior.

### How Should Seed/Test-Style Rows Be Treated?

Hide them from the initial public-facing list/search/detail contract.

If they need to remain visible for debugging or seeding workflows, that should stay in internal/debug surfaces only.

## Minimum Stable Release Contract

The first public-facing release contract should be intentionally small.

### Stable For Initial List / Search

- `release_id`
- `guid`
- chosen display `title`
- `posted_at`
- `size_bytes`
- `file_count`
- `completion_pct`
- `has_par2`
- `has_nfo`
- stable password/encryption state only if already trustworthy
- stable readiness/quality summary fields that do not expose enrichment internals

Current hardened list/search DTO:

- `release_id`
- `guid`
- `title`
- `posted_at`
- `size_bytes`
- `file_count`
- `completion_pct`
- `has_par2`
- `has_nfo`
- `password_state`
  - only `not_passworded`, `passworded_known`, and `passworded_unknown` are emitted
  - raw `unknown` and other unstable values are suppressed from the public payload
- `availability_score`
- `availability_tier`
- `media_quality_score`
- `media_quality_tier`

### Stable For Initial Detail

- everything from list/search that remains relevant
- stable file summaries needed for release detail:
  - file name
  - size
  - file index when meaningful
- basic posted-at and completeness signals if those are already stable
- high-level release status summaries that are already trustworthy

Current hardened detail-only file summary DTO:

- `file_name`
- `size_bytes`
- `file_index`
- `is_pars`
- `posted_at`
- `article_count`
- `total_parts`
- `observed_parts`

### Internal / Debug-Only For Initial Product Phase

- `release_key`
- `source_release_key`
- `release_family_key`
- `deobfuscated_title`
- `matched_media_title`
- `title_source`
- title-confidence and provenance internals
- external IDs and external-media-type fields
- season and episode provenance
- predb, TMDB, and TVDB match payloads
- grouping evidence
- inspect artifact payloads and binary/file debug detail payloads

## Route Hardening Direction

### Harden In Place

Use the current `/api/v1/indexer/releases` and `/api/v1/indexer/releases/:id` routes as the initial product-facing release surfaces, but narrow their contract intentionally.

Do not create a second public namespace for the first release catalog surface.

### Keep These Internal / Debug-Oriented

- `/api/v1/indexer/overview`
- `/api/v1/indexer/stages`
- `/api/v1/indexer/runs`
- `/api/v1/indexer/binaries/:id`
- `/api/v1/indexer/files/:id`

Those routes may still be valuable, but they should not define the first end-user product contract.

## Query And Code Refactors Required Before Phase 3

- stop using the current wide `IndexerReleaseSummary` and `IndexerReleaseDetail` shapes as the implicit product contract
- introduce explicit product release DTOs or projections for list/search/detail
- keep internal inspect/debug DTOs separate from public release DTOs
- make sure public release search and detail queries do not depend on internal/debug columns being present in the response

## Commit-Sized Execution Order

1. Inventory current route and DTO exposure.
   - list every field currently returned by release list/detail responses
   - list every field currently returned by binary and file detail responses

2. Classify release-facing fields.
   - stable for initial public contract
   - keep internal but preserve for debug/ops
   - internal now and candidate for later removal from current responses

3. Define release visibility policy.
   - decide how weak fragment rows are suppressed
   - decide how seed/test rows are suppressed
   - decide how rows with weak titles or opaque fallback identity are handled

4. Harden the release routes in place.
   - narrow `/api/v1/indexer/releases`
   - narrow `/api/v1/indexer/releases/:id`
   - keep debug/ops routes out of product UI assumptions

5. Add explicit tests and validation queries.
   - suppression tests for fragmentary rows
   - suppression tests for seed/test rows
   - field-level contract tests so unstable fields do not leak back in

## Validation Criteria

- release list/detail/search no longer expose `release_key` or other unstable identity/debug fields as product contract
- public release responses do not expose provenance, external-match, or inspect/debug payload fields
- weak fragmentary rows are suppressed from initial public-facing behavior
- synthetic seed/test rows are suppressed from initial public-facing behavior
- binary/file inspection routes remain clearly internal/debug-only
- the resulting release contract is stable enough for a UI to consume without depending on enrichment internals

## Must Be Complete Before Phase 3

- the minimum stable release contract is written down field-by-field
- release visibility and suppression rules are decided
- the current release routes are hardened in place or an equivalent in-place hardening path is fully defined
- internal/debug-only fields are explicitly named
- UI work can start without depending on:
  - `release_key`
  - source/family identity keys
  - provenance internals
  - enrichment internals
  - binary/file inspection payloads
