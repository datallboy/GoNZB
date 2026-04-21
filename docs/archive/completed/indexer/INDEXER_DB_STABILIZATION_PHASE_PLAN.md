# Indexer DB Stabilization Phase Plan

Snapshot date: 2026-04-20

This is the active execution doc for the current feature-freeze stabilization pass.

## Purpose

- reduce write-path cost on the Postgres-backed indexer
- shrink unnecessary or dead schema/storage
- harden stale operational data cleanup
- leave the repo and schema in a safer state before Phase 3 API/UI work resumes

## Measured Baseline

- `article_headers`: `32 GB` total, `26 GB` heap, `5827 MB` indexes, `33,174,242` live rows, `4,306,611` dead tuples
- `binaries`: `3607 MB`
- `binary_parts`: `1511 MB`
- `release_file_articles`: `521 MB`
- `article_poster_map`: `403 MB`
- `raw_overview_json` averages `295` bytes and is about `9.11 GiB` of inline heap
- assembly fetch for `5,000` pending headers takes about `26.46s`
- release candidate query takes about `5.13s`
- public release list query is already healthy at about `4ms`
- stale operational data exists:
  - `32` abandoned `indexer_stage_runs`
  - `6` stale `scrape_runs` still marked `running`

## Post-Implementation Status

The DB/runtime stabilization slice was implemented and validated on a reset dev DB:

- transient header payload was split out of `article_headers`
- `article_poster_map` was removed
- assembly now uses `assembled_at IS NULL`
- release formation now uses `release_stage_dirty_families`
- maintenance cleanup and backfill-until-date are working
- migrations were squashed into a new baseline

The remaining stabilization gap is now catalog-quality rather than schema/runtime safety.

Current follow-up measurements on the dev DB:

- `548` releases at `completion_pct >= 100`
- only `49` archive-like `release_files`
- `0` direct media-like `release_files`
- `37,809` `release_files` ending in `.bin`

Interpretation:

- inspect archive/media candidate discovery is under-covering obfuscated posts
- release formation is still falling back to opaque `.bin` filenames too often
- many releases remain source-titled because inspect and enrich never get strong local evidence

Latest implementation status:

- added `inspect_discovery` as a first-class inspect stage and CLI subcommand
- discovery now scans one opaque release at a time instead of trusting a single fallback-sorted binary
- `binaries` now persist recovered kind/extension/source/confidence
- recovery can canonicalize opaque `release_files` and `binary_parts`
- archive recovery can rename coherent opaque sibling families into split-archive shapes for downstream archive/media inspection
- assembly now has a yEnc-header recovery path for low-confidence opaque subjects so multipart obfuscated posts can be regrouped by authoritative yEnc `name/part/total`
- confirmed failure mode on dev DB:
  - `release_id=3CdNsxCiPeAHoEMGeTcqdXPT71L` was not `99` real files
  - it was `99` captured parts of one yEnc file with shared `name=kuqn1sj0tdehymt5l4ba7u` and `total=807`
  - current stabilization work now treats that class of post as an assembly regression, not just an inspect-discovery problem

## Active Workstreams

### 1. Header storage and retention

- split transient header payload into `article_header_ingest_payloads`
- keep permanent raw-header identity on `article_headers`
- add `assembled_at`
- retain payload rows only while pending or within `7 days` after assembly
- remove `article_poster_map`

### 2. Hot query cleanup

- replace assembly pending detection with `assembled_at IS NULL`
- add partial pending-header index
- replace normal release full-table regrouping with `release_stage_dirty_families`
- keep reform mode separate from normal scheduled release work

### 3. Safe schema/index cleanup

- remove dead tables:
  - `regex_rules`
  - `regex_hits`
  - `part_repair_queue`
- drop dead indexes:
  - `idx_binary_parts_message_id`
  - `idx_binaries_source_release_key`
- keep `idx_article_headers_newsgroup_id_date_utc` for now until backfill-until-date validation is signed off

### 4. Maintenance operations

- add recurring `indexer_maintenance` stage
- reuse `RepairIndexerStageRuntime()`
- abandon stale `scrape_runs`
- purge old stage runs and scrape runs
- purge old assembled header payload rows

