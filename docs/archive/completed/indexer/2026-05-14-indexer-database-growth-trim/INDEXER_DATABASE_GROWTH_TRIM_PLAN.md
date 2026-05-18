# Indexer Database Growth Trim Plan

Snapshot date: 2026-05-14

This is the active plan for reducing indexer database growth after the grouping-model sprint proved the release/readiness improvements were landing but overnight retention growth pushed the PostgreSQL database to about `92 GB`.

This plan is the execution tracker for the completed sprint. Use `docs/archive/completed/indexer/2026-05-14-indexer-database-growth-trim/INDEXER_DATABASE_SCHEMA_AUDIT.md` as the completed current-state audit and column-ownership reference for this sprint, and use `docs/INDEXER_CURRENT_SCHEMA_AND_SYSTEM_INTERACTIONS.md` as the living whole-system schema map after sprint close.

This sprint runs on one branch. Treat the commit order below as the working execution order for Codex sessions on that branch.

## Current Finding

The main storage problem is retention and duplication in ingest and identity-audit tables, not releases.

Largest live tables from the overnight run:

- `article_headers`: `33 GB`
- `article_header_ingest_payloads`: `23 GB`
- `binary_grouping_evidence`: `14 GB`
- `binaries`: `11 GB`
- `binary_parts`: about `5 GB`
- `release_family_readiness_summaries`: about `4.6 GB`

That means roughly `70 GB` is concentrated in:

- article/header retention
- grouping evidence retention
- repeated readiness surfaces

## Immediate Goals

1. stop unnecessary per-header retention from growing without bound
2. identify which tables are canonical versus audit/debug-only
3. add bounded retention and cleanup policies for pre-alpha scale testing
4. preserve enough evidence to debug grouping issues without keeping every repeated row forever

## Up-Front Answers

These answers should stay true unless a later audit or implementation commit proves otherwise.

- schema truth comes from the live Docker Postgres database, not archived migrations
- `article_headers`, `binaries`, and `binary_parts` are the core canonical identity surfaces
- `article_header_ingest_payloads` and `release_family_readiness_summaries` are active derived workflow surfaces that should be retained only as long as they still drive assemble, recovery, or release
- `binary_grouping_evidence` and verbose JSON payloads are the first storage surfaces to compact, sparsify, or prune
- safe cleanup means stopping unnecessary writes and shortening derived retention before removing canonical columns

## Execution Tracks

### Track 0. Documentation baseline

Goals:

- keep this doc as the active sprint entrypoint
- maintain a separate active audit tracker for schema truth and column usage
- keep `docs/active/INDEXER_FOUNDATION_DOCS.md` aligned with both active docs

Deliverables:

- active execution plan
- active schema audit doc
- updated foundation doc pointers

## Priority Work

### Phase 1. Reconfirm canonical ownership and system interaction

Use `docs/INDEXER_CURRENT_SCHEMA_AND_SYSTEM_INTERACTIONS.md` as the living system map and `docs/archive/completed/indexer/INDEXER_SCHEMA_AND_SERVICE_DATAFLOW.md` as historical reference support when needed. Answer:

- what must stay in `article_headers`
- what can be compacted or aged out from `article_header_ingest_payloads`
- what portions of `binary_grouping_evidence` need full retention versus rolling retention
- whether `release_family_readiness_summaries` needs pruning for stale families

Deliverables:

- current system-layer map for ingest, identity, recovery, readiness, and inspect
- completed hot-table column inventory in the active schema audit doc
- per-column writer and reader map
- keep, compact, prune, migrate, or drop-candidate disposition for each hot-table column

### Phase 2. Trim ingest payload retention

Target:

- reduce or age out `article_header_ingest_payloads` aggressively once typed fields are persisted and assemble/recovery no longer need the bulky payload row

Candidates:

- null or compact old `raw_overview_json`
- bound retention by age, assembled state, or recovery usefulness
- move retry/backoff-only fields out of the bulky payload row if needed

Implementation notes:

- do not remove payload fields until the audit confirms all remaining readers and writers
- split low-risk retention cleanup from any schema move or field removal

Recommended default policy:

- stop treating `article_header_ingest_payloads` as a long-lived shadow copy of scrape input
- keep structured payload fields only as long as assemble or yEnc recovery still needs them
- reduce assembled payload retention from the current `7 days` to a staged policy:
  - purge assembled rows with `subject_file_name <> ''` and no active yEnc retry state after `1 hour`
  - purge assembled rows with missing structured file identity or non-zero recovery state after `24 hours`
- stop writing `raw_overview_json` for new rows unless a current stage proves it still needs original raw JSON

