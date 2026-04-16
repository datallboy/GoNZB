# Indexer Stabilization Worklist

Snapshot date: 2026-04-16

This document turns the current release/database assessment into a commit-sized worklist so we can reduce tech debt before API and web UI work.

The goal is not to add new capabilities. The goal is to make the current indexer model trustworthy, smaller, easier to reason about, and easier to expose safely.

## Current Snapshot

Live DB snapshot from 2026-04-16:

- database size: `32 GB`
- `article_headers`: `26 GB`
- `binaries`: `3693 MB`
- `binary_parts`: `1509 MB`
- `release_file_articles`: `466 MB`
- `releases`: `984`
- complete releases: `422`

Important live correctness notes:

- only `15 / 178,789` `binaries` rows currently have `posted_at`
- only `7 / 14,512` `release_files` rows currently have `posted_at`
- `binaries.release_key = binaries.release_family_key` for all current rows
- `3` release rows still have blank `source_release_key` and blank `release_family_key`
- only `49` releases currently have `title_source = 'archive_entry'`
- only `52` releases currently have `deobfuscated_title`

## Stabilization Goals

1. make release formation deterministic and explainable
2. reduce duplicated identity and duplicated persistence logic
3. shrink hot tables and indexes where the data is redundant or debug-only
4. move non-essential enrichment out of the critical path
5. leave a schema and codebase that are safe to build API/UI on top of

## Commit-Sized Work Plan

### 1. Fix Binary Timing Persistence

Problem:

- release clustering uses posting time as a signal
- the live DB shows almost all `binaries.posted_at` values are missing
- `release_files.posted_at` is also almost always missing

Work:

- update `RefreshBinaryStats` to refresh `posted_at` from related `article_headers`
- backfill existing `binaries.posted_at`
- rebuild `release_files.posted_at`
- add a regression test for preserved/backfilled binary posting time

Why this comes first:

- release heuristics should not be tuned while one of the main clustering signals is mostly absent

### 2. Eliminate Blank Family Identity

Problem:

- some `releases` still persist with blank `source_release_key` and `release_family_key`

Work:

- identify the code path that allows blank family identity through
- enforce non-blank family identity before release persistence
- repair existing blank-family rows
- add test coverage for "never persist blank family key"

Why this matters:

- blank family identity is a correctness bug, not just ugly data

### 3. Finish The Identity Cutover

Problem:

- old and new identity fields are both still present and actively overlap
- `release_key` is still carrying duplicate meaning

Work:

- make the intended roles explicit in code and docs:
  - `source_release_key`: matcher/debug/repair trace
  - `release_family_key`: family-level grouping key
  - `group_name` / `release_id`: final release identity
- remove hot-path dependence on legacy `release_key`
- drop duplicate indexes once the cutover is complete
- plan the eventual removal or repurposing of legacy `release_key`

Immediate duplicate candidates:

- `binaries.release_key` duplicates `binaries.release_family_key` in the live DB
- `idx_binaries_release_key` is likely removable after cutover

### 4. Move Release Rules Out Of `repository.go`

Problem:

- release behavior is currently spread across store queries and domain helpers
- `repository.go` is carrying family-coercion behavior, inspection title adoption logic, and availability rollups

Work:

- move title-candidate selection rules into shared release/title helpers
- keep store responsibilities focused on reading and writing records
- reduce "silent business logic" inside persistence functions

Outcome:

- easier testing
- easier future refactors
- lower chance of API/UI accidentally depending on persistence quirks

### 5. Reduce Derived Data Weight

Problem:

- derived/indexer tables are larger than expected for the amount of useful catalog state

Work:

- review whether `release_file_articles` must remain fully materialized
- review whether some release-file lineage can be regenerated from `binary_parts`
- review whether `grouping_evidence_json` belongs inline on every `binaries` row

Potential direction:

- keep binary/release hot rows small
- move bulky debug evidence to a side table keyed by `binary_id`
- keep only summary confidence/state on `binaries`