### 5. Backfill-until-date

- support per-group cutoff dates using provider XOVER dates during backfill
- do not depend on local DB date-window scans
- persist cutoff state so restart does not continue past the configured date

### 6. Obfuscated post recovery and inspect eligibility

- improve candidate discovery for releases whose files currently degrade to `.bin`
- make yEnc metadata first-class in assembly for obfuscated multipart posts
- treat square-bracket counters as authoritative release file-count markers when present:
  - `[current/total]` is file index / file count
  - `yEnc (current/total)` is article index / article count for one file
- prevent multipart article slices from becoming fake standalone binaries
- prevent weak one-binary opaque clusters from becoming releases unless there is explicit single-file evidence like `[1/1]` or strong readable standalone media identity
- add a pre-inspection discovery pass for opaque release files and binaries
- recover likely archive/media/container kind using byte-level or article-level evidence before archive/media stages filter them out
- improve file-name recovery for obfuscated posts so inspect has better file candidates and release formation has better file names
- keep this work inside the stabilization phase because it directly affects:
  - release identity quality
  - inspect coverage
  - title quality
  - downstream enrich effectiveness

## Acceptance Criteria

- `article_headers` no longer stores `subject`, `poster`, `xref`, or `raw_overview_json`
- `article_poster_map` no longer exists
- dead tables and dead indexes above no longer exist
- assembly fetch for `5,000` pending headers completes in under `1s`
- normal release candidate fetch completes in under `1s`
- stale `scrape_runs` are auto-abandoned
- payload purge removes only transient header payload, not permanent lineage
- per-group backfill cutoff survives restart and stops at the configured boundary
- `.bin` fallback is no longer the dominant release-file shape for complete releases
- multipart yEnc files no longer explode into many one-article fake binaries when the visible subject is opaque
- inspect archive/media candidate discovery is no longer blocked primarily by filename opacity
- release titles for obfuscated posts can be improved by inspect/enrich because stronger local evidence is discoverable
- test-seeded release rows are not mistaken for live catalog output during normal dev validation

## Execution Order

1. apply schema/store changes for header payload split, dirty-family queue, and cleanup removals
2. update scrape/assemble/release/runtime code paths to use the new schema and maintenance stage
3. validate with focused tests and `EXPLAIN (ANALYZE, BUFFERS)` on assembly and release hot paths
4. update docs and archive completed Phase 1/Phase 2 docs
5. after the stabilization changes are signed off, squash migrations into a new post-cleanup baseline and reset the dev DB
6. continue with obfuscated-post recovery, file-name recovery, and inspect-eligibility hardening before declaring the stabilization phase complete

## Validation Checklist

- measure table sizes for:
  - `article_headers`
  - `article_header_ingest_payloads`
  - `binaries`
  - `binary_parts`
  - `release_file_articles`
- measure index sizes and scans for:
  - `article_headers`
  - `binary_parts`
  - `binaries`
- count stale operational rows:
  - abandoned `indexer_stage_runs`
  - stale `running` `scrape_runs`
- run `EXPLAIN (ANALYZE, BUFFERS)` for:
  - assembly pending-header fetch
  - normal release candidate fetch
- run a canary backfill cutoff at `2025-01-01` and verify restart safety
- measure complete-release inspect eligibility:
  - count complete releases
  - count archive-like release files
  - count direct media-like release files
  - count `.bin` release files
- confirm that byte-level discovery or pre-inspection recovery materially reduces `.bin`-only complete releases
- confirm inspect archive/media can process a representative sample of formerly opaque complete releases

## Migration And Reset Strategy

- implement and validate against the current schema first
- do not squash migrations until the new behavior is validated
- once stabilized, replace the active migration chain with a new baseline and reset/rebuild the dev DB

## Remaining Execution Focus

The remaining stabilization focus is no longer primarily:

- schema size
- stale runtime cleanup
- release query cost

It is now primarily:

- obfuscated-post discovery
- file-name recovery
- inspect eligibility for complete releases
- title quality improvement through stronger local evidence
