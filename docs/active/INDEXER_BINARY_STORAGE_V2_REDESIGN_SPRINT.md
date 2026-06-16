# Indexer Binary Storage V2 Redesign Sprint

Snapshot date: 2026-06-15

This is the live sprint document for the binary-storage redesign. It replaces the assumption that the current `binaries` table is safe to keep extending.

## Summary

The repeated corruption incidents should not be treated as maintenance-stage bugs. Maintenance and release reads exposed already-damaged `public.binaries` pages. The architectural problem is that `binaries` became a wide, heavily indexed, multi-owner, high-churn table touched by assemble, recovery, inspection, release refresh, maintenance, and purge.

PostgreSQL SQL should not physically corrupt heap or index pages by itself. The current shape still maximizes WAL pressure, bloat, autovacuum pressure, checkpoint pressure, and concurrent update exposure on the hottest relation. Because this is alpha software and the old database has been deleted, the correct direction is to redesign the binary storage path instead of preserving the old schema.

## Current Touch Audit

Resolved direct `binaries` write surfaces:

- `assemble_lane_a` and `assemble_lane_b` now create/update `binary_core`, `binary_observation_stats`, `binary_identity_current`, `binary_recovery_current`, and `binary_lifecycle` directly.
- `recover_yenc` now promotes recovered identity into `binary_core`, `binary_identity_current`, `binary_observation_stats`, and `binary_recovery_current`; merge cleanup deletes the source `binary_core` row.
- `binary_recovery` now updates `binary_recovery_current` and canonicalized filenames in `binary_identity_current`, `release_files`, and `binary_parts`.
- `release_purge_archived_sources` now deletes terminal binary lineage through `binary_core`, which is the canonical cascade anchor.
- production store ownership tests now reject direct SQL access to `binaries`; migration files and tests remain the only tolerated references.

Current read-heavy surfaces:

- `release_summary_refresh` reads binary family, identity, expected counts, observed counts, payload/auxiliary flags, and recovered fields.
- `release_formation` reads candidate binaries, release-family grouping, parts, and completeness.
- `inspect_*` stages read binary identity and parts to decide whether work is actionable.
- catalog/admin reads join `binaries` for release detail, file lists, and inspection artifacts.
- yEnc work-item backfill and recovery reads `binaries` for candidate selection and merge decisions.

The old ownership exception policy is too broad. Stage write ownership must be enforced by schema and code, not just documentation.

## Target Architecture

The final target is to remove the monolithic hot `binaries` table as the canonical state store. The transition starts with v2 side tables because many current reads and foreign keys still require the existing anchor while the stores are rewritten.

Target table ownership:

- `binary_core`: assemble-owned immutable anchor projection for provider, group, poster, binary key, original binary name, and creation timestamp.
- `binary_observation_stats`: assemble-owned mutable stats for total parts, observed parts, bytes, first/last article number, posted timestamp, and stats timestamp.
- `binary_identity_current`: assemble/projector-owned current grouping identity for release family, file set, file family, expected counts, match confidence, subject-set identity, auxiliary/main-payload flags, and grouping summary scalars.
- `binary_recovery_current`: recovery-owned current recovered kind, extension, source, confidence, recovered filename, and recovery timestamp.
- `binary_lifecycle`: release/archive/purge-owned lifecycle state for release linkage, archive state, purge eligibility, and terminal timestamps.
- `binary_projection_events`: append-only future event stream for identity, recovery, inspection, and lifecycle changes.
- `release_family_dirty_queue`: the only trigger surface for release summary refresh; no raw binary scans should be needed to discover changed families.

Hot JSONB is not acceptable for behavior-bearing mutable state. JSONB remains acceptable for cold evidence artifacts and append-only diagnostics.

## Migration Direction

The database can be rebuilt from scratch, so no corrupt-data migration is required.

Phase A, implemented first:

- Keep the existing `binaries` table as the temporary foreign-key anchor and read compatibility surface.
- Add v2 side tables for stage-owned binary state.
- Dual-write assemble, yEnc recovery, binary recovery, stat refresh, lifecycle purge, and maintenance cleanup into the v2 tables.
- Document table ownership in a database table so ownership can be surfaced and tested.

Phase B:

- Move release-summary-refresh, release formation, inspect selection, yEnc selection, and catalog reads to the v2 side tables.
- `release_summary_refresh` scheduled summary aggregate/dominant reads now use `binary_identity_current`, `binary_observation_stats`, and `binary_core` instead of behavior-bearing fields on `binaries`.
- release formation binary fan-out (`ListBinariesForReleaseCandidate`) now uses the same v2 projection tables instead of behavior-bearing fields on `binaries`.
- release reform candidate discovery (`ListExistingReleaseCandidates`) now derives candidate binaries from the v2 projection tables.
- Stop reading behavior-bearing fields from the legacy `binaries` row.
- Add SQL ownership tests that fail on forbidden writes.

