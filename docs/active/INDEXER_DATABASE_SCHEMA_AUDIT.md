# Indexer Database Schema Audit

Snapshot date: 2026-05-14

This is the active current-state audit doc for the database growth-trim sprint.

This doc is updated on the same sprint branch as the implementation work. Treat the commit order in `docs/active/INDEXER_DATABASE_GROWTH_TRIM_PLAN.md` as the execution order for audit and cleanup sessions.

Use `docs/active/INDEXER_CURRENT_SCHEMA_AND_SYSTEM_INTERACTIONS.md` when a cleanup decision depends on whole-system ownership rather than only column-by-column usage.

Use this doc to capture:

- the live Docker Postgres schema as the source of truth
- current column ownership and code usage for the hot indexer tables
- keep, compact, prune, migrate, and drop-candidate decisions
- handoff-ready findings that feed the active trim plan

## Audit Rules

1. Use the live Docker Postgres database as schema truth.
2. Use `internal/store/pgindex/migrations` as the active migration history to reconcile against.
3. Do not use `internal/store/pgindex/migrations_archive` for current-state decisions.
4. Record code readers and writers before proposing any column removal.
5. Treat canonical data, derived data, and audit/debug retention as separate classes.

## Current Live Findings

Largest live tables from the current Docker database:

- `article_headers`: `34 GB`
- `article_header_ingest_payloads`: `23 GB`
- `binary_grouping_evidence`: `15 GB`
- `binaries`: `11 GB`
- `binary_parts`: `5206 MB`
- `release_family_readiness_summaries`: `4701 MB`

Row-count notes:

- `article_header_ingest_payloads` exact count: `79,109,360`
- `binaries` exact count: `9,467,122`
- `article_headers`, `binary_grouping_evidence`, `binary_parts`, and `release_family_readiness_summaries` currently have unreliable planner row stats because they have not been analyzed recently
- `binaries` is the only hot table in this group with a current `last_autoanalyze` on `2026-05-14`

Current readiness bucket distribution:

- `weak_single_binary`: `9,079,151`
- `fragment_only`: `344,196`
- `stale_cleanup_only`: `84,457`
- `actionable`: `4,749`
- `weak_obfuscated_set`: `278`

Known current-state risks to validate in code:

- `article_header_ingest_payloads` is still read heavily by assemble and recovery paths
- `binaries.grouping_evidence_json` is still present in the live schema and active code paths
- `release_family_readiness_summaries` appears to be carrying a large amount of weak/stale queue state
- `article_header_ingest_payloads` still stores `raw_overview_json`, but the live database currently has `0` rows with a non-empty JSON payload
- `article_header_ingest_payloads` still stores `yenc_recovery_*` backoff fields, but the live database currently has `0` rows with `yenc_recovery_retry_after IS NOT NULL`
- `binaries.file_set_key` is populated on essentially all live rows, while only a small subset currently uses inline `grouping_evidence_json`, `expected_file_count`, or `expected_archive_file_count`

## Audit Workflow

### Phase 1. Baseline capture

Record for each hot table:

- total size
- approximate row count
- live columns and types
- relevant indexes and constraints when they affect retention or query shape

Hot tables in scope:

- `article_headers`
- `article_header_ingest_payloads`
- `binaries`
- `binary_parts`
- `binary_grouping_evidence`
- `release_family_readiness_summaries`

Status:

- completed for live table size, live column list, and live index shape
- partially completed for exact row counts; planner stats are currently unreliable for several hot tables because analyze has not run

### Phase 2. Column ownership map

For each column in the hot tables, document:

- purpose
- writer stage or service
- reader stage or service
- canonical, derived, or debug/audit classification
- live usefulness
- disposition:
  - keep
  - compact
  - prune by retention
  - migrate off
  - drop candidate

### Phase 3. Migration and code reconciliation

For each hot table, record whether:

- the live Docker schema matches active migrations
- the active migrations match current code assumptions
- any live columns appear under-documented
- any code paths appear to depend on fields that should become non-canonical

### Phase 4. Cleanup recommendation set

For each hot table, produce:

- safe short-term cleanup candidates
- medium-risk follow-up changes that need code movement or schema edits
- validation requirements before implementation

## Baseline Capture

Source-of-truth inputs used in this audit pass:

- live Docker database: `gonzb-postgres`, database `gonzb`
- active migrations: `internal/store/pgindex/migrations`
- excluded from current-state reasoning: `internal/store/pgindex/migrations_archive`

