# Indexer Schema Target

Snapshot date: 2026-04-16

This document describes the intended stable schema shape for the indexer after the current stabilization phase.

This is not a migration checklist. It is the target data-model reference we should evaluate changes against.

## Design Rules

1. Keep hot operational identity rows small.
2. Do not store the same identity concept in multiple hot columns indefinitely.
3. Keep raw scrape data separate from derived assembly data and separate again from release-catalog data.
4. Move bulky debug evidence and optional enrichment behind side tables when they are not required for the hot path.
5. Treat API/UI-facing shape as a projection from stable core tables, not as the reason core tables become wide.

## Core Table Roles

### `article_headers`

Purpose:

- raw scraped provider/newsgroup article overview data

Contains:

- article number
- message id
- subject
- poster
- posted time
- xref / overview fields needed for assembly

Does not need to become:

- a release-facing query surface
- a long-term home for duplicated release-level metadata

### `binaries`

Purpose:

- one assembled file candidate built from many raw articles

Should contain:

- canonical binary identity
- core family identity used for grouping
- file-level completeness and byte counts
- compact match confidence / status
- compact classification flags needed for release formation
- stable timing fields needed for clustering

Should not grow into:

- a debug blob warehouse
- a full inspection artifact store
- a second release table

### `binary_parts`

Purpose:

- binary-to-article lineage

Should contain:

- binary id
- article header id
- part number
- part counts / bytes needed for binary completeness

Current stabilization note:

- `binary_parts` remains the canonical binary-to-article lineage
- `release_file_articles` still stays materialized for now because current NZB and inspect paths depend on release-file-scoped ordered article refs

### `releases`

Purpose:

- one formed release row representing the NZB/package-facing object

Should contain:

- release identity
- release family identity for reconciliation
- display title / source title state
- release completeness and readiness metrics
- compact release-level flags required for search and filtering

Should stay compact enough that:

- list queries stay cheap
- identity is obvious
- API/UI can rely on it without coupling to every enrichment experiment

## Identity Model

The target identity model is:

### `source_release_key`

Role:

- exact matcher-originated grouping guess
- debug / repair / traceability only

### `release_family_key`

Role:

- family-level grouping key used to gather binaries that should be compared together

### `group_name` / `release_id`

Role:

- final unique identity for one release row
- allows multiple distinct postings of the same family when needed

Rule:

- do not let `release_key`, `source_release_key`, and `release_family_key` coexist as ambiguous synonyms

## Recommended Side Tables

These are the best candidates for normalization or side storage.

### `binary_grouping_evidence`

Suggested role:

- verbose grouping/matcher evidence currently stored in `binaries.grouping_evidence_json`

Shape:

- `binary_id`
- evidence version / source
- JSON payload
- updated timestamp

Why:

- large debug payloads should not live inline on the hottest derived table forever

### `release_titles` or `release_title_candidates`

Suggested role:

- title provenance history and competing title candidates

Why:

- core `releases` should store the current chosen title
- provenance and candidate history are useful but do not need to bloat the main row

### `release_media_metadata`

Suggested role:

- optional media rollups that are helpful but not part of minimal release identity

Examples:

- codecs
- runtime
- stream counts
- subtitle languages
- media tags

### `release_external_ids`

Suggested role:

- optional external enrichment IDs and title mappings

Examples:

- TMDB
- TVDB
- external media type
- original title
- year

## Columns That Need Firm Decisions

These should be classified as keep, move, or defer before API/UI work hardens them.

### On `binaries`

- `release_key`
- `grouping_evidence_json`
- any long text fields that duplicate family identity rather than add new meaning

### On `releases`

- `matched_media_title`
- external-id columns
- season/episode columns
- optional media rollups that could move to side storage

Current stabilization classification:

- `matched_media_title`:
  - defer from API/UI
  - keep in schema temporarily while inspection and enrichment continue to settle
- external-id columns (`tmdb_id`, `tvdb_id`, `external_media_type`, `original_media_title`, `external_year`):
  - defer from API/UI
  - plan to move behind `release_external_ids`
- season/episode columns (`season_number`, `episode_number`, `season_episode_source`):
  - defer from API/UI
  - keep only as enrichment-side data unless the stable release core proves it needs them
- optional media rollups:
  - keep only the compact fields that already support current release quality summaries
  - move richer metadata behind side tables rather than widening `releases`

## Query And Index Rules

1. Keep indexes only for active access patterns.
2. Remove redundant indexes only after checking real query usage or plans.
3. Prefer indexes on stable compact keys over indexes on wide duplicated text columns.
4. Revisit index cost after each identity-column retirement.

## End-State Summary

The stable schema should feel like:

- `article_headers` = raw scrape facts
- `binaries` = compact assembled file facts
- `releases` = compact package facts
- side tables = debug evidence, provenance history, and optional enrichment

That is the line we should hold whenever new fields or tables are proposed.
