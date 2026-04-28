# Indexer Header Storage And Retention Plan

Snapshot date: 2026-04-20

This doc is the subsystem guide for raw-header storage cleanup.

## Current Problem

- `article_headers` dominates DB size and write pressure
- permanent rows currently carry transient ingest payload
- `raw_overview_json` is the largest single payload contributor
- `article_poster_map` adds storage and assembly join cost without helping the long-term hot path

That storage cleanup is now implemented.

The remaining header-adjacent problem is not row size. It is evidence recovery:

- many obfuscated posts still lose useful file/container identity during assembly
- later stages then only see opaque `.bin` file names
- inspect archive/media cannot select those rows because its candidate filters are filename-shaped today

## Permanent Vs Transient Fields

Keep on `article_headers`:
- `id`
- `provider_id`
- `newsgroup_id`
- `article_number`
- `message_id`
- `date_utc`
- `bytes`
- `lines`
- `scraped_at`
- `assembled_at`

Move to `article_header_ingest_payloads`:
- `subject`
- `poster`
- `xref`
- `raw_overview_json`
- `created_at`

## Write-Path Rules

### Scrape ingest

- insert or reuse the permanent `article_headers` row
- upsert the matching transient payload row in `article_header_ingest_payloads`
- keep both writes in one transaction

### Assembly

- read only `article_headers` rows where `assembled_at IS NULL`
- join `article_header_ingest_payloads` by `article_header_id`
- use transient payload for matcher input
- set `assembled_at` when `binary_parts` persistence succeeds

## Remaining Header/Evidence Work

### Why header payload still matters after the storage split

The payload split reduced storage pressure, but the retained pending payload is still the best local source for early recovery signals.

Fields that may help future obfuscated-post recovery:

- `subject`
- `xref`
- `raw_overview_json`
- article ordering and part structure derived from `article_number`, `bytes`, and `lines`

### Current stabilization implementation

The recovery path is now implemented after assembly and before archive/media inspection.

Current persisted recovery fields on `binaries`:

- `recovered_kind`
- `recovered_extension`
- `recovered_source`
- `recovered_confidence`
- `recovered_at`

Possible evidence sources:

- `subject` tokens that survive yEnc stripping
- `base_stem` quality and family-kind hints from matcher output
- `binary_parts.file_name` consistency
- archive/media signatures from sampled article bytes or decoded file prefixes
- `raw_overview_json` fields such as `line` and `references` when they improve grouping confidence

### Candidate design direction

Introduce a lightweight recovery result stored on `binaries` or side evidence tables before inspect archive/media selection.

The recovery output should answer:

- likely container kind:
  - `archive`
  - `media`
  - `par2`
  - `nfo`
  - `unknown`
- likely canonical extension:
  - `.7z`
  - `.rar`
  - `.zip`
  - `.mkv`
  - `.mp4`
  - `.avi`
  - `.par2`
  - `.nfo`
- confidence score
- evidence source:
  - filename
  - subject
  - byte_signature
  - archive_probe
  - media_probe

This should not rewrite raw lineage. It should improve downstream candidate discovery and file-name selection.

## Poster Handling

- remove `article_poster_map`
- keep `posters`
- keep `binaries.poster_id`
- use payload-table `poster` during assembly and matcher input

## Retention Rules

Keep payload rows:
- while `assembled_at IS NULL`
- for `7 days` after `assembled_at`

Purge only:
- `article_header_ingest_payloads` rows where parent `assembled_at < NOW() - INTERVAL '7 days'`

Do not purge in this phase:
- `article_headers`
- `binary_parts`
- `release_file_articles`

## Index Decisions

Add:
- partial pending index on `article_headers (id DESC) WHERE assembled_at IS NULL`

Keep for now:
- `idx_article_headers_newsgroup_id_date_utc`

Reason:
- the app does not currently depend on it for backfill control
- but it was likely intended for future date-oriented raw-header ops
- re-evaluate only after backfill-until-date is validated without local DB date scans

## Backfill-Until-Date

### Goal

- support stopping backfill for a configured group at a configured date such as `2025-01-01`

### Config

- use `indexing.backfill_until_date_by_group`
- values are `YYYY-MM-DD`

### Behavior

- backfill remains article-number-driven
- each XOVER batch is inspected for `DateUTC`
- when the batch oldest date crosses the configured cutoff, stop further backfill for that group
- persist cutoff state in `scrape_checkpoints`

### Fallback

- if a batch has no usable `DateUTC`, ingest it and continue
- only stop when a dated batch crosses the configured threshold

## Out-Of-Scope For This Doc

This doc does not itself define the inspect candidate rewrite, release title rules, or byte-level probing workflow. Those belong in the query/runtime and release/inspect stabilization tracks.