Live-stat caveat:

- `pg_stat_user_tables.n_live_tup` is currently unreliable for most hot tables because `last_analyze` and `last_autoanalyze` are empty
- exact full-table counts are safe but expensive on the largest tables, so this audit records exact counts where already measured and otherwise treats planner counts as advisory only

### `article_headers` baseline

Live size:

- `34 GB`

Live row-count status:

- planner estimate currently unreliable because the table has not been analyzed recently

Active migration provenance:

- baseline table in `001_baseline.up.sql`
- assembly claim fields added in `009_article_header_assembly_claims.up.sql`

Live columns:

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
- `assembly_claimed_by`
- `assembly_claimed_until`

Important live indexes:

- unique `(newsgroup_id, article_number)`
- unique `(newsgroup_id, message_id)`
- pending assemble index on `id DESC` where `assembled_at IS NULL`
- pending assemble claim index on `(assembly_claimed_until, id DESC)` where `assembled_at IS NULL`

Baseline audit note:

- this is still the single largest table and carries both durable article facts and hot-path claim state

Ingest ownership map:

- writer: `InsertArticleHeaders` in `internal/store/pgindex/repository.go` writes the durable article facts at scrape ingest time
- writer: `ClaimUnassembledArticleHeaders`, retry release, and successful completion paths in `internal/store/pgindex/assembly_store.go` mutate `assembly_claimed_by`, `assembly_claimed_until`, and `assembled_at`
- readers:
  - assemble claim and candidate selection in `internal/store/pgindex/assembly_store.go`
  - payload retention purge in `internal/store/pgindex/maintenance_store.go`
  - article date range stats in `internal/store/pgindex/enrichment_store.go`
  - joins through `binary_parts` in catalog and inspect reads

Initial column classification and disposition:

- `id`, `provider_id`, `newsgroup_id`, `article_number`, `message_id`: canonical ingest identity, keep
- `date_utc`, `bytes`, `lines`, `scraped_at`: canonical article facts still used downstream, keep
- `assembled_at`: hot-path workflow state and maintenance-retention anchor, keep
- `assembly_claimed_by`, `assembly_claimed_until`: hot-path coordination state for assemble leasing, keep

Initial audit conclusion:

- `article_headers` is not a good early drop-column target
- likely growth control here will come from retention policy decisions, not schema slimming, because all current columns still have active purpose

### `article_header_ingest_payloads` baseline

Live size:

- `23 GB`

Live row-count status:

- exact count measured: `79,109,360`

Active migration provenance:

- baseline table in `001_baseline.up.sql`
- structured subject and poster fields added in `004_article_header_structured_fields.up.sql`
- structured-name lookup index added in `005_indexer_refinement_query_support.up.sql`
- yEnc recovery backoff fields added in `012_article_header_yenc_recovery_backoff.up.sql`

Live columns:

- `article_header_id`
- `subject`
- `poster_id`
- `poster`
- `xref`
- `subject_file_name`
- `subject_file_index`
- `subject_file_total`
- `yenc_part_number`
- `yenc_total_parts`
- `yenc_file_size`
- `raw_overview_json`
- `created_at`
- `yenc_recovery_missing_count`
- `yenc_recovery_last_missing_at`
- `yenc_recovery_retry_after`

Important live indexes:

- primary key on `article_header_id`
- structured-name lookup index on `lower(btrim(subject_file_name)), article_header_id` where `subject_file_name` is non-empty

Measured live-shape notes:

- rows with non-empty `raw_overview_json`: `0`
- rows with non-empty `subject_file_name`: `33,315,428`
- rows with active `yenc_recovery_retry_after`: `0`

Baseline audit note:

- this table is still huge even though its bulkiest JSON payload currently appears unused in stored data

Ingest ownership map:

- writer: `InsertArticleHeaders` in `internal/store/pgindex/repository.go` sanitizes NNTP header input, parses structured metadata from `subject`, and upserts payload rows
- writer: duplicate ingest rows overwrite payload columns on conflict by `article_header_id`
- writer: `RecordYEncRecoveryNotFound` and related update paths in `internal/store/pgindex/assembly_store.go` mutate `yenc_recovery_missing_count`, `yenc_recovery_last_missing_at`, and `yenc_recovery_retry_after`
- writer: `ApplyYEncHeaderRecovery` in `internal/store/pgindex/yenc_recovery_store.go` clears `yenc_recovery_retry_after`
- readers:
  - assemble lane selection and candidate hydration in `internal/store/pgindex/assembly_store.go`
  - subject matcher inputs in `internal/indexing/assemble/service.go` and `internal/indexing/match/*`
  - yEnc recovery candidate selection in `internal/store/pgindex/yenc_recovery_store.go`
  - maintenance retention purge in `internal/store/pgindex/maintenance_store.go`

