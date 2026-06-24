# Source/Work Partitioning Sprint Plan

## Summary

Implement daily PostgreSQL range partitioning for indexer source/work data so
retention purging older than the active source-work horizon can drop dated
partitions instead of deleting millions of rows. Use Option B: keep durable
release/archive/catalog data and `binary_core` unpartitioned, while
partitioning article-header lineage, binary-derived work/projection tables,
yEnc work, inspect work, and derived release-formation queues by a standardized
`source_posted_at timestamptz not null`.

Defaults:

- Partition key: `source_posted_at`.
- Partition grain: daily.
- Rollout target: fresh/rebuilt indexer database first.
- Active work-window behavior is owned by
  `2026-06-24-indexer-active-work-window-pipeline-plan.md`; retention should
  not decide what the pipeline processes.

Do not start schema partitioning before the active-window plan is finalized and
implemented, except for harmless prep such as docs and read-only audits.
Retention depends on `source_work_campaigns` and `source_work_windows` to know
whether a dated partition still contains active or incomplete work.

## Phase 1: Schema Foundation

Add `source_posted_at timestamptz not null` to all partitioned source/work
tables and make every write path populate it from the original article posted
time.

Partitioned source/header tables:

- `article_headers`
- `article_header_ingest_payloads`
- `article_header_crosspost_groups`
- `article_header_poster_refs`
- `article_header_assembly_queue`
- `poster_materialization_queue`

Partitioned binary work/projection tables:

- `binary_parts`
- `binary_observation_stats`
- `binary_identity_current`
- `binary_recovery_current`
- `binary_lifecycle`
- `binary_completion_keys`
- `binary_grouping_evidence`
- `binary_projection_events`
- `binary_superseded_sources`

Partitioned inspect/yEnc work tables:

- `yenc_recovery_work_items`
- `binary_inspection_ready_queue`
- `binary_inspections`
- `binary_inspection_artifacts`
- `binary_archive_entries`
- `binary_text_evidence`
- `binary_media_streams`
- `binary_par2_sets`
- `binary_par2_targets`

Partitioned release-formation derived tables:

- `release_family_readiness_summaries`
- `release_ready_candidates`
- `release_recovered_file_set_candidates`
- `release_stage_dirty_families`

Keep these durable/public tables unpartitioned:

- `source_work_campaigns`
- `source_work_windows`
- `binary_core`
- `releases`
- `release_files`
- `release_catalog_files`
- `release_newsgroups`
- `release_archive_state`
- `release_archive_detail_*`
- `release_archive_lineage_*`
- `nzb_cache`
- `release_overrides`
- enrichment metadata
- public archive/catalog tables

Add `binary_core.source_posted_at` and index
`(source_posted_at, binary_id)` for root cleanup/reporting, but do not
partition `binary_core` in v1. This avoids partitioned-FK and global uniqueness
issues at the root while the large child/work tables become droppable by day.

High-volume source/work rows should also carry `source_work_window_id` for
retention reporting and lifecycle checks:

- required on new writes for `article_headers`,
  `article_header_assembly_queue`, `binary_parts`,
  `binary_observation_stats`, `binary_identity_current`,
  `binary_recovery_current`, and `yenc_recovery_work_items`;
- optional but recommended on supporting detail rows when it can be copied
  without extra broad joins;
- never use `source_work_window_id` as the only query boundary, because release
  families can span window overlap. Use `source_posted_at` for pruning and
  overlap matching.

## Phase 2: Partitioned Table Migration

Implement as a fresh/rebuild-first migration. The migration must fail fast with
a clear error if target source/work tables contain data, unless a later explicit
online migration is added.

Create daily range-partitioned parents for all partitioned tables using:

```sql
PARTITION BY RANGE (source_posted_at)
```

Create a default partition for safety, but add audit warnings if rows route
there.

PostgreSQL FK/unique caveat:

- Partitioned unique constraints must include the partition key.
- `article_headers` primary key becomes `(source_posted_at, id)`.
- Child tables that reference article headers use composite FKs:
  `(source_posted_at, article_header_id) ->
  article_headers(source_posted_at, id)`.
- Binary child/work tables use keys such as
  `(source_posted_at, binary_id)` while retaining non-unique lookup indexes on
  `binary_id`.
- Existing logical uniqueness such as `binary_parts(binary_id, part_number)`
  and `yenc_recovery_work_items(binary_id)` becomes partition-scoped with
  `source_posted_at` included in the constraint. Application code must always
  route by stable `source_posted_at`.