### 6. Reduce Raw Header Index Overhead

Problem:

- `article_headers` owns most of the DB size
- one index pair looks redundant:
  - unique `(newsgroup_id, article_number)`
  - separate `(newsgroup_id, article_number DESC)`

Work:

- confirm query plans that use the descending index
- if safe, drop the redundant descending index
- reindex / vacuum after cleanup

Why this matters:

- this is a likely multi-GB win without touching raw scrape history

### 7. Trim Unused Release Columns Before API/UI

Problem:

- some release columns are effectively empty or unused in the live DB
- exposing them in API/UI too early hardens weak schema choices

Current low-value or effectively-unused fields:

- `matched_media_title`
- `tmdb_id`
- `tvdb_id`
- `external_media_type`
- `original_media_title`
- `external_year`
- `season_number`
- `episode_number`
- `season_episode_source`

Work:

- classify each as one of:
  - keep for near-term product value
  - defer from API/UI but keep in schema
  - remove/migrate later

### 8. Clean Up Stage And Logging Debt

Problem:

- stale stage-run rows and oversized logs make it harder to trust runtime state

Work:

- clean stale running/abandoned stage rows during repair/reset flows
- add log rotation or retention limits
- keep per-item debug spam out of default logs

## Relational Opportunities

These are the main places where duplicated or wide data should likely move behind IDs or side tables.

### Opportunity 1. Binary Grouping Evidence Should Be Side Data

Current state:

- `binaries.grouping_evidence_json` averages about `601` bytes per row

Recommendation:

- move verbose grouping evidence to a `binary_grouping_evidence` table keyed by `binary_id`
- keep only compact identity/confidence fields on `binaries`

Why:

- this is debug/inspection data, not hot relational identity
- it inflates the hottest derived table

### Opportunity 2. Legacy And Canonical Release Identity Should Not Coexist Forever

Current state:

- `source_release_key`, `release_family_key`, and legacy `release_key` all exist
- at least one pair is currently duplicative in live data

Recommendation:

- keep canonical identity normalized by role rather than storing duplicate text columns indefinitely
- if `release_key` remains, define one meaning only

Why:

- duplicate identity strings cost table space, index space, and cognitive load

### Opportunity 3. Release Enrichment Can Be Split From Core Release Identity

Current state:

- `releases` mixes core identity, formation stats, inspection rollups, title state, password state, and external-media enrichment

Recommendation:

- consider a future split between:
  - core `releases`
  - `release_titles` or title provenance history
  - `release_media_metadata`
  - `release_external_ids`

Why:

- the core release row should stay small and stable
- optional enrichment changes more often and can be recomputed

### Opportunity 4. Release-File To Article Lineage May Be Too Eagerly Materialized

Current state:

- `release_file_articles` is materially sized

Recommendation:

- confirm which use cases truly require release-scoped article lineage
- if binary-scoped lineage is sufficient, consider deriving some release-file lineage on demand

Why:

- we may be storing the same relationship twice:
  - `binary_parts -> article_headers`
  - `release_file_articles -> article_headers`

### Opportunity 5. Poster And Newsgroup Dimensions Are Good Patterns To Reuse

Current state:

- poster identity is already normalized through `posters`
- newsgroups are already normalized

Recommendation:

- continue using dimension tables when values are repeated at scale
- avoid embedding more repeated long strings directly on hot rows unless they are part of a stable key

## What I Would Not Do Yet

- do not wipe `article_headers`
- do not add more release/enrichment columns just because they might be useful later
- do not expose unstable release fields in API/UI contracts yet
- do not do broad architectural rewrites before the identity/timing/storage issues are closed

## Suggested Execution Order

1. binary timing persistence and backfill
2. blank family identity fix and repair
3. identity cutover and duplicate-index cleanup
4. store-vs-domain release logic cleanup
5. derived-table slimming and debug-evidence normalization
6. raw-header index cleanup
7. API/UI only after the above are stable