Important current behavior:

- assemble no longer reads stored `raw_overview_json` on its normal hot path; hydrated candidates rebuild `RawOverview` from structured columns and article facts
- yEnc recovery now rebuilds recovery match input from structured columns and article facts instead of reading stored `raw_overview_json`
- there is already a live maintenance delete for payload rows where the owning `article_headers.assembled_at` is older than `7 days`

Initial column classification and disposition:

- `article_header_id`: canonical join key back to the durable article row, keep
- `subject`: active input to matcher and yEnc recovery, keep for now
- `poster_id`: active normalized poster identity, keep
- `poster`: fallback text only when poster normalization did not resolve; keep for now but treat as fallback data, not canonical identity
- `xref`: active matcher and recovery input, keep for now
- `subject_file_name`, `subject_file_index`, `subject_file_total`, `yenc_part_number`, `yenc_total_parts`, `yenc_file_size`: active structured hot-path fields for assemble and recovery, keep
- `raw_overview_json`: no longer used by assemble or yEnc recovery runtime paths and currently empty in live stored data; drop candidate after retention changes clear old rows
- `created_at`: retention/debug support field, keep for now
- `yenc_recovery_missing_count`, `yenc_recovery_last_missing_at`, `yenc_recovery_retry_after`: active recovery workflow state, keep unless moved to a smaller side surface later

Initial audit conclusion:

- this table is not just dead weight; most structured columns are still on the active assemble and recovery path
- `raw_overview_json` is the strongest early trim candidate inside the table
- if this table still grows too quickly with the existing `7 day` purge, the next likely levers are shorter assembled retention, recovery-state extraction, or more aggressive purging of rows no longer needed by yEnc recovery

Recommended trim policy:

- keep the structured columns needed by assemble and yEnc recovery
- stop writing `raw_overview_json` for new rows unless a current reader still requires original raw NNTP payload data
- implementation status: new ingest writes now persist `'{}'` instead of the incoming raw overview payload, and yEnc recovery no longer reads the column
- replace the current flat `7 day` payload purge with a two-tier assembled-row policy:
  - `1 hour` retention for assembled rows that already have `subject_file_name <> ''` and no active retry state
  - `24 hours` retention for assembled rows that still lack structured filename identity or still participate in yEnc retry/backoff
- implementation status: the two-tier assembled-row purge now runs in `RunIndexerMaintenance`
- implementation status: payload maintenance now walks bounded `article_header_id` windows instead of attempting one giant delete
- if later code changes remove yEnc recovery dependence on `raw_overview_json`, drop the column entirely in a later migration wave

Reasoning:

- the live database reached roughly `100 GB` within one day, so current retention windows are too long for the ingest rate
- live stored data shows `raw_overview_json` is effectively empty already
- normal assemble hydration and yEnc recovery no longer rely on stored raw JSON

Live validation note:

- the first successful local maintenance run on `2026-05-14` reported `purged_header_payloads=7150219`
- exact live payload row count dropped from `79,109,360` to `70,909,141`
- immediate physical table size remained `23 GB`, which is expected before tuple reuse or vacuum reclaim
- a follow-up `2026-05-14` maintenance run reclaimed `389,908` additional payload rows, bringing the exact live row count to `70,519,233`
- follow-up `VACUUM (ANALYZE)` refreshed planner stats for `article_header_ingest_payloads` on `2026-05-14`

### `binaries` baseline

Live size:

- `11 GB`

Live row-count status:

- exact count measured: `9,467,122`
- this is the only hot table in scope with a current `last_autoanalyze` on `2026-05-14`

Active migration provenance:

- baseline table in `001_baseline.up.sql`
- recovery fields added in `002_binary_recovery_fields.up.sql`
- refinement query indexes added in `005_indexer_refinement_query_support.up.sql`
- grouping identity helper fields added in `018_binary_grouping_identity_fields.up.sql`
- archive expected file count added in `020_archive_expected_file_count.up.sql`

Live columns:

- `id`
- `provider_id`
- `newsgroup_id`
- `poster_id`
- `release_key`
- `release_name`
- `binary_key`
- `binary_name`
- `file_name`
- `total_parts`
- `observed_parts`
- `total_bytes`
- `first_article_number`
- `last_article_number`
- `posted_at`
- `status`
- `created_at`
- `updated_at`
- `match_confidence`
- `match_status`
- `grouping_evidence_json`
- `file_index`
- `expected_file_count`
- `expected_archive_file_count`
- `source_release_key`
- `release_family_key`
- `file_family_key`
- `family_kind`
- `base_stem`
- `recovered_kind`
- `recovered_extension`
- `recovered_source`
- `recovered_confidence`
- `recovered_at`
- `is_auxiliary`
- `is_main_payload`
- `file_set_key`
- `identity_strength`
- `identity_reason`
- `subject_set_token`
- `subject_set_kind`

Important live indexes:

- unique `(provider_id, newsgroup_id, binary_key)`
- release-family lookup on `(provider_id, newsgroup_id, release_family_key)`
- normalized file identity lookup
- base-stem family lookup for `expected_file_count > 1`
- archive-expected-family lookup
- file-set key and identity-strength indexes
- updated-at index

Measured live-shape notes:

- rows with non-empty inline `grouping_evidence_json`: `88,313`
- rows with `expected_file_count > 0`: `112,825`
- rows with `expected_archive_file_count > 0`: `13,481`
- rows with non-empty `file_set_key`: `9,467,122`

Baseline audit note:

- `file_set_key` is effectively universal in the live data, while inline grouping evidence is now sparse relative to total row count

Binary identity ownership map:

- writer: `UpsertBinary` in `internal/store/pgindex/assembly_store.go` writes the canonical binary identity fields produced by assemble and the matcher
- writer: `ApplyYEncHeaderRecovery` in `internal/store/pgindex/yenc_recovery_store.go` rewrites identity fields when recovery promotes a better binary key and filename
- writer: `ApplyBinaryRecovery` in `internal/store/pgindex/binary_recovery_store.go` writes recovery metadata and may canonicalize filenames across binaries, release files, and binary parts
- writer: PAR2 coverage updates in `internal/store/pgindex/inspection_store.go` mutate `expected_archive_file_count`, `file_index`, `is_auxiliary`, `is_main_payload`, `base_stem`, and inline grouping summary fragments
- readers:
  - release-family summary refresh in `internal/store/pgindex/release_family_summary_store.go`
  - release candidate selection and release formation in `internal/store/pgindex/release_store.go`
  - yEnc recovery candidate selection in `internal/store/pgindex/yenc_recovery_store.go`
  - catalog, inspect, and admin/debug reads across `catalog_reads.go`, `inspect_reads.go`, and UI admin detail pages

Important current behavior:

- assemble writes canonical identity fields such as `release_family_key`, `file_set_key`, `file_family_key`, `identity_strength`, `identity_reason`, `subject_set_token`, `subject_set_kind`, `base_stem`, `is_auxiliary`, and `is_main_payload`
- `expected_file_count` is a core release/readiness input and is refreshed from matcher/assemble output
- `expected_archive_file_count` is downstream enrichment from archive/PAR2 evidence and is actively consumed by release-family summary refresh and release selection
- inline `grouping_evidence_json` is still updated on the binary row by assemble, yEnc recovery, and PAR2 coverage updates

Initial column classification and disposition:

- `binary_key`, `provider_id`, `newsgroup_id`: canonical binary identity, keep
- `release_key`, `source_release_key`, `release_family_key`, `file_family_key`, `file_set_key`, `base_stem`, `family_kind`: canonical grouping and release identity surfaces, keep
- `binary_name`, `file_name`, `file_index`, `total_parts`, `observed_parts`, `total_bytes`, `posted_at`: canonical binary/file facts, keep
- `expected_file_count`: canonical release/readiness denominator, keep
- `expected_archive_file_count`: active derived enrichment used by readiness and release logic, keep
- `is_auxiliary`, `is_main_payload`: active release and inspect classification, keep
- `identity_strength`, `identity_reason`, `subject_set_token`, `subject_set_kind`: active grouping model support fields and query surfaces, keep
- `recovered_kind`, `recovered_extension`, `recovered_source`, `recovered_confidence`, `recovered_at`: active downstream recovery evidence and canonicalization state, keep
- `grouping_evidence_json`: keep for now, but treat as a compact inline summary surface only; do not let it remain a second full audit trail

Initial audit conclusion:

- `binaries` itself is not dominated by dead columns; most fields still participate directly in grouping, readiness, release formation, recovery, or inspect gating
- the strongest trim angle on `binaries` is reducing or standardizing inline `grouping_evidence_json`, not removing the core identity fields
- because `file_set_key` is now universal, it has moved from “experimental helper” to “active identity key” status

### `binary_parts` baseline

Live size:

- `5206 MB`

Live row-count status:

- planner estimate currently unreliable because the table has not been analyzed recently

Active migration provenance:

- baseline table in `001_baseline.up.sql`

Live columns:

- `id`
- `binary_id`
- `article_header_id`
- `message_id`
- `part_number`
- `total_parts`
- `segment_bytes`
- `file_name`
- `created_at`
- `updated_at`

Important live indexes:

- unique on `article_header_id`
- unique on `(binary_id, part_number)`
- lookup index on `binary_id`

Baseline audit note:

- this remains the canonical membership bridge between article headers and binaries and has not had follow-on schema churn in the active migration set

### `binary_grouping_evidence` baseline

Live size:

- `15 GB`

Live row-count status:

- planner estimate currently unreliable because the table has not been analyzed recently

Active migration provenance:

- baseline table in `001_baseline.up.sql`

Live columns:

- `binary_id`
- `evidence_source`
- `evidence_version`
- `payload_json`
- `updated_at`

Important live indexes:

- primary key on `binary_id`

Baseline audit note:

- the table is extremely large relative to its narrow shape, which points directly at `payload_json` retention as a primary audit target

Grouping evidence ownership map:

- writer: `upsertBinaryGroupingEvidence` in `internal/store/pgindex/assembly_store.go` deletes and rewrites the side-table row whenever assemble upserts a binary
- readers:
  - admin and inspect detail reads in `internal/store/pgindex/inspect_reads.go`
  - admin UI JSON views in `ui/src/modules/admin/AdminReleaseDetailPage.tsx`

Measured live-shape notes:

- exact row count matches the binary population pattern: `9,714,680`
- average `payload_json` size: about `1365` bytes
- max `payload_json` size in the live sample: about `1962` bytes
- all sampled rows currently use `evidence_source = 'matcher'`

Relationship to inline evidence:

- the side table currently behaves like a one-row-per-binary audit blob, not a sparse exception log
- inline `binaries.grouping_evidence_json` is much sparser:
  - rows with inline grouping JSON: `88,877`
  - average inline JSON size when present: about `902` bytes
  - max inline JSON size in the live sample: about `1412` bytes
- admin/debug detail endpoints read the side-table payload, not the inline JSON field
- yEnc recovery and PAR2 coverage logic still reads or appends some inline summary values on `binaries`

Initial column classification and disposition:

- `binary_id`: canonical join key, keep
- `evidence_source`, `evidence_version`: audit metadata, keep if the table remains
- `payload_json`: primary trim target in this table; compact or reduce retention scope
- `updated_at`: audit/change tracking field, keep if the table remains

Initial audit conclusion:

- this is currently the clearest oversized derived surface in the binary identity layer
- the table is not storing selective “interesting events”; it is storing matcher evidence for effectively every binary
- likely trim directions are:
  - compact payload shape aggressively
  - keep only the minimum summary needed for admin/debug
  - or move to change-point retention instead of universal per-binary evidence blobs

Recommended trim policy:

- keep `binaries.grouping_evidence_json` as the compact always-available summary surface
- convert `binary_grouping_evidence` from universal retention to sparse retention
- retain side-table evidence indefinitely only for:
  - low-confidence or provisional matches
  - fallback-driven matches
  - binaries whose identity was later changed by recovery or inspect stages
  - binaries that remain in weak or overgrouped readiness states
- for high-confidence stable binaries:
  - avoid writing side-table evidence rows when possible
  - otherwise purge them after `24 hours`
- compact payload shape:
  - always keep `summary`
  - keep only the evidence modules that explain a weak, fallback, or changed decision
  - drop verbose module-by-module JSON for stable high-confidence rows
- implementation status: assemble now keeps a compact inline `summary` on `binaries.grouping_evidence_json`
- implementation status: detailed `binary_grouping_evidence` rows are now skipped for stable high-confidence matches and retained only for weak, provisional, low-confidence, or fallback-driven cases
- implementation status: inspect/admin detail reads now fall back to inline grouping evidence when the side-table row is absent
- implementation status: maintenance now backfills inline `summary` from legacy side-table rows and purges older stable high-confidence side-table evidence

Live validation note:

- a follow-up successful local maintenance run on `2026-05-14` reported `purged_grouping_evidence=523`
- exact live `binary_grouping_evidence` row count dropped to `10,341,903`
- immediate physical side-table size still read `16 GB`, so retention logic is now ahead of physical reclaim
- follow-up `VACUUM (ANALYZE)` refreshed planner stats for `binary_grouping_evidence` on `2026-05-14`

Reasoning:

- the side table currently averages about `1365` bytes per row across essentially the full binary population
- inline evidence is much sparser and already close to a compact summary role
- current admin/debug readers can continue working if they are pointed at a compacted, selective audit surface instead of a universal blob per binary

Operational note:

- a live audit query already failed with `No space left on device` while PostgreSQL tried to spill temp files, which reinforces that large retained JSON surfaces are an active operational problem

### `release_family_readiness_summaries` baseline

Live size:

- `4701 MB`

Live row-count status:

- planner estimate currently unreliable because the table has not been analyzed recently

Active migration provenance:

- table created in `006_release_family_readiness_summaries.up.sql`
- expected-file coverage fields added in `015_release_family_expected_file_coverage.up.sql`
- processing state and stale cleanup rows added in `016_release_family_summary_processing_state.up.sql`
- dominant family fields added in `017_release_family_weak_single_bucket.up.sql`
- archive expected-file coverage fields added in `020_archive_expected_file_count.up.sql`

Live columns:

- `provider_id`
- `newsgroup_id`
- `key_kind`
- `family_key`
- `source_release_key`
- `release_key`
- `release_name`
- `binary_count`
- `complete_binary_count`
- `incomplete_binary_count`
- `has_expected_file_count`
- `total_bytes`
- `earliest_posted_at`
- `readiness_bucket`
- `processed_at`
- `updated_at`
- `expected_file_count`
- `complete_main_payload_binary_count`
- `expected_file_coverage_pct`
- `dominant_family_kind`
- `dominant_file_name`
- `dominant_match_confidence`
- `expected_archive_file_count`
- `has_expected_archive_file_count`
- `archive_file_coverage_pct`

Important live indexes:

- primary key on `(provider_id, newsgroup_id, key_kind, family_key)`
- pending summary work index on `(updated_at, provider_id, newsgroup_id)` where `updated_at > COALESCE(processed_at, updated_at)`

Measured live-shape notes:

- readiness bucket mix is still heavily skewed toward `weak_single_binary`
- the table includes both active queue state and `stale_cleanup_only` placeholder rows created from dirty-family history

Baseline audit note:

- this is a derived read-model table that is now large enough to deserve explicit retention and pruning rules, not just refresh logic

Readiness ownership map:

- writer: `refreshReleaseFamilySummary` in `internal/store/pgindex/release_family_summary_store.go` rebuilds or updates summary rows from `binaries`
- writer: zero-binary families are preserved as `stale_cleanup_only` placeholders rather than deleted outright
- writer: release-processing acknowledgements in `internal/store/pgindex/release_store.go` update `processed_at` to mark rows as consumed
- readers:
  - release queue selection and candidate ranking in `internal/store/pgindex/release_store.go`
  - yEnc recovery candidate selection in `internal/store/pgindex/yenc_recovery_store.go`
  - overview/dashboard pending counts in `internal/store/pgindex/inspect_reads.go`
  - tests and admin/debug diagnostics around queue state

Important current behavior:

- this table is the active release queue surface, not just a report table
- release selection ranks directly out of `updated_at > COALESCE(processed_at, updated_at)` and then applies readiness-bucket ordering
- `stale_cleanup_only` rows are intentionally retained when a family key has no remaining binaries so downstream cleanup can observe the disappearance
- `weak_single_binary`, `weak_obfuscated_set`, `overgrouped_contextual`, and `prefer_base_stem` are active behavioral states, not documentation-only labels

Measured live-shape notes:

- total rows: `9,937,099`
- pending rows by `updated_at > COALESCE(processed_at, updated_at)`: `684,790`
- rows with `processed_at IS NULL`: `0`
- `stale_cleanup_only` rows: `85,160`
- key-kind distribution:
  - `release_family`: `9,856,662` rows, `683,276` pending
  - `base_stem`: `80,437` rows, `1,514` pending
- readiness-bucket distribution:
  - `weak_single_binary`: `9,489,493` rows, `662,671` pending
  - `fragment_only`: `357,207` rows, `20,370` pending
  - `stale_cleanup_only`: `85,160` rows, `1,336` pending
  - `actionable`: `4,952` rows, `397` pending
  - `weak_obfuscated_set`: `287` rows, `16` pending