Implementation status:

- completed in cleanup wave 1: new ingest writes now persist empty JSON for `raw_overview_json`
- completed in cleanup wave 1: yEnc recovery no longer reads `raw_overview_json` and rebuilds its matcher input from structured columns and article facts
- completed in cleanup wave 1: assembled payload retention now uses a two-tier maintenance purge:
  - `1 hour` for rows with structured filename identity and no active recovery state
  - `24 hours` for rows that still lack structured identity or still carry recovery backoff state
- completed in cleanup wave 1: payload purge execution now walks bounded `article_header_id` windows so maintenance can run on the live dev database without immediate temp-file spill failure
- completed in cleanup wave 2: `raw_overview_json` has been removed from the active schema, baseline migration, and payload upsert path
- live migration validation on `2026-05-14`: opening the store against the Docker database advanced `pgindex` schema version to `21` and confirmed `article_header_ingest_payloads.raw_overview_json` is no longer present

Live validation:

- on `2026-05-14`, a successful local maintenance run completed with `purged_header_payloads=7150219` and `purged_readiness_summaries=0`
- exact live payload row count dropped from `79,109,360` to `70,909,141`
- physical table size remained `23 GB` immediately after the run, which is expected until PostgreSQL reuses or vacuums dead tuples
- total observed payload-row reduction exceeded the final-pass counter because earlier interrupted maintenance experiments had already removed additional rows while the purge execution shape was being tuned
- a follow-up `2026-05-14` maintenance run reclaimed `389,908` additional payload rows, bringing the exact live row count to `70,519,233`
- follow-up `VACUUM (ANALYZE)` completed for `article_header_ingest_payloads`; physical size still read `23 GB`, but planner stats are now current again

Why this is the default:

- the database was able to reach roughly `100 GB` within one day, so a `7 day` payload horizon is far too permissive for this stage of development
- live data already shows `raw_overview_json` is empty on stored rows
- normal assemble hydration now rebuilds `RawOverview` from structured columns instead of reading stored raw JSON

### Phase 3. Trim grouping evidence retention

Target:

- keep useful identity audit trails without letting `binary_grouping_evidence` grow into a second giant history store

Candidates:

- retain only the most recent or most meaningful evidence rows per binary
- keep promotion/change-point evidence and age out redundant steady-state rows
- add a maintenance path to compact superseded evidence

Implementation notes:

- audit whether `binaries.grouping_evidence_json` and `binary_grouping_evidence` currently duplicate each other
- keep enough promotion history to debug grouping regressions after cleanup

Recommended default policy:

- keep `binaries.grouping_evidence_json` only as a compact inline summary surface
- turn `binary_grouping_evidence` into a sparse audit table instead of a universal one-row-per-binary blob
- retain side-table rows indefinitely only when at least one of these is true:
  - matcher confidence is below high-confidence threshold
  - fallback grouping was used
  - recovery or inspect changed binary identity, family key, base stem, or archive file count
  - the binary remains in a weak or overgrouped readiness state
- for high-confidence stable binaries, either do not persist a side-table row at all or retain a compact row for at most `24 hours`
- compact side-table payload shape to the minimum needed for debugging:
  - keep `summary`
  - keep only the specific evidence modules that explain a weak or changed decision
  - drop full verbose module payloads for stable high-confidence rows

Implementation status:

- completed in cleanup wave 1: assemble now persists a compact inline `grouping_evidence_json.summary` on `binaries`
- completed in cleanup wave 1: detailed `binary_grouping_evidence` rows are now retained only for weak, provisional, low-confidence, or fallback-driven matches
- completed in cleanup wave 1: inspect and admin detail reads now fall back to inline grouping evidence when no side-table row exists
- completed in cleanup wave 1: maintenance now backfills inline `summary` from legacy side-table rows and purges older stable high-confidence `binary_grouping_evidence` rows

Live validation:

- on `2026-05-14`, a follow-up local maintenance run completed with `purged_grouping_evidence=523`
- the same run also reclaimed `389,908` additional payload rows and `9,865` readiness summary rows that had become eligible since the prior pass
- exact live `binary_grouping_evidence` row count dropped to `10,341,903`
- immediate physical side-table size still read `16 GB`, which again points to dead-tuple reuse/vacuum as the next operational concern after retention logic
- follow-up `VACUUM (ANALYZE)` completed for `binary_grouping_evidence`; physical size still read `16 GB`, but planner stats are now refreshed for subsequent measurement

Why this is the default:

- `binary_grouping_evidence` currently averages about `1365` bytes per row across roughly one row per binary
- inline grouping JSON is much sparser and already acts more like a compact summary layer
- admin/debug detail views read the side-table payload today, so trimming should focus first on sparsifying and compacting that surface rather than removing core identity fields

Operational note:

- one audit query already failed with `No space left on device` while Postgres tried to spill temp files, which is another sign that oversized retained JSON surfaces are an immediate operational risk, not just a storage-usage concern

### Phase 4. Prune stale readiness and weak junk

Target:

- stop stale weak/test families from inflating summaries forever

Candidates:

- age out stale `weak_single_binary` and `stale_cleanup_only` rows whose source binaries are gone or permanently filtered
- explicitly quarantine known test/noise contextual families
- add maintenance metrics for pruned family counts

Implementation notes:

- validate which readiness buckets are productively consumed versus stale queue residue
- keep actionable family visibility and release throughput checks intact

Recommended default policy:

- keep `release_family_readiness_summaries` as the active release queue/read model
- do not prune rows that are still pending work by `updated_at > COALESCE(processed_at, updated_at)`
- do not prune weak-family rows that are still needed by yEnc recovery targeting
- add bounded retention for non-pending weak-family residue:
  - `stale_cleanup_only`: purge after `24 hours` when no matching binaries remain
  - `fragment_only`: purge after `24 hours` when not pending and no stale release cleanup remains to perform
  - `weak_single_binary`: purge after `24 hours` when not pending and the family no longer has active recovery-eligible binaries
  - `weak_obfuscated_set` and `overgrouped_contextual`: purge after `24 hours` when not pending and no recovery-eligible binaries remain
  - `prefer_base_stem`: keep short-lived and purge after `6 hours` when not pending
- if a family becomes active again, allow summary refresh to recreate the row rather than preserving it indefinitely

Implementation status:

- completed in cleanup wave 1: `RunIndexerMaintenance` now prunes non-pending readiness residue for:
  - `prefer_base_stem` after `6 hours`
  - `fragment_only` after `24 hours`
  - `stale_cleanup_only` after `24 hours`
  - `weak_single_binary`, `weak_obfuscated_set`, and `overgrouped_contextual` after `24 hours` when no recovery-eligible binaries remain
- completed in cleanup wave 1: maintenance logging and metrics now expose `purged_readiness_summaries`
- live validation on `2026-05-14`: the successful maintenance run reported `purged_readiness_summaries=0`, which means current live growth pressure is still dominated by payload retention and grouping evidence rather than already-eligible summary residue
- live validation on a follow-up `2026-05-14` maintenance run: `purged_readiness_summaries=9865`, confirming the bounded cleanup is working once rows age into eligibility
- follow-up `VACUUM (ANALYZE)` completed for `release_family_readiness_summaries`; physical size still read `5236 MB`, but planner stats are now refreshed

Why this is the default:

- the table has roughly `9.94M` rows, but only about `684k` are currently pending release work
- the dominant cost driver is retained `weak_single_binary`, not actionable queue rows
- `processed_at` already gives the queue a natural lifecycle marker we can use for pruning

Safety rule:

- any readiness retention job must preserve rows still referenced by current release selection or yEnc recovery candidate discovery

### Phase 5. Add operator-visible storage reporting

Target:

- make storage problems visible before a one-night run hits `100 GB`

Minimum reporting:

- top table sizes
- row counts
- retained payload/evidence percentages
- cleanup run history

Implementation status:

- completed in cleanup wave 1: admin dashboard cached stats now expose exact rows and on-disk bytes for:
  - `article_header_ingest_payloads`
  - `binary_grouping_evidence`
  - `release_family_readiness_summaries`
- completed in cleanup wave 1: admin dashboard cached stats now expose planner-visible dead-tuple counts for those same three tables
- completed in cleanup wave 1: UI stat cards now format byte-based storage stats in human-readable units

### Phase 6. Reclaim on-disk table space

Target:

- return reclaimed bytes to the Docker volume and host filesystem after retention logic has already reduced row growth

Operator sequence:

1. stop the app and background indexer stages
2. run `indexer maintenance` to apply retention cleanup first
3. run `VACUUM (ANALYZE)` on the hot trim tables to refresh planner stats
4. if host filesystem usage still does not recover, run table-by-table `VACUUM (FULL, ANALYZE)` in this order:
   - `release_family_readiness_summaries`
   - `binary_grouping_evidence`
   - `article_header_ingest_payloads`
5. recheck exact table sizes and host free space after each table before continuing

Why this order:

- it starts with the smallest rewrite
- it validates that the Docker-backed Postgres volume is actually returning bytes before risking the largest rewrite
- it leaves the largest retained payload table for last, after the reclaim path is already proven on the current machine