Add partition-management functions:

- create partitions for `today - 1 day` through `today + 8 days` for every
  partitioned table;
- create missing partitions idempotently;
- list partitions and default-partition row counts;
- detach/drop a completed day in dependency order.

## Phase 3: Store And Query Updates

- Standardize all write records to carry `SourcePostedAt` from original article
  posted time.
- Ingest writes `article_headers.source_posted_at = date_utc`, then uses the
  same value for payloads, crosspost rows, poster refs, assembly queue, and
  poster queue.
- Assemble propagates `source_posted_at` into `binary_parts`,
  `binary_observation_stats`, `binary_identity_current`,
  `binary_recovery_current`, `binary_lifecycle`, `binary_completion_keys`, and
  binary-derived work rows.
- yEnc recovery uses `source_posted_at` instead of mixed
  `date_utc`/`posted_at` for queue routing and time-window candidate selection.
- Inspect ready-refresh and inspect writes copy `source_posted_at` from binary
  observation/current state into ready queue, inspection history, artifacts,
  archive entries, media streams, PAR2 rows, and text evidence.
- Release summary/candidate refresh paths persist `source_posted_at`, normally
  equal to the candidate's earliest posted time, and include it in upsert keys
  for partitioned derived tables.
- Replace broad `date_utc`/`posted_at` retention predicates on source/work
  tables with `source_posted_at` predicates so partition pruning is used.

Required index paths:

- Article scrape/backfill: `(newsgroup_id, source_posted_at DESC, article_number)`.
- Assembly claim: `(source_posted_at, claim_until, article_header_id DESC)`,
  plus structured-name indexes with source date included.
- yEnc ready/recent window:
  `(status, source_posted_at DESC, priority_rank, updated_at DESC, binary_id)`.
- Binary observation retention/candidates:
  `(source_posted_at, binary_id)` and existing completion indexes inside each
  partition.
- Release/summary refresh: provider/newsgroup/family-key indexes with
  `source_posted_at`.
- Inspect ready queues: existing ready/running indexes with
  `source_posted_at` included where date-window pruning matters.

Index/query regression requirements:

- Before converting a table to partitions, inventory the current indexes and
  hot query predicates that use that table. The migration must recreate the
  equivalent runtime-speed indexes on the partitioned parent or every child,
  with `source_posted_at` added only where needed for pruning.
- Do not treat partitioning as a substitute for status/claim/readiness indexes.
  Partition pruning narrows by date; it does not replace indexes used to find
  ready work inside the day.
- Preserve existing sort/order keys used by candidate selection. If a stage
  currently orders by priority, retry time, claim expiry, completeness, or
  article number, the partitioned query must keep that behavior unless the
  active-window plan explicitly changes it.
- Query predicates must be written so PostgreSQL can prune partitions using
  `source_posted_at` constants or stable bind parameters. Avoid expressions
  around `source_posted_at` that prevent pruning, such as
  `date(source_posted_at) = ...` on hot paths.
- Cross-partition queries are allowed only when the stage intentionally spans an
  active-window overlap or an explicit admin/reporting range. Ordinary stage
  ticks should not scan all partitions.
- Any new admin/reporting query that counts or groups large source/work history
  must be a manual report, a bounded date query, or backed by stored summaries.
  Do not add unbounded `count(*)` dashboard refreshes over partitioned source
  tables.
- Implementation commits that alter hot candidate queries must include
  representative `EXPLAIN (ANALYZE, BUFFERS)` output in the PR/body or sprint
  notes showing partition pruning or partition-local index scans.
- If a migrated query regresses to scanning every child partition or doing a
  broad append followed by filtering, fix the query/index before moving to the
  next table.

## Phase 4: Retention Drop Workflow

Add maintenance CLI/API/UI task: `partition_retention_drop`.

Task behavior:

- dry-run lists partitions older than configured retention;
- checks default partitions are empty or reports them;
- checks `source_work_windows` for the day;
- checks no running assemble/yEnc/inspect/stage claims exist for that day;
- checks durable release/archive/catalog preservation before drop;
- detaches/drops partitions in dependency order.

Eligibility rules:

- A day partition is eligible only when every overlapping source work window is
  terminal: `complete` or `abandoned`.
- Refuse drop if any overlapping window is `pending`, `discovering`,
  `scraping`, `draining`, `paused`, or `failed`.
