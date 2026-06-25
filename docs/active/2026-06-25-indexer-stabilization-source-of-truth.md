# Indexer Stabilization Source Of Truth Sprint

This is the only active execution plan for the current indexer sprint. The
previous dated active plans were archived because they mixed incompatible
models: short work windows, daily buckets, yEnc admission, and partial
partitioning. Use those archived files only as historical context.

## Canonical Reference

`docs/INDEXER_CURRENT_SCHEMA_AND_SYSTEM_INTERACTIONS.md` is the canonical
long-lived reference for:

- stage ownership;
- table shapes and table ownership;
- allowed reads and writes;
- forbidden write-backs;
- query-shape policy;
- retention and purge policy;
- release formation data contracts.

If another document contradicts that reference, update or remove the
contradiction before changing code. `docs/INDEXER_HOW_IT_WORKS.md`,
`docs/INDEXER_RELEASE_FORMATION_PLAYBOOK.md`, and
`docs/INDEXER_STORAGE_RETENTION_AND_PURGE.md` may provide narrative or
operator detail, but they must defer to the canonical reference for stage/table
ownership.

## Sprint Goals

1. Stop schema and query drift by making stage ownership enforceable in tests.
2. Fix assemble regressions caused by partition/retention work.
3. Complete the partial partitioning design for source/work retention.
4. Keep release formation correct while making retention drops cheap.
5. Keep yEnc recovery bounded and stage-owned.

## Non-Negotiable Ownership Rules

- `article_headers` is an immutable scrape-owned fact table. It is not an
  assemble claim, retry, progress, or completion table.
- `article_header_assembly_queue` is the assemble claim/progress surface.
  Assemble completes input work by deleting queue rows after writing
  `binary_parts`.
- Assemble owns binary creation/projection writes:
  `binary_core`, `binary_parts`, `binary_observation_stats`,
  `binary_identity_current`, `binary_completion_keys`, and related assemble
  projections.
- `recover_yenc` owns recovery work and recovered identity state:
  `yenc_recovery_work_items`, `binary_recovery_current`, recovered projection
  updates, superseded-source lineage, completion-key refresh, and release dirty
  queue enqueue.
- `release_summary_refresh` is the heavy writer for readiness summaries and
  ready candidates.
- Release formation writes release-owned catalog/lineage tables and must not
  lock or mutate assemble-owned binary rows as progress state.
- Inspect stages write only inspection-owned history/evidence/ready state.
- Source purge is the only intentional terminal mutator/deleter of upstream
  source lineage, and only after archive/catalog safety gates pass.

## Documentation Tasks

- Keep this file as the only `docs/active/*.md` sprint plan.
- Archive superseded active plans under
  `docs/archive/development/indexer/2026-06-25-superseded-active-plans/`.
- Update root docs so stale text cannot reintroduce old behavior:
  - remove claims that assemble filters or marks `article_headers.assembled_at`;
  - state that queue rows and `binary_parts` relationships are authoritative;
  - mark yEnc retry/backoff in ingest payloads as transitional debt only;
  - point readers to the canonical reference for ownership decisions.

## Query Guardrails

Add automated tests that fail when hot store code violates ownership rules:

- assemble store code must not contain `UPDATE article_headers`,
  `article_headers SET`, `assembled_at`, or `assembly_claimed_until`;
- assemble claim selection must use `article_header_assembly_queue` and
  `binary_completion_keys`, not article-header progress columns;
- binary part upsert must carry `source_posted_at` from the claimed queue/header
  and must delete completed assembly queue rows by
  `(source_posted_at, article_header_id)`;
- yEnc recovery must not write retry/progress state into `article_headers`;
- any remaining yEnc writes to `article_header_ingest_payloads` must be covered
  by a failing TODO test or disabled before this sprint is signed off;
- release formation must not mutate assemble-owned source/binary projections
  except documented release ack/queue state.

Any hot query changed in this sprint must have a focused test and a short
`EXPLAIN (ANALYZE, BUFFERS)` note recorded in the sprint signoff or PR notes.