Implementation status:

- completed in cleanup wave 2: operator reclaim runbook documented in `docs/INDEXER_POSTGRES_RUNTIME_TUNING.md`
- completed in cleanup wave 2: `indexer maintenance reclaim-storage` now runs the allowlisted reclaim sequence with a safe default:
  - `VACUUM (ANALYZE)` by default
  - `VACUUM (FULL, ANALYZE)` with `--full`
  - `--check` reports current bytes without running `VACUUM`
- live validation on `2026-05-14`: `go run ./cmd/gonzb --config config.yaml indexer maintenance reclaim-storage readiness` completed successfully and reported:
  - `before_bytes=5490278400`
  - `after_bytes=5490286592`
  - `delta_bytes=8192`
- live validation on `2026-05-14`: `go run ./cmd/gonzb --config config.yaml indexer maintenance reclaim-storage --check` completed successfully and reported:
  - `release_family_readiness_summaries=5490286592`
  - `binary_grouping_evidence=17332527104`
  - `article_header_ingest_payloads=24795742208`
- current blocker on `2026-05-14`: host `/` free space was only `5.3G` while the smallest reclaim target still measured about `5.49 GB`, so a live `VACUUM (FULL, ANALYZE)` run is currently unsafe on this dev machine until more root-volume space is freed

## Handoff Action Items

Use these as concise Codex-sized work chunks. Each chunk should update either this plan, the schema audit doc, or both.

Codex ownership for this sprint:

- perform the live Docker database review
- audit schema columns against code usage and active migrations
- document current-state findings and cleanup decisions
- implement the approved code, migration, and documentation changes on this same branch
- validate before and after behavior and storage outcomes

## Suggested Commit Order

Use this order for commits on the sprint branch unless a later audit finding forces a dependency change.

### Commit 1. `docs-baseline-sync`

Purpose:

- lock the active docs structure
- keep this file as the sprint entrypoint
- keep the schema audit doc as the source of truth for current-state findings

Expected commit contents:

- active doc pointer fixes
- sprint workflow notes
- no schema or code behavior changes

### Commit 2. `schema-audit-live-db`

Purpose:

- capture live table sizes, live columns, row counts, and schema notes from Docker Postgres
- reconcile live shape against `internal/store/pgindex/migrations`

Expected commit contents:

- audit doc baseline sections filled in
- current schema/system interaction doc added or updated as needed
- documented schema mismatches or under-documented live fields
- no code behavior changes

### Commit 3. `code-usage-map-ingest-and-assembly`

Purpose:

- map `article_headers` and `article_header_ingest_payloads` readers and writers
- classify ingest fields as canonical, derived, debug-only, or cleanup candidates

Expected commit contents:

- audit doc updates for ingest ownership
- links to store and service code paths
- no schema changes yet

### Commit 4. `code-usage-map-binary-identity`

Purpose:

- map `binaries`, `binary_parts`, and `binary_grouping_evidence`
- identify duplicated grouping evidence and identity helper fields

Expected commit contents:

- audit doc updates for binary identity ownership
- keep versus compact versus migrate recommendations
- no schema changes yet

### Commit 5. `code-usage-map-readiness-and-ui`

Purpose:

- map `release_family_readiness_summaries` usage across release selection, admin, API, and debug surfaces
- identify stale weak-family dependencies

Expected commit contents:

- audit doc updates for readiness ownership
- bucket-usage findings
- no schema changes yet

### Commit 6. `trim-policy-payloads-and-evidence`

Purpose:

- turn audit findings into explicit retention and compaction rules for ingest payloads and grouping evidence

Expected commit contents:

- plan updates with cleanup predicates
- validation criteria for future implementation commits
- docs-only unless a trivial safe cleanup is proven

### Commit 7. `trim-policy-readiness-and-junk-families`

Purpose:

- define prune rules for stale readiness surfaces and weak junk families
- define required operator-visible metrics

Expected commit contents:

- plan updates for readiness cleanup policy
- non-regression checkpoints
- docs-only unless a trivial safe cleanup is proven

### Commit 8. `cleanup-implementation-wave-1`

Purpose:

- apply low-risk cleanup supported by the completed audit
- prefer bounded retention, doc/query cleanup, and dead-code removal before risky schema surgery

Expected commit contents:

- code and migration changes that are low risk
- first-pass write suppression for obviously unnecessary verbose payloads where the system map and audit both agree
- validation query updates if needed
- before and after measurements added to the active docs

### Commit 9. `cleanup-implementation-wave-2`

Purpose:

- apply higher-risk changes that require schema rewiring, migration updates, or UI/debug surface cleanup