- `failed` is not terminal. It must be retried or explicitly abandoned.
- Refuse drop if any campaign that owns overlapping windows is non-terminal.
- Refuse drop if any matching window has running claims or nonzero blockers
  that have not been refreshed since the latest stage run.
- Refuse drop if durable release/archive/catalog preservation checks fail.
- Retention does not decide whether old work should be processed. It only
  reclaims source/work data after the active-window lifecycle says it is safe.

Dependency order for a dated partition drop:

1. Work queues: `binary_inspection_ready_queue`,
   `article_header_assembly_queue`, `poster_materialization_queue`,
   `yenc_recovery_work_items`, release candidate/summary queues.
2. Inspect/binary derived detail: `binary_inspection_artifacts`,
   `binary_archive_entries`, `binary_text_evidence`, `binary_media_streams`,
   `binary_par2_targets`, `binary_par2_sets`, `binary_inspections`.
3. Binary projections/work: `binary_grouping_evidence`,
   `binary_completion_keys`, `binary_recovery_current`,
   `binary_identity_current`, `binary_observation_stats`, `binary_lifecycle`,
   `binary_projection_events`, `binary_superseded_sources`.
4. Linkage: `binary_parts`.
5. Article support rows: `article_header_poster_refs`,
   `article_header_crosspost_groups`, `article_header_ingest_payloads`.
6. Source anchor: `article_headers`.
7. Post-drop cleanup: batch-delete unreferenced old `binary_core` roots by
   `source_posted_at`. This is small compared with cascading all child tables
   because children are already dropped.

Do not drop durable release/archive/catalog tables. `release_archive_lineage_*`
keeps IDs and archive audit data after source partitions are gone.

Retention interaction with active work windows:

- Retention must never drop a partition containing an active/incomplete
  `source_work_window`.
- If the server was off and resumes an old 15-minute active window, that
  partition is retained until the window completes or is explicitly abandoned.
- Old missed time is not automatically scraped. It becomes a manual backfill
  campaign owned by the active work-window plan.
- Paused missed-latest-gap campaigns block retention for their date range until
  an admin resumes/completes them or abandons them.
- Abandoned windows mean the admin/system accepts losing future releases from
  that source/work slice, but durable release/archive/catalog data must still be
  preserved.

## Phase 5: Documentation And Operations

- Update operator docs to state source/work retention is partition-based,
  deletes are fallback only, and `source_posted_at` is canonical.
- Add admin maintenance-page report for existing partition range, missing future
  partitions, default partition row counts, eligible retention partitions, and
  estimated tables/files dropped.
- Include active-window blockers in the partition report:
  - non-terminal windows by day;
  - paused campaigns by day;
  - failed windows requiring retry or abandon;
  - running claims by day.
- Add a startup/maintenance guard that creates tomorrow's partitions before
  scrape/assemble/yEnc can write into a new day.
- Document that daily partitioning is safe for 7-30 day retention windows;
  60-90 days requires review, and 180+ days across all partitioned work tables
  should trigger a partition-count/design review.

## Test Plan

- Fresh DB migrates with partitioned parents and today/future daily partitions.
- Migration fails clearly when run against non-empty source/work tables in
  fresh/rebuild-first mode.
- Article ingest routes rows to correct article/payload/crosspost/poster/queue
  partitions.
- Assemble writes binary parts and binary projections with matching
  `source_posted_at`.
- yEnc and inspect queues preserve existing candidate behavior and use
  partition-pruned date windows.
- Release summary/candidate upserts work with partition-key-inclusive
  constraints.
- Retention dry-run reports eligible partitions and dependency order.
- Retention drop refuses when running claims or non-terminal source work
  windows exist.
- Retention drop allows only days whose overlapping windows are all `complete`
  or `abandoned`.
- Retention drop preserves `releases`, `release_files`,
  `release_catalog_files`, archive detail, archive lineage, and archived NZBs.
- `EXPLAIN` for scrape/assemble/yEnc/inspect/release refresh shows partition
  pruning or partition-local index usage.
- Run `go test ./...`, `npm run build`, and `git diff --check` before
  completing implementation.

## Execution Instructions For Codex Sessions

- Do not partition durable public release/archive/catalog tables.
- Do not rely on `article_headers.id` alone for partitioned FKs; use composite
  `(source_posted_at, article_header_id)` relationships.
- Do not ship this migration as safe for the current large live DB. This sprint
  targets a fresh/rebuilt indexer database first.