Phase B remaining reader migration checklist:

- [x] `release_summary_refresh` queued summary aggregate/dominant reads.
- [x] release formation binary fan-out reads.
- [x] release reform candidate discovery reads.
- [x] yEnc recovery work-item selection, stale-retire, seed, and target reads.
- [x] inspect candidate selection reads for discovery, PAR2, NFO, archive, password, and media stages.
- [x] catalog/detail/admin/public release reads.
- [x] NZB generation, archive, and purge reads.
- [x] maintenance/helper reads and backlog counters.
- [x] ownership tests expanded to reject new legacy behavior-field reads/writes once each path migrates.

Remaining intentional temporary `binaries` references:

- migration files still create and carry the legacy table so older dev databases can migrate forward.
- tests may still create or mutate legacy rows where they exercise migration/backward-compatibility setup.
- production store code must not read, write, lock, or delete `binaries`.

Phase C:

- Replace `binaries` with a narrow anchor or compatibility view.
- Move foreign keys to the canonical anchor.
- Drop legacy behavior columns and legacy hot indexes.
- Add hash partitioning for high-volume tables.

## Partitioning Plan

Use PostgreSQL declarative hash partitioning once read paths are off the legacy table:

- Partition `article_headers`, `article_header_ingest_payloads`, `assembly_work_items`, `binary_core`, `binary_parts`, `binary_observation_stats`, `binary_identity_current`, `binary_recovery_current`, and high-cardinality dirty/event tables by `HASH(provider_id, newsgroup_id)`.
- Start with 128 partitions.
- Include partition keys in unique constraints where required.
- Use fillfactor 100 for append-mostly tables and 80 for mutable projection/work tables.
- Set per-table autovacuum thresholds on mutable projection/work tables.

## Enforcement Plan

Add enforcement in code and CI:

- Split store access behind narrow interfaces: scrape, assemble, recovery, inspection, release, archive, purge, and maintenance.
- Add a table ownership policy in migrations and mirror it in a Go test.
- Add SQL scanner tests that reject `INSERT`, `UPDATE`, or `DELETE` against tables outside an allowed owner list.
- Treat direct production reads or writes to legacy `binaries` as failures.
- Remove test helpers that mutate `binaries` directly unless they are specifically testing the bridge.

## Validation Plan

Before stable schema freeze:

- Fresh database migrations apply cleanly from zero.
- Scrape, assemble, yEnc recovery, release-summary-refresh, release formation, inspect, NZB generation, archive, and purge all run on the v2-backed schema.
- `pg_amcheck` passes on critical heaps and indexes after sustained all-stage soak.
- EXPLAIN plans show no full scans over raw article or binary state for scheduled hot stages.
- Dashboard exposes relation size, dead tuples, autovacuum timestamps, checksum failures, refresh queue depth, and per-stage throughput.

## Explicit Defaults

- PostgreSQL remains the primary store for v0.8.
- Current corrupt data is discarded.
- The first v2 implementation is a bridge, not the final narrow-anchor schema.
- Partition count target is 128.
- Public API and UI behavior should remain stable while internals move.

## Scrape Ingest V2 Append Plan

The scrape deadlock on poster upsert showed the same ownership problem as `binaries`: scrape was doing hot ingest plus dimension/summary materialization in the same transaction. That makes poster dimension locks and crosspost rollup locks part of the article ingest critical path.

Implemented target shape:

- `scrape_latest` and `scrape_backfill` insert canonical/raw ingest rows only: `article_headers`, `article_header_ingest_payloads`, `article_header_crosspost_groups`, checkpoints, and queue seeds.
- `poster_materialize` owns `posters` and `article_header_poster_refs`.
- `crosspost_popularity_refresh` owns `article_header_crosspost_group_summary`, `article_header_crosspost_group_messages`, and `article_header_crosspost_group_sources`.
- `poster_materialization_queue` is seeded by scrape and claimed/completed by `poster_materialize`.
- `crosspost_popularity_refresh_queue` is seeded by scrape/manual backfill and claimed/completed by `crosspost_popularity_refresh`.

Stage prerequisites:

- scrape requires provider/newsgroup config and healthy critical article-header indexes.
- `poster_materialize` requires queued raw poster names from scrape.
- `crosspost_popularity_refresh` requires raw `article_header_crosspost_groups`.
- assemble may continue while these materializers lag; raw poster text remains in `article_header_ingest_payloads` and poster dimension linkage is eventually consistent.

This is intentionally not a retry-loop-only fix. Retries remain useful for transient PostgreSQL conflicts, but the structural fix is removing shared dimension/summary writes from the scrape insert transaction.

## Phase A/B Closeout Validation

Validation date: 2026-06-15

Result: Phase A/B is complete, but this branch is not complete while Phase C remains open. The v2 side-table bridge passed the write-contention soak, but the legacy `binaries` anchor must still be replaced before this sprint can close.