Expected commit contents:

- remaining code and migration changes
- active doc updates describing what moved, what was removed, and why
- validation evidence

### Commit 10. `validation-and-signoff`

Purpose:

- close the sprint with measured results and remaining follow-up items

Expected commit contents:

- final storage and behavior measurements
- resolved versus deferred cleanup items
- sign-off notes in the active docs

### 1. `docs-baseline-sync`

Scope:

- ensure the active docs point at the current sprint correctly
- preserve this doc as the active entrypoint
- add and wire the schema audit tracker

Completion check:

- foundation doc references both active docs correctly

### 2. `schema-audit-live-db`

Scope:

- capture live table sizes, row counts, and column lists from Docker Postgres
- reconcile the hot tables against `internal/store/pgindex/migrations`

Completion check:

- active audit doc has live schema sections for all hot tables

### 3. `code-usage-map-ingest-and-assembly`

Scope:

- document where `article_headers` and `article_header_ingest_payloads` are written and read
- identify hot-path versus debug-only ingest fields

Completion check:

- ingest tables have reader/writer ownership and disposition notes

### 4. `code-usage-map-binary-identity`

Scope:

- document `binaries`, `binary_parts`, and `binary_grouping_evidence`
- identify redundant inline versus side-table grouping evidence

Completion check:

- binary identity tables have clear canonical versus audit-only decisions

### 5. `code-usage-map-readiness-and-ui`

Scope:

- document how `release_family_readiness_summaries` is used by release selection, admin, and debug surfaces
- identify stale or weak-family-only surfaces that keep large state alive

Completion check:

- readiness summary columns and bucket usage are mapped to concrete code surfaces

### 6. `trim-policy-payloads-and-evidence`

Scope:

- define retention or compaction policy for payload rows and grouping evidence
- separate low-risk cleanup from changes that require schema movement

Completion check:

- this plan includes concrete cleanup predicates and validation notes

### 7. `trim-policy-readiness-and-junk-families`

Scope:

- define prune policy for stale readiness buckets and weak junk families
- define operator-visible metrics for cleanup outcomes

Completion check:

- readiness cleanup rules are documented with non-regression checks

### 8. `cleanup-implementation-wave-1`

Scope:

- execute low-risk removals or retention changes supported by the audit
- update docs and validation queries as needed

Completion check:

- before/after storage and behavior metrics are recorded

### 9. `cleanup-implementation-wave-2`

Scope:

- execute schema or code changes that require migrations, rewiring, or UI/debug surface cleanup

Completion check:

- implementation notes and validation results are appended to the active docs

## Codex Session Rules

For each commit-sized session:

1. read this plan and the schema audit doc first
2. update the docs with findings before removing columns or changing retention behavior
3. use the Docker Postgres database as source of truth for current state
4. reconcile against `internal/store/pgindex/migrations`, not `migrations_archive`
5. keep changes focused on the current commit purpose
6. record validation evidence or blockers in the active docs before moving to the next commit

## Validation

Track before and after:

- total database size
- top 10 table sizes
- `article_headers` and `article_header_ingest_payloads` growth per hour
- `binary_grouping_evidence` growth per hour
- release count and actionable family counts to ensure trimming does not regress the grouping improvements

Per implementation wave, also track:

- readiness bucket distribution changes
- any changes in admin/debug surface dependencies
- whether release visibility, actionable families, and grouping quality remain stable

## Sign-Off

Signed off on `2026-05-14`.

Resolved in this sprint:

- completed the live schema and code-usage audit for the hot growth tables
- reduced ongoing ingest-payload retention from `7 days` to staged `1 hour` and `24 hour` windows
- stopped persisting verbose raw overview payload data and removed `article_header_ingest_payloads.raw_overview_json` from the active schema
- made `binary_grouping_evidence` sparse by default and added legacy stable-row cleanup
- added bounded cleanup for non-pending readiness residue
- added operator-visible storage stats and allowlisted reclaim tooling
- validated the code paths with focused Go test coverage and live Docker-database checks

Deferred outside the sprint code changes:

- physical filesystem reclaim through `VACUUM (FULL, ANALYZE)` is still blocked by root-volume free space
- longer-run post-trim growth measurement should be rerun after the next sustained ingest session on `dv`

Closeout note:

- this sprint no longer has open code or schema tasks
- the remaining work is operational reclaim plus follow-up measurement after merge
- once merged, these active docs can move to completed/archive status if you want to keep `docs/active/` limited to in-flight work

This is now the active indexer execution doc after the grouping-model re-evaluation sprint was signed off on 2026-05-14.
