# Indexer Stabilization Worklist

Snapshot date: 2026-04-16

This document turns the current release/database assessment into a commit-sized worklist so we can reduce tech debt before API and web UI work.

The goal is not to add new capabilities. The goal is to make the current indexer model trustworthy, smaller, easier to reason about, and easier to expose safely.

This is the primary execution-plan document for the current stabilization phase.

Related docs:

- target release-formation design:
  - `docs/active/INDEXER_RELEASE_FORMATION_SNAPSHOT_AND_PLAN.md`
- target schema and table boundaries:
  - `docs/active/INDEXER_SCHEMA_TARGET.md`
- short current-state terminology reference:
  - `docs/INDEXER_HOW_IT_WORKS.md`
- docs map and which files are active vs reference:
  - `docs/active/INDEXER_FOUNDATION_DOCS.md`

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

## Validation Status

Validation date: 2026-04-16

Implementation status:

- the commit-sized stabilization backlog in this document is complete in code
- migrations `019` through `023` are applied on the dev DB
- repo validation passed with:
  - `go test ./internal/store/pgindex`
  - `go test ./internal/indexing/release`
  - `go test ./internal/runtime/commands`

Operational validation run on dev DB:

- `./bin/gonzb --config config.yaml indexer assemble --once`
- `./bin/gonzb --config config.yaml indexer release --once`
- `./bin/gonzb --config config.yaml indexer release --once --reform`
  - started cleanly but was canceled after a bounded validation window while still processing candidates
- validation queries run directly against the dev DB after migrations and command execution

Validated live results:

- schema version: `23`
- blank `source_release_key` / `release_family_key` rows: `0`
- release-family fan-out rows: `0`
- `binary_grouping_evidence` side-table rows: `178,817`
- non-empty inline `binaries.grouping_evidence_json` rows: `0`
- redundant descending `article_headers` index present: `0`
- stale stage runtime rows: `0`
- orphaned running stage rows: `0`
- complete releases: `433 / 999`
- `title_source = 'archive_entry'`: `57`
- non-empty `deobfuscated_title`: `69`

Follow-up validation date: 2026-04-17

Timing repair completed:

- fixed NNTP overview date parsing for two-digit-year rows such as `Thu, 09 Apr 26 18:13:57 UTC`
- repaired `4,250,363` binary-linked `article_headers.date_utc` rows in the dev DB from saved raw XOVER lines
- backfilled persisted timing again into `binaries`, `release_files`, and `releases`

Post-repair live results:

- `binaries.posted_at`: `178,817 / 178,817`
- `release_files.posted_at`: `14,526 / 14,526`
- binaries with any linked dated header through `binary_parts`: `178,817 / 178,817`
- `article_headers.date_utc`: `8,465,956 / 33,264,357`

Definition-of-stable sign-off:

- schema and storage goals: signed off on dev DB
- release identity goals: signed off on dev DB
- data-quality timing goals for assembled binaries and release files: signed off on dev DB
- stage/runtime repair goals: signed off on dev DB
- full stabilization sign-off: granted

Current conclusion:

- the stabilization code and schema work are complete and validated
- the active stabilization backlog in this document is complete
- the foundation is ready for the next API/UI phase without the previous posting-time blocker

## Stabilization Goals

1. make release formation deterministic and explainable
2. reduce duplicated identity and duplicated persistence logic
3. shrink hot tables and indexes where the data is redundant or debug-only
4. move non-essential enrichment out of the critical path
5. leave a schema and codebase that are safe to build API/UI on top of

## Definition Of Stable

The stabilization phase should not be considered done until these conditions are true:

### Release Formation

- blank `source_release_key` rows in `releases`: `0`
- blank `release_family_key` rows in `releases`: `0`
- `release_family_key` fan-out only exists for intentional repost / separate posting-wave cases
- multi-file family fragmentation is rare enough that canary families consistently assemble into one logical release row
- release clustering is using persisted timing data, not mostly-null timing fields

### Data Quality

- `binaries.posted_at` is populated for the overwhelming majority of assembled binaries
- `release_files.posted_at` is populated for the overwhelming majority of release files backed by binaries
- release title fields have a clear source-of-truth order and no longer depend on ad hoc store-side overrides

### Schema And Storage

- duplicate hot-path identity columns have a defined final role or have been removed
- redundant hot indexes are removed after query-plan validation
- bulky debug payloads are no longer inflating the hottest operational tables unnecessarily

### Operational Health

- stage state can be repaired/rebuilt with a documented runbook
- logs no longer grow explosively from expected release/inspect behavior
- we have a small set of validation queries that can quickly confirm release-health after changes

## Primary Execution Order