Observed fresh-database serve soak:

- all enabled stages executed: `scrape_latest`, `scrape_backfill`, `assemble_lane_a`, `assemble_lane_b`, `recover_yenc`, `release_summary_refresh`, `release`, `inspect_discovery`, `inspect_par2`, `inspect_nfo`, `inspect_archive`, `inspect_password`, `inspect_media`, `release_generate_nzb`, `release_archive_nzb`, `release_purge_archived_sources`, and `indexer_maintenance`.
- scrape materializer queues were seeded during the run. `poster_materialize` and `crosspost_popularity_refresh` are wired as supervisor stages, but were disabled in the runtime settings used for the serve soak.
- materializer CLI validation passed after the serve soak: `materialize-posters --batch-size 10000` claimed 10,000 rows and upserted 10,000 refs; `refresh-crosspost-popularity --batch-size 1000` claimed 86 groups, refreshed 86 summaries, and upserted 634,073 message rows.
- release outputs were produced and archived/purged: `nzb_cache` rows existed, release catalog rows existed, and `release_archive_state` reached `purged`.
- v2 projection parity held after the soak: `binaries`, `binary_core`, `binary_identity_current`, `binary_observation_stats`, `binary_recovery_current`, and `binary_lifecycle` had matching row counts.
- PostgreSQL logs contained no application deadlock, corruption, recovery-mode, invalid-page, or unexpected-EOF errors during the serve window.
- `pg_amcheck -U postgres -d gonzb --schema=public` completed with no corruption output after the soak.
- `go test ./...` passed after the closeout guard changes.

Residual notes:

- stage failures recorded during the final window were caused by the intentional Ctrl-C shutdown and had `context canceled` errors.
- serve shutdown exceeded its graceful deadline after cancellation; this is cleanup polish, not a database-integrity blocker.
- direct production `binaries` access has been removed; remaining references are migration/test compatibility only.
- inspection candidate selection can still perform broad v2 projection scans during bursts. It did not block writers or corrupt data in this soak, but it is the next throughput optimization target if inspection becomes the dominant load.
- crosspost popularity refresh currently performs full-group aggregation for queued groups. It completed successfully, but the observed batch was heavy enough that a delta or smaller-batch strategy should be considered before enabling it aggressively in supervisor defaults.

## Phase C Required Work

This branch cannot close until Phase C is complete.

- [x] Replace `binaries` as the schema-level foreign-key root with `binary_core`.
- [x] Move child-table foreign keys away from legacy `binaries`.
- [x] Stop assemble from inserting/updating `binaries`; write canonical binary state directly to `binary_core`, `binary_observation_stats`, and `binary_identity_current`.
- [x] Stop release-summary compatibility helpers from reading `binaries`.
- [x] Stop inspection claim/start/finish and recovery helpers from checking/locking `binaries`.
- [x] Stop recovery and purge from updating/deleting `binaries`.
- [x] Remove behavior-bearing legacy binary columns from active read/write paths.
- [x] Remove `binary_storage_v2.go` legacy projection backfill dependency after canonical writes land directly in v2 tables.
- [x] Decompose `recover_yenc` and `binary_recovery` so they mutate recovery-owned v2 tables and do not update/delete legacy `binaries`.
- [x] Move inspection claim/start/finish FK safety to the new canonical binary anchor.
- [x] Update release purge so terminal cleanup deletes through the new anchor/source lineage contract instead of deleting `binaries` as the cascade root.
- [x] Expand ownership scanner tests from allowlisted bridge access to rejecting all production `binaries` table access, except compatibility view definitions or migration-only cleanup.
- [ ] Remove or freeze `binaries` as a compatibility view/table in a later schema-squash cleanup after test fixtures and old-database compatibility are retired.

## Crosspost Popularity Refresh Redesign

The current refresh is correct but too heavy for aggressive steady-state scheduling because it re-aggregates all raw rows for each dirty observed group. The better long-term shape is an incremental rollup, not a giant maintenance query and not inline scrape summary writes.

Recommended direction:

- keep scrape writing only raw `article_header_crosspost_groups` and dirty queue rows
- replace full-group refresh with batch-local delta ingestion keyed by newly observed raw rows
- add processed/high-water state, such as `(observed_group_name, last_article_header_id)` or a claimed raw-row queue, so each raw observation is rolled up once
- keep exact distinct helper tables, but insert only new `(observed_group_name, message_id)` and `(observed_group_name, source_newsgroup_id)` keys from the delta batch
- update summary counters from inserted delta counts, not from `COUNT(*)`/`COUNT(DISTINCT ...)` over all historical raw rows
- keep a manual full-rebuild command for repair/report regeneration, not normal scheduled execution

This preserves ownership boundaries: scrape owns raw observations, `crosspost_popularity_refresh` owns reporting tables, and no release-critical stage depends on the report.