Initial column classification and disposition:

- `provider_id`, `newsgroup_id`, `key_kind`, `family_key`: canonical queue/read-model key, keep
- `source_release_key`, `release_key`, `release_name`: active release formation inputs, keep
- `binary_count`, `complete_binary_count`, `incomplete_binary_count`, `complete_main_payload_binary_count`: active queue scoring and readiness logic, keep
- `expected_file_count`, `expected_archive_file_count`, `has_expected_file_count`, `has_expected_archive_file_count`, `expected_file_coverage_pct`, `archive_file_coverage_pct`: active release gating and ordering inputs, keep
- `readiness_bucket`: core queue-state field, keep
- `processed_at`, `updated_at`: queue-state lifecycle fields, keep
- `earliest_posted_at`, `total_bytes`: queue ordering and display support, keep
- `dominant_family_kind`, `dominant_file_name`, `dominant_match_confidence`: active weak-family reclassification inputs in release selection, keep for now

Initial audit conclusion:

- this table is genuinely active, but the vast majority of stored rows are not currently pending release work
- most growth is in retained weak-family state, especially `weak_single_binary`, not in actionable release candidates
- early trim opportunities are more likely to come from pruning stale or long-idle weak-family summaries than from dropping columns
- `stale_cleanup_only` is a minority of the table; the dominant cost driver is the huge retained `weak_single_binary` population

Recommended trim policy:

- keep the table as the active queue/read-model surface for release formation
- never purge rows still pending by `updated_at > COALESCE(processed_at, updated_at)`
- preserve weak-family rows that are still feeding yEnc recovery candidate discovery
- add age-bounded cleanup for non-pending residue:
  - `stale_cleanup_only`: purge after `24 hours` when no matching binaries remain
  - `fragment_only`: purge after `24 hours` when not pending and no stale release cleanup is still needed
  - `weak_single_binary`: purge after `24 hours` when not pending and no recovery-eligible binaries remain in the family
  - `weak_obfuscated_set` and `overgrouped_contextual`: purge after `24 hours` when not pending and no recovery-eligible binaries remain
  - `prefer_base_stem`: purge after `6 hours` when not pending
- rely on normal summary refresh to recreate rows when new binary activity makes a family relevant again
- implementation status: `RunIndexerMaintenance` now prunes non-pending `prefer_base_stem`, `fragment_only`, and `stale_cleanup_only` rows by age
- implementation status: `RunIndexerMaintenance` now prunes non-pending `weak_single_binary`, `weak_obfuscated_set`, and `overgrouped_contextual` rows only when no recovery-eligible binaries remain
- live validation note: the successful local maintenance run on `2026-05-14` reported `purged_readiness_summaries=0`, so current live cleanup pressure is still much higher on ingest payloads than on already-eligible summary residue
- live validation note: a follow-up `2026-05-14` maintenance run later reported `purged_readiness_summaries=9865`, confirming the bounded cleanup does remove aged residue once rows become eligible
- follow-up `VACUUM (ANALYZE)` refreshed planner stats for `release_family_readiness_summaries` on `2026-05-14`
- operator reclaim follow-up on `2026-05-14`:
  - new CLI wrapper validation succeeded with `go run ./cmd/gonzb --config config.yaml indexer maintenance reclaim-storage readiness`
  - live result logged `before_bytes=5490278400`, `after_bytes=5490286592`, `delta_bytes=8192`
  - exact post-run stats for `release_family_readiness_summaries`: `n_live_tup=10551590`, `n_dead_tup=0`, `bytes=5490286592`
  - check-only reclaim preflight also succeeded with `go run ./cmd/gonzb --config config.yaml indexer maintenance reclaim-storage --check`
  - check-only sizes logged:
    - `release_family_readiness_summaries=5490286592`
    - `binary_grouping_evidence=17332527104`
    - `article_header_ingest_payloads=24795742208`
  - live `VACUUM FULL` remains blocked for now because host `/` free space was only `5.3G`, below the smallest current hot-table rewrite target

Reasoning:

- the table currently has about `9,937,099` rows but only about `684,790` pending rows
- `weak_single_binary` alone accounts for about `9,489,493` rows, far larger than actionable queue state
- release and recovery logic already differentiate pending and non-pending states through `processed_at`, `updated_at`, and readiness buckets

Safety constraints:

- release candidate selection reads directly from this table, so pruning must not remove pending rows
- yEnc recovery candidate selection explicitly targets `weak_single_binary`, `weak_obfuscated_set`, and `overgrouped_contextual`, so those rows must survive while they are still actionable

## Migration Reconciliation Summary

Current reconciliation status:

- the live Docker schema matches the active migration story for the six hot tables
- the live schema includes all follow-on fields expected from migrations `009`, `012`, `015`, `016`, `017`, `018`, and `020`
- no discrepancy was found that would require consulting `migrations_archive` for current-state truth

Important audit observations from migration history:

- `article_header_ingest_payloads` evolved from a payload capture table into a structured-field plus retry-state table, but still retains the large-payload storage shape
- `binaries` accumulated both inline grouping evidence and side-table grouping evidence instead of fully collapsing onto one storage surface
- `release_family_readiness_summaries` absorbed queue state, stale placeholder rows, dominant-family hints, and archive coverage fields over several migrations and is now carrying multiple roles

## Operator Reporting Status

Current implementation status:

- admin dashboard cached stats now expose exact row counts for:
  - `article_header_ingest_payloads`
  - `binary_grouping_evidence`
  - `release_family_readiness_summaries`
- admin dashboard cached stats now expose total on-disk bytes for those same tables
- admin dashboard cached stats now expose planner-visible dead-tuple counts for those same tables
- this gives operators a direct read on:
  - current hot-table growth
  - whether retention cleanup is reducing rows
  - whether vacuum has caught up enough to clear dead tuples

## Table Audit Templates

### `article_headers`

Status: open

Questions to answer:

- which columns are the durable per-article facts required after assembly
- whether assembled rows can be aged out later or only payload-side data should be trimmed first
- whether assembly claim fields remain purely hot-path coordination state

### `article_header_ingest_payloads`

Status: open

Questions to answer:

- which structured fields are still required by assemble, release-adjacent reads, or recovery
- whether `raw_overview_json` is still needed after typed fields and article facts are persisted
- whether yEnc retry/backoff state belongs here long term

### `binaries`

Status: open

Questions to answer:

- which identity fields are canonical for grouping and release formation
- whether `grouping_evidence_json` should remain inline, become compact summary-only state, or move out entirely
- which helper fields are still needed after the grouping-model sprint

### `binary_parts`

Status: open

Questions to answer:

- whether all persisted membership fields remain necessary for NZB export, inspect, and recovery
- whether any duplicated filename or part metadata is now redundant with upstream canonical surfaces

### `binary_grouping_evidence`

Status: open

Questions to answer:

- which evidence rows are meaningful change-point or promotion history
- whether repeated steady-state evidence can be compacted or aged out
- what minimum audit trail is required to debug grouping regressions

### `release_family_readiness_summaries`

Status: open

Questions to answer:

- which readiness rows are active queue state versus stale weak-family residue
- whether `weak_single_binary`, `fragment_only`, and `stale_cleanup_only` need bounded retention
- which columns are only needed for admin/debug surfaces

## Handoff Sessions

Use these as bounded Codex work chunks:

1. `schema-audit-live-baseline`
2. `schema-audit-ingest-columns`
3. `schema-audit-binary-identity-columns`
4. `schema-audit-readiness-columns`
5. `schema-audit-unused-code-and-debug-surfaces`
6. `schema-audit-cleanup-recommendations`

Each session should update this doc with:

- findings
- files or services examined
- decisions made
- open blockers

## Commit Mapping

Use this mapping when working through the sprint on one branch:

1. `schema-audit-live-baseline`
   Maps to commit `schema-audit-live-db`
2. `schema-audit-ingest-columns`
   Maps to commit `code-usage-map-ingest-and-assembly`
3. `schema-audit-binary-identity-columns`
   Maps to commit `code-usage-map-binary-identity`
4. `schema-audit-readiness-columns`
   Maps to commit `code-usage-map-readiness-and-ui`
5. `schema-audit-unused-code-and-debug-surfaces`
   Usually lands inside commit `code-usage-map-readiness-and-ui` or the first cleanup implementation wave, depending on what the audit finds
6. `schema-audit-cleanup-recommendations`
   Maps to commits `trim-policy-payloads-and-evidence` and `trim-policy-readiness-and-junk-families`

## Codex Responsibilities

Codex is expected to perform:

- live Docker database inspection
- active migration reconciliation
- code reader and writer mapping
- documentation updates
- cleanup implementation
- validation and sign-off evidence capture

## Sign-Off

Open.