These are the chunks we should execute in order unless a later item is required to unblock an earlier one:

1. binary timing persistence and backfill
2. blank family identity fix and repair
3. identity cutover and duplicate-index cleanup
4. store-vs-domain release logic cleanup
5. derived-table slimming and debug-evidence normalization
6. raw-header index cleanup
7. release-column trim and enrichment boundary cleanup
8. stage/logging operational cleanup

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

Current classification for stabilization:

- keep for near-term product value:
  - none of the listed low-value fields currently clear the bar for core API/UI exposure
- defer from API/UI but keep in schema for now:
  - `matched_media_title`
  - `tmdb_id`
  - `tvdb_id`
  - `external_media_type`
  - `original_media_title`
  - `external_year`
  - `season_number`
  - `episode_number`
  - `season_episode_source`
- remove/migrate later:
  - after API/UI stabilization, move external-id and season/episode enrichment behind side storage instead of keeping them on the core `releases` row

Why this classification:

- these fields have some enrichment and repair value today
- they do not belong in the minimum stable release contract yet
- deferring them avoids hardening weak or sparsely-populated schema choices before the core model is stable

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

Current review outcome:

- keep `release_file_articles` materialized for now
- do not trim or derive it on demand yet

Reason:

- NZB generation currently reads exact release-file article refs from `release_file_articles`
- inspect/materialization flows also depend on release-file-scoped article ordering
- removing it before those consumers change would shift complexity into hot runtime paths instead of reducing it

Later direction:

- revisit only after NZB and inspect paths can reliably derive the same ordered refs from `binary_parts` without increasing runtime cost or coupling

### Opportunity 5. Poster And Newsgroup Dimensions Are Good Patterns To Reuse

Current state:

- poster identity is already normalized through `posters`
- newsgroups are already normalized

Recommendation:

- continue using dimension tables when values are repeated at scale
- avoid embedding more repeated long strings directly on hot rows unless they are part of a stable key

## Target Schema Summary

The intended stable schema shape is documented in:

- `docs/active/INDEXER_SCHEMA_TARGET.md`

The short version is:

- `article_headers`: raw provider scrape state
- `binaries`: compact assembled-file identity and completeness state
- side tables for bulky debug evidence and optional enrichment
- `releases`: compact release identity and release-level readiness
- optional release enrichment split out from the core release row where appropriate

The important schema rule is:

- keep hot identity rows small
- move debug, provenance history, and optional enrichment to side tables once they no longer belong in the critical path

## Removal And Deprecation Plan

These removals should happen only after the replacement path is active and validated.

### Identity

- stop treating legacy `release_key` as a separate operational identity once `release_family_key` is fully authoritative
- remove duplicate `binaries.release_key` hot-path use after cutover
- drop duplicate indexes that only support the retired identity path

### Debug Payloads

- if `grouping_evidence_json` moves to side storage, leave only compact summary/confidence state on `binaries`

### Release Columns

- classify underused release columns as:
  - core and keep
  - optional and move behind enrichment tables
  - remove from near-term API/UI surface

### Indexes

- validate usage before dropping any index
- prefer removing one redundant index at a time with measurement before/after

## Validation Queries And Health Checks

We should keep a short repeatable validation checklist after each release-formation or schema change.

### Release Identity Checks

- blank release-family rows
- blank source-release rows
- release-family values with more than one release row
- release-family values with only singleton multi-file fragments

### Release Quality Checks

- release count vs complete release count
- average `file_count`
- average `expected_file_count`
- releases with inspect-derived titles
- releases still stuck on `title_source = 'source'`

### Storage Checks

- top tables by `pg_total_relation_size`
- top indexes by size on `article_headers`, `binaries`, and `releases`
- check whether expected cleanup actually reclaimed space

### Runtime Checks

- stale or abandoned stage runs
- running stages with stale heartbeats
- whether assemble/release/inspect are producing eligible work

## Rebuild And Repair Procedure

Any stabilization chunk that changes identity, clustering, or release persistence should follow a standard repair path.

1. pause or stop active assemble/release/inspect workers
2. apply migrations
3. backfill any required derived fields
4. clear only the derived state that is invalidated by the change
5. rerun `assemble`, then `release`, then inspect stages
6. run the validation queries
7. only resume normal background processing after the health checks look correct

Important rule:

- keep `article_headers` unless we have explicitly decided raw scrape history itself is invalid

## Guideline Rules For Execution

- use this document as the active backlog, not `INDEXER_BACKEND_MILESTONES.md`
- keep commits narrow enough that each chunk can be validated independently
- update the "Current Snapshot" and any success metrics when a chunk materially changes the live state
- when a chunk changes the intended end state, update the target design docs in the same commit

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
