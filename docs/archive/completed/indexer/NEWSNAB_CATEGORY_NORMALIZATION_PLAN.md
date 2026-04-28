# Newsnab Category Normalization Plan

Snapshot date: 2026-04-24

## Purpose

Normalize release categories to canonical Newsnab numeric categories and subcategories so the indexer, aggregator, Newznab-compatible API, and public browse UI all consume one stable category system.

This work is intentionally separate from the broader Phase 3 API/UI expansion plan because it changes:

- release formation
- release storage
- public browse behavior
- Newznab caps/search output
- downstream interoperability expectations for aggregator consumers

## Scope

- introduce a shared canonical Newsnab category package
- persist normalized category IDs on indexer releases
- generate category IDs during release formation and release reform
- reuse the same category map for Newznab caps output
- replace public browse heuristics with category-ID-backed filtering
- tighten public visibility so obfuscated/misc archive rows do not leak publicly
- document the supported category matrix and sign-off criteria

## Non-Goals

- building a separate long-running category stage
- adding grab statistics or full Newznab metrics parity
- introducing a hard runtime dependency between aggregator and indexer modules
- changing downloader path/category behavior in this pass

## Architectural Decision

Category normalization lives in a shared dedicated package:

- `internal/categories/newsnab`

Reason:

- the indexer needs it during release formation and reform
- the aggregator/Newznab compatibility layer needs the same canonical definitions for caps and category interpretation
- the modular monolith boundary stays clean if category semantics are owned by a neutral shared package instead of either module

## Canonical Category Matrix

### Root categories

- `1000` Console
- `2000` Movies
- `3000` Audio
- `4000` PC
- `5000` TV
- `6000` XXX
- `7000` Books
- `8000` Other

### Subcategories

- Console
  - `1010` NDS
  - `1020` PSP
  - `1030` Wii
  - `1035` Switch
  - `1040` Xbox
  - `1050` Xbox 360
  - `1080` PS3
  - `1090` Xbox One
  - `1100` PS4
- Movies
  - `2010` Foreign
  - `2020` Other
  - `2030` SD
  - `2040` HD
  - `2045` UHD
  - `2050` BluRay
  - `2060` 3D
- Audio
  - `3010` MP3
  - `3020` Video
  - `3030` Audiobook
  - `3040` Lossless
  - `3050` Podcast
- PC
  - `4010` 0day
  - `4020` ISO
  - `4030` Mac
  - `4040` Mobile-Other
  - `4050` Games
  - `4060` Mobile-iOS
  - `4070` Mobile-Android
  - `4080` 3dModels
- TV
  - `5020` Foreign
  - `5030` SD
  - `5040` HD
  - `5045` UHD
  - `5050` Other
  - `5060` Sport
  - `5070` Anime
  - `5080` Documentary
- XXX
  - `6010` DVD
  - `6020` WMV
  - `6030` SD
  - `6040` HD
  - `6045` UHD
  - `6050` Pack
  - `6060` ImgSet
  - `6070` Other
- Books
  - `7010` Mags
  - `7020` Ebook
  - `7030` Comics
- Other
  - `8010` Misc

## Source Of Truth Rules

The shared package owns:

- numeric category IDs
- root/subcategory names
- browse slugs
- ID-to-label lookups
- release evidence to category resolution

No controller, store query, or UI helper should maintain a separate hardcoded Newsnab category tree after this plan lands.

## Category Generation Decision

Category assignment is part of the release command and release reform flow.

It is not a separate command or scheduler stage.

Reason:

- category is a release read-model attribute, not a standalone pipeline product
- the required evidence already exists in release formation inputs and release-side metadata
- `indexer release --once --reform` already provides the deterministic backfill path for existing rows

## Release Category Evidence

The release command should derive the category from the best available combination of:

- release `classification`
- `external_media_type`
- `primary_resolution`
- `primary_audio_codec`
- release title and deobfuscated title
- matched media title
- PreDB category and genre when available
- title/platform tokens such as `ps4`, `switch`, `anime`, `flac`, `ebook`, `xxx`

The initial category assignment should prefer deterministic structural signals over weak text guesses:

1. explicit media domain hints such as TV/movie/audio
2. platform-specific console/PC/mobile tokens
3. resolution/audio tokens for subcategory selection
4. conservative fallback to `8010`

## Public Visibility Policy Changes

Public browse should only surface releases that are both:

- release-ready by the existing public thresholds
- public-safe by title/category quality

Additional suppression rules:

- obscure archive/misc rows without readable title evidence stay admin-only
- category `8000/8010` rows should not surface publicly unless a trusted deobfuscated title/evidence source is present

## Execution Plan

1. Create the shared `internal/categories/newsnab` package with canonical definitions and lookup helpers.
2. Add normalized category fields to the release catalog schema.
3. Update release formation to assign category ID and category label.
4. Ensure release reform repopulates categories through the same path.
5. Replace Newznab caps hardcoding with the shared package.
6. Replace public browse heuristic filtering with category-ID-backed filtering.
7. Tighten public visibility to suppress unreadable misc/archive rows.
8. Update stable API/admin DTOs as needed to expose the normalized category information cleanly.
9. Add unit and repository tests for category resolution, storage, caps output, and public filtering.
10. Document results and sign-off status in this plan.

## Validation

Minimum validation required:

- `go test ./internal/categories/...`
- `go test ./internal/indexing/release/...`
- `go test ./internal/store/pgindex/...`
- `go test ./internal/api/...`
- `npm run build`

## Sign-Off Checklist

- [x] shared Newsnab category package exists and is the only canonical category tree
- [x] releases persist numeric category IDs
- [x] release formation and reform populate those IDs
- [x] public browse uses numeric categories instead of string heuristics
- [x] Newznab caps output uses the shared definitions
- [x] obfuscated misc/archive rows no longer leak to the public catalog
- [x] tests and UI build pass

## Execution Notes

- 2026-04-24: branch creation was attempted for `feat/newsnab-category-normalization` but the current environment could not write `.git` refs, so implementation proceeds in the current worktree until branch creation is available again.
- 2026-04-24: added shared `internal/categories/newsnab` package with canonical root/subcategory definitions, browse slugs, display-name parsing, and release-category resolution heuristics.
- 2026-04-24: added `releases.category_id` migration and wired release formation to persist normalized category IDs and labels.
- 2026-04-24: replaced public browse string heuristics with category-ID-backed filtering and added public suppression for unreadable `Other > Misc` rows without trusted title evidence.
- 2026-04-24: replaced Newznab caps hardcoding with the shared category package and normalized RSS category output to use canonical numeric category attrs with matching labels.
- 2026-04-24: aligned the public browse UI subcategory routes with the canonical Newsnab slugs and validated Go tests plus the web UI production build.