## Assemble Stabilization Tasks

- Restore Lane A/B behavior using queue-owned state:
  - Lane A targets structured queue rows that can extend existing incomplete
    binaries via `binary_completion_keys`;
  - Lane B pulls recent general queue rows for fresh binary creation;
  - combined mode preserves configured Lane A target and Lane B minimum.
- Claim only queue keys inside the claim transaction.
- Hydrate article facts only after claiming exact
  `(source_posted_at, article_header_id)` keys.
- Remove or quarantine unused legacy helpers that still select from
  `article_headers.assembled_at`.
- Refresh binary stats with partition-key joins:
  `binary_parts.source_posted_at` plus `binary_parts.article_header_id`.
- Keep scrape blocked while the assemble queue is above configured backlog
  limits; do not let `scrape_latest` trickle into a saturated assemble queue.

## Partition And Retention Design

Use daily UTC range partitions keyed by `source_posted_at` only. Do not
subpartition by provider or newsgroup in this sprint. Provider/newsgroup
control belongs in indexed predicates, runtime group profiles, deferred ranges,
and explicit admin purge workflows.

Keep durable roots/catalog unpartitioned:

- `binary_core`
- `releases`
- `release_files`
- `release_catalog_files`
- `release_newsgroups`
- `release_archive_state`
- `release_archive_detail_*`
- `release_archive_lineage_*`
- `nzb_cache`
- enrichment and override tables

Complete partitioning for high-volume source/work/projection tables:

- source/header lineage: `article_headers`,
  `article_header_ingest_payloads`, `article_header_crosspost_groups`,
  `article_header_poster_refs`, `article_header_assembly_queue`,
  `poster_materialization_queue`;
- binary work/projection: `binary_parts`, `binary_observation_stats`,
  `binary_identity_current`, `binary_recovery_current`, `binary_lifecycle`,
  `binary_completion_keys`, `binary_grouping_evidence`,
  `binary_projection_events`, `binary_superseded_sources`;
- yEnc/inspect work and evidence: `yenc_recovery_work_items`,
  `binary_inspection_ready_queue`, `binary_inspections`,
  `binary_inspection_artifacts`, `binary_archive_entries`,
  `binary_text_evidence`, `binary_media_streams`, `binary_par2_sets`,
  `binary_par2_targets`;
- release-derived work: `release_family_readiness_summaries`,
  `release_ready_candidates`, `release_recovered_file_set_candidates`,
  `release_stage_dirty_families`.

Retention drop order:

1. ready/work queues;
2. inspect/yEnc evidence;
3. binary projections/work;
4. `binary_parts`;
5. article support rows;
6. `article_headers`;
7. prune unreferenced old `binary_core` roots after archive/catalog gates.

Retention must refuse to drop a day when there are running claims, active
source work, non-terminal release/archive dependencies, or default partition
rows that would make the drop incomplete.

## yEnc Recovery Boundary Tasks

- Keep yEnc admission limits and deferred range behavior from the existing
  runtime-settings implementation.
- Ensure work-item upserts are idempotent on partition-key-inclusive
  `binary_id` and `article_header_id` uniqueness.
- Move retry/backoff/progress state out of scrape-owned ingest payload rows
  into recovery-owned work/evidence state, or block signoff with an explicit
  regression test documenting the remaining debt.
- Maintain recovered identity merge semantics so recovered filenames merge
  fragments into file-level binaries instead of leaving one binary per article.

## Acceptance Criteria

- This file is the only active sprint plan.
- Root indexer docs no longer contradict the canonical ownership reference.
- Guardrail tests fail on forbidden write-backs.
- Fresh database migrations create all required daily partition parents and
  today/future partitions.
- Assemble drains queue batches without writing to `article_headers`.
- `recover_yenc` consumes ready work and records recovery evidence without
  creating unbounded backlog.
- Release summary refresh and release formation process newly assembled work.
- Partition retention dry-run reports eligible partitions, blockers, and drop
  order without broad unbounded source/work scans.
- Focused Go tests and `git diff --check` pass before signoff.
