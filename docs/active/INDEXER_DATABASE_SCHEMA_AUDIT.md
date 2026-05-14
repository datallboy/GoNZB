# Indexer Database Schema Audit

Snapshot date: 2026-05-14

This is the active current-state audit doc for the database growth-trim sprint.

This doc is updated on the same sprint branch as the implementation work. Treat the commit order in `docs/active/INDEXER_DATABASE_GROWTH_TRIM_PLAN.md` as the execution order for audit and cleanup sessions.

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

## Migration Reconciliation Summary

Current reconciliation status:

- the live Docker schema matches the active migration story for the six hot tables
- the live schema includes all follow-on fields expected from migrations `009`, `012`, `015`, `016`, `017`, `018`, and `020`
- no discrepancy was found that would require consulting `migrations_archive` for current-state truth

Important audit observations from migration history:

- `article_header_ingest_payloads` evolved from a payload capture table into a structured-field plus retry-state table, but still retains the large-payload storage shape
- `binaries` accumulated both inline grouping evidence and side-table grouping evidence instead of fully collapsing onto one storage surface
- `release_family_readiness_summaries` absorbed queue state, stale placeholder rows, dominant-family hints, and archive coverage fields over several migrations and is now carrying multiple roles

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
