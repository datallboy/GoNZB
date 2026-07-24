# Indexer Supervisor Soak Audit

Status: completed on 2026-07-24; see `INDEXER_SUPERVISOR_SOAK_FINDINGS.md`
Branch: `audit/indexer-sustained-workload`

## Purpose

Run a clean, sustained, production-shaped indexer workload against:

- `alt.binaries.sleazemovies`
- `alt.binaries.multimedia.rail`

The audit must measure the complete supervised pipeline, trace every stage through
its service and PostgreSQL store calls, inspect representative query plans, and
verify that binary grouping, release formation, NZB generation, inspection, and
yEnc recovery behave according to the maintained indexer wiki.

The first measurement interval is evidence gathering. Straightforward,
low-risk problems may be fixed during the audit after their before-state has
been captured. Each fix is validated and committed independently, then measured
again. Broad architectural or destructive changes remain findings for later
review.

## Sources Of Truth

The audit uses these maintained contracts:

- `docs/wiki/indexer/stage-flow.md`
- `docs/wiki/indexer/stage-ownership.md`
- `docs/wiki/indexer/schema-and-partitions.md`
- `docs/wiki/indexer/binary-grouping-evidence.md`
- `docs/wiki/indexer/yenc-recovery-queueing.md`
- `docs/wiki/indexer/release-formation.md`
- `docs/wiki/indexer/retention.md`
- `docs/wiki/indexer/operations-playbook.md`

Archived performance notes may supply regression candidates, but they are not
treated as current findings.

## Audit Questions

1. Can the normal supervisor sustain live ingest and downstream processing
   without deadlocks, persistent lock waits, long transactions, queue starvation,
   or unbounded lag?
2. Does every stage use bounded, partition-pruned, index-supported query shapes
   appropriate to its call frequency and batch size?
3. Do service loops avoid N+1 queries, excessive recounting, oversized in-memory
   batches, long-held transactions, and external work while database locks are
   held?
4. Are binary identities and part counts correct under multipart subjects,
   duplicates, crossposts, weak subjects, and posts spanning UTC days?
5. Do complete payload families become releases and produce valid, retrievable
   NZBs without requiring optional auxiliary files to be complete?
6. How much work enters yEnc recovery, why does it enter, and how much useful
   identity or grouping evidence does recovery produce?
7. Are current indexes justified by observed query plans and write cost? Are
   missing, redundant, or unused indexes visible after a representative run?

## Isolation And Safety

The workload must not reuse, wipe, or alter the normal development database.

Create a dedicated PostgreSQL 17 container with:

- a new named volume;
- a database name containing `soak`, such as `gonzb_indexer_soak`;
- a localhost-only alternate port, such as `55434`;
- `POSTGRES_INITDB_ARGS=--data-checksums`;
- `pg_stat_statements`, `amcheck`, `pgstattuple`, and `pg_visibility`;
- `track_io_timing=on`, `track_wal_io_timing=on`, and `compute_query_id=on`;
- `log_lock_waits=on`, `deadlock_timeout=500ms`;
- `log_min_duration_statement=1000ms`;
- `log_temp_files=0` and `log_checkpoints=on`.

Before starting GoNZB, record proof that data checksums are enabled and that the
diagnostic extensions load successfully.

Use an isolated runtime root:

```text
.tmp/indexer-soak/<run-id>/
  config.yaml
  metadata/gonzb.db
  blobs/
  release-archive/
  logs/
  snapshots/
  explain/
  manifest/
```

`.tmp/` is ignored by Git. Raw logs, query parameters, article subjects,
Message-IDs, provider credentials, and database dumps stay there and are never
committed.

If existing NNTP credentials are reused, first stop the normal GoNZB process and
copy the SQLite settings database with SQLite's backup operation into the
isolated runtime root. Do not copy a live SQLite database file directly. Edit
only the isolated copy through the API or Web UI.

The isolated configuration should:

- enable the usenet indexer, aggregator, API, and Web UI;
- disable the downloader and GoNZBNet modules for attributable measurements;
- use an unused HTTP port if another development service is running;
- point every mutable store/blob/archive/log path at the run directory;
- point PostgreSQL only at the dedicated soak database.

## Suspected Memory Fault Protocol

The audit cannot make unreliable RAM safe. PostgreSQL data checksums detect a
damaged page when it is read from storage, and `amcheck` detects many structural
inconsistencies, but neither can prove that arbitrary in-memory corruption did
not occur. A page corrupted in RAM before its checksum is calculated may be
written with a matching checksum. ECC memory or a successful offline memory test
is the real hardware-level control.

The audit therefore treats memory corruption as a containment problem:

- use a disk-backed PostgreSQL volume, never tmpfs, for the soak database;
- write telemetry as append-only files directly under the run directory rather
  than retaining the only copy in process memory;
- flush each checkpoint to disk and write an atomic completion marker after it;
- keep bounded telemetry batches so an application crash loses at most the
  current sample;
- record host OOM, EDAC/MCE, machine-check, filesystem, and storage messages from
  the system journal when the host exposes them;
- continuously watch PostgreSQL and application logs for `XX001`, invalid-page,
  checksum, panic, out-of-memory, and unexpected-process-exit signals;
- run lightweight online `pg_amcheck` checks at checkpoints, with strict time and
  load bounds, then run complete `pg_amcheck` and checksum verification after
  PostgreSQL is stopped;
- take a disk-backed database snapshot only at a verified-clean checkpoint and
  keep its manifest/checksum separate from later run state;
- never copy a suspect cluster forward or attempt to repair an invalid page in
  place for the sake of continuing the audit.

At stage boundaries, impossible in-memory state must not be committed merely to
keep the soak running. When durable source evidence already exists:

1. validate structural invariants before writing derived state;
2. roll back the current transaction on an invariant violation;
3. discard the entire claimed in-memory batch;
4. reread the source rows from PostgreSQL and retry once with a new batch;
5. record both observations and their stable identifiers/hashes;
6. quarantine the work item and raise the corruption stop signal if the
   violation repeats or the durable source also fails validation.

Examples include invalid part ordinals, `observed_parts > total_parts`, a
Message-ID changing within one claimed unit of work, broken source-day identity,
or release/NZB counts that disagree with their durable component rows. For an
NNTP response that has not yet been persisted, discard it and refetch within the
normal retry limits.

This reread path protects against committing a transient damaged batch and helps
localize the failure. It does not certify the reread as correct: PostgreSQL
caches and disk reads both use RAM, so a repeated or database-level inconsistency
still quarantines the run.

If corruption is detected, stop writers, preserve the corrupt run as evidence,
mark all results after the last verified-clean checkpoint untrusted, and
quarantine the cluster. Continue without asking for routine direction by
creating a new disposable checksummed cluster from the schema/configuration (or
the last independently verified clean snapshot), then resume at a new run ID.
Do not restore raw database pages from a snapshot whose integrity was not
verified.

Lowering PostgreSQL cache sizes or forcing more reads from disk is not considered
a correction for bad RAM: disk-backed pages must still pass through memory.
Likewise, repeated query results alone are not sufficient corruption detection.

## Workload Configuration

Configure exactly the two requested active groups and verify the saved runtime
settings after restarting:

```text
alt.binaries.sleazemovies
alt.binaries.multimedia.rail
```

For the baseline:

- enable latest scraping and deferred-range recovery;
- disable backfill and historical timeframe jobs;
- begin at the normal latest-scrape boundary for a fresh group;
- run the normal built-in supervisor;
- enable every locally applicable downstream stage;
- enable PreDB enrichment when its provider is configured;
- enable TMDB enrichment only when valid credentials are available;
- keep destructive source purge, emergency reset, retention drop, and default
  rehome work out of the evidence window;
- do not change stage concurrency, batches, delays, SQL, or indexes during an
  active measurement interval.

The repository's equivalent of `go run main.go` is:

```bash
go run ./cmd/gonzb --config .tmp/indexer-soak/<run-id>/config.yaml \
  serve --disable-release-purge-archived-sources
```

Do not pass `--no-indexer-supervisor`.

The purge override preserves archived release sources for correctness sampling
while leaving the normal supervisor and the rest of the stage graph active.

## Duration And Checkpoints

The intended certification run is:

1. a 15-minute warm-up;
2. at least four uninterrupted hours after warm-up;
3. checkpoints at 15 minutes, 1 hour, 2 hours, and 4 hours;
4. an extension to 8-12 hours if queues have not reached a stable cycle or no
   complete release has reached NZB/archive/inspection stages.

Record exact start/stop times, commit SHA, Go version, PostgreSQL version,
container limits, host CPU/RAM/storage, NNTP provider connection settings with
secrets redacted, and all runtime stage tuning.

If instability prevents a four-hour run, retain every completed checkpoint and
report the longest verified-clean interval. Shorter runs can still establish
query shapes and expose correctness or locking failures, but must not be
described as sustained-workload certification.

The completed audit used multiple clean, labeled measurement intervals because
evidence-backed fixes were applied between runs. It is a functional, query-shape,
and integrity audit, not a four-hour endurance or hardware certification. The
achieved intervals and limitations are recorded in the findings document.

## Repair Policy During The Audit

Use best judgment to fix a finding without interrupting the audit for routine
questions when the change is:

- directly supported by captured evidence;
- narrow, reversible, and covered by focused tests;
- consistent with the maintained indexer contracts;
- unlikely to change user-visible grouping, retention, release, or security
  policy;
- independently committable and measurable before and after.

Examples include a clearly missing bounded-query index, an incorrect predicate
that prevents partition pruning, an accidental N+1 call, an unbounded batch, a
needlessly long transaction, incorrect retry/backoff behavior, or telemetry that
fails to persist useful evidence.

For each easy fix:

1. preserve the original query plan, runtime metrics, and reproduction;
2. stop the current measurement interval cleanly;
3. add a focused regression/integration test;
4. implement the smallest correction;
5. run focused tests and query guardrails;
6. capture the after-plan and repeat the affected workload;
7. commit the fix separately with its finding identifier;
8. start a new labeled measurement interval so pre-fix and post-fix evidence are
   never mixed.

Do not make an unproven index change solely because an index appears unused
during a short run. Do not automatically perform destructive migrations, alter
retention semantics, weaken correctness/security checks, or broadly redesign a
stage. Document those findings and recommended options for later implementation.

Correctness, corruption, or security issues may justify stopping the affected
workload immediately. The audit should preserve evidence and proceed with other
safe analysis or a fresh disposable run rather than waiting for routine user
input.

## Complete Stage And Query Inventory

Before judging performance, build a traceable inventory with one row per store
operation:

```text
supervisor stage
  -> stage runner
  -> service method
  -> pgindex Store method
  -> SQL statement/queryid
  -> tables/indexes/partitions
  -> call frequency and batch size
  -> transaction and lock behavior
  -> observed plan and runtime
```

The inventory covers the following supervised work.

| Area | Stages/tasks | Primary store surfaces |
| --- | --- | --- |
| Scrape | `scrape_latest`, `scrape_backfill`, `scrape_timeframe`, `scrape_deferred` | `scrape_boundary_store.go`, `scrape_timeframe_store.go`, `work_window_control_store.go`, `partition_provision.go` |
| Materialize | `poster_materialize`, `crosspost_popularity_refresh`, `article_cohort_schedule` | `scrape_materializer_store.go`, `article_ingest_metadata.go`, `crosspost_store.go`, `article_cohort_scheduler_store.go` |
| Assemble | `assemble` | `assembly_store.go`, `subject_multipart_regroup_store.go`, `group_profile_scoring_store.go` |
| Recover | `recover_yenc` | `yenc_admission_store.go`, `yenc_work_item_store.go`, `yenc_recovery_store.go`, `binary_recovery_store.go` |
| Release | `release_summary_refresh`, `release` | `release_family_summary_store.go`, `release_ready_policy.go`, `release_store.go` |
| NZB | `release_generate_nzb`, `release_archive_nzb` | `release_generate_store.go`, `archive_store.go`, `archive_assets.go`, `release_catalog_files.go` |
| Inspect | `inspect_discovery`, `inspect_par2`, `inspect_nfo`, `inspect_archive`, `inspect_password`, `inspect_media` | `inspect_reads.go`, `inspect_ready_queue_store.go`, `inspection_store.go` |
| Enrich | `enrich_predb`, `enrich_tmdb` | `enrichment_store.go`, release category/title stores |
| Runtime | stage history, NNTP snapshots, provider inventory, outcome reconcile | `stage_runtime_store.go`, `nntp_runtime_store.go`, `provider_group_inventory_store.go`, `outcome_retention_store.go` |
| Maintenance | stats, queue cleanup, group profiles, retention/reclaim, partition work | `maintenance_store.go`, `maintenance_tasks_store.go`, `storage_reclaim.go`, `partition_provision.go`, `group_profile_scoring_store.go` |
| Read paths | dashboard, release catalog/detail, Newznab/NZB retrieval | `catalog_reads.go`, `public_release_reads.go`, `release_generate_store.go`, `work_window_control_reads.go` |

Disabled producer stages are still reviewed statically and with representative
queries on the captured database. Destructive maintenance is never invoked on
the live evidence database solely to obtain a plan.

For each service implementation, review:

- transaction scope and whether locks span NNTP, filesystem, `ffprobe`, archive,
  or other external calls;
- batch size, pagination, ordering, and retry/backoff behavior;
- queries issued per claimed item and possible N+1 patterns;
- repeated exact counts or full refreshes in hot loops;
- unbounded result sets and large allocations;
- worker concurrency versus database pool and NNTP connection limits;
- cancellation and claim lease behavior after error or shutdown;
- progress semantics when one group or item repeatedly fails.

## Live PostgreSQL Evidence

Reset `pg_stat_statements` immediately before warm-up. Snapshot at least every
60 seconds:

- `pg_stat_activity`, transaction age, state, wait event, and query age;
- `pg_locks` plus blockers from `pg_blocking_pids`;
- `pg_stat_statements`, including calls, total/mean/max/stddev time, rows,
  shared/local/temp blocks, I/O time, and WAL;
- `pg_stat_database`, including deadlocks, conflicts, temp files/bytes, block
  read time, and block write time;
- `pg_stat_io`, `pg_stat_wal`, checkpoint, and background writer statistics;
- `pg_stat_user_tables` and `pg_stat_user_indexes`;
- relation and partition sizes;
- live/dead tuples, analyze/vacuum timestamps, and default partition row counts;
- stage-run duration/outcome, queue depth, oldest-item age, claim rate, completion
  rate, and retry rate.

Also capture PostgreSQL logs, application structured logs, process CPU/RSS/file
descriptors, container CPU/memory/block I/O, volume free space, and NNTP
connection/command throughput. Persist samples incrementally to disk; the
collector must not depend on a large in-memory buffer surviving until shutdown.

Checkpoint snapshots must not contain full SQL parameters or credentials.

## EXPLAIN / ANALYZE Rules

`EXPLAIN ANALYZE` executes the statement. Use it safely:

1. On the running workload, use
   `EXPLAIN (ANALYZE, BUFFERS, VERBOSE, SETTINGS, FORMAT JSON)` only for
   bounded, read-only selectors and counters.
2. Set a local `statement_timeout` and `lock_timeout` before every manual plan.
3. Never analyze destructive cleanup, DDL, or an unbounded selector on the live
   evidence database.
4. Stop the supervisor and take a consistent clone/snapshot for mutating claims,
   upserts, refreshes, requeues, cleanup, and partition maintenance.
5. On the clone, wrap reversible DML in `BEGIN`, apply representative parameters,
   collect `ANALYZE, BUFFERS, WAL`, then `ROLLBACK`.
6. Capture both the normalized query ID and its source Store method. Save JSON
   plans under `.tmp/indexer-soak/<run-id>/explain/`.
7. Compare a warm plan and, where cache sensitivity matters, an explicitly
   identified cold-ish plan. Do not restart shared infrastructure merely to
   manufacture a cold cache during the live baseline.
8. Do not enable `auto_explain` with `ANALYZE` during the baseline. It can be
   used later for one targeted reproduction if statement-to-source mapping
   remains unresolved.

Plans are reviewed for:

- correct daily partition pruning;
- joins that include `source_posted_at`;
- bounded index scans matching queue ordering and claim predicates;
- unexpected sequential scans of large hot relations;
- high rows removed by filter or excessive buffers per returned row;
- sorts/hashes spilling to disk;
- nested loops with unexpectedly high inner-loop counts;
- cardinality estimate errors;
- repeated aggregate/count work;
- unnecessary row locks, broad lock scope, or lock-order inversions;
- WAL amplification and write churn caused by hot-row updates;
- unused, overlapping, or write-expensive indexes.

Index changes are not inferred from `pg_stat_user_indexes.idx_scan = 0` alone.
Constraint support, rare operational paths, partition age, and observation
duration must also be considered.

## Binary Correctness Checks

Sample both aggregate metrics and raw evidence chains:

```text
overview Subject/Message-ID/groups
  -> ingest metadata
  -> binary identity/core
  -> logical binary parts
  -> binary stats/readiness
  -> release family/file
```

Verify:

- `(part/total)` determines binary segment identity;
- `[file_index/file_total]` remains file-set identity and is never counted as a
  binary segment number;
- duplicate overview rows and crossposts do not inflate observed parts;
- `observed_parts <= total_parts`;
- one Message-ID contributes at most one logical part;
- crosspost alternatives are retained without becoming duplicate NZB segments;
- strong canonical multipart identity groups matching files despite poster/time
  variance where the contract permits it;
- strong identities may merge across UTC source partitions;
- weak/random subjects are not promoted as complete without recovered evidence;
- auxiliary files do not inflate main-payload completeness;
- samples show neither systematic oversplitting nor overgrouping.

Report results by newsgroup, family kind, identity reason, readiness state, and
source day.

## Release And NZB Correctness Checks

Verify complete release paths end to end:

- a complete main payload and strong identity can create an internal release;
- incomplete optional auxiliary files do not block internal formation;
- public readiness remains a separate policy decision;
- expected file counts and family summaries match release files;
- release article counts represent unique logical parts;
- generated NZBs contain each logical segment once;
- NZB groups preserve observed crosspost alternatives;
- archived NZB state and catalog files match the release;
- discovery/PAR2/NFO/archive/password/media inspection advances or records a
  specific terminal/retry outcome;
- a public-ready release is visible through the aggregator and Newznab API;
- its NZB can be fetched and parsed without missing or duplicate segments.

If no release completes in four hours, extend the run before declaring release
formation broken. Separately report whether the cause is insufficient incoming
data, incomplete payloads, queue lag, policy, inspection, or a software error.

## yEnc Recovery Evaluation

Capture, per group and priority lane:

- eligible headers/binaries;
- admitted work items and percentage of assembled main-payload binaries;
- admission reason and priority rank;
- queue depth, oldest age, selection rate, completion rate, retry rate;
- `identity_recovered`, `merged`, `no_op`, `not_found`, transient failure, and
  terminal failure outcomes;
- recovered identity/grouping yield by admission reason;
- HEAD-complete subject entries that were nevertheless queued;
- impact on binary completion and release formation.

Use measured percentages instead of labels such as "a lot." Initial review
triggers are:

- more than 25% of assembled main-payload binaries require yEnc recovery; or
- more than 50% of a sufficiently large admitted sample produces neither useful
  identity nor a merge.

These are investigation triggers, not automatic failures. Group content and
provider behavior must be included in the interpretation.

## Finding Thresholds

Record a review finding for:

- any PostgreSQL corruption, checksum failure, amcheck failure, or `XX001`;
- any deadlock or SQLSTATE `40P01`;
- repeated lock waits or a lock wait over one second;
- steady-state statements over one second, maxima over five seconds, or a
  high-cumulative-time statement even when individual calls are fast;
- an idle-in-transaction session or unexpectedly long transaction;
- temp-file spill in a normal stage query;
- missing partition pruning or writes to a default partition;
- a large sequential scan where a bounded queue/index access is expected;
- high rows removed by filter, poor estimates, or excessive buffers/WAL;
- sustained queue growth, oldest-age growth, starvation, or zero useful
  throughput;
- repeated retry loops without durable progress;
- autovacuum/analyze falling materially behind write churn;
- unexplained binary count inflation, grouping errors, or missing releases/NZBs.

Severity:

- **Blocker:** corruption, data loss, invalid release/NZB data, or pipeline-wide
  deadlock/stall.
- **High:** repeatable query/lock behavior that prevents sustained indexing or
  causes unbounded backlog.
- **Medium:** material inefficiency or correctness edge case with a bounded
  operational workaround.
- **Low:** measurable cleanup or tuning opportunity without current workload
  impact.

## Stop Conditions

Stop the live workload without attempting a fix if any of these occur:

- checksum/corruption errors, failed structural checks, or storage I/O errors;
- repeated deadlocks or a pipeline-wide stall;
- unexpected writes into a default partition;
- free disk reaches the configured safety reserve;
- uncontrolled memory, WAL, temp-file, or log growth;
- NNTP provider quota, connection, or abuse limits are at risk;
- the isolated configuration is discovered to point at a non-soak database or
  normal runtime storage.

Preserve logs and database state before investigating.

Stopping an affected run does not stop the overall audit. Apply the repair policy
for a software problem, or the suspected-memory protocol for corruption, then
continue with a new labeled interval or disposable cluster when it is safe.

## Deliverables

The execution produces:

1. `.tmp/indexer-soak/<run-id>/manifest/` with environment, configuration, commit,
   checksums, timings, and redacted runtime settings;
2. a stage/service/Store/SQL inventory with source locations and query IDs;
3. time-series snapshots, top-query reports, lock graphs, and JSON plans;
4. binary, release, NZB, inspection, and yEnc correctness samples;
5. `docs/active/INDEXER_SUPERVISOR_SOAK_FINDINGS.md` containing:
   - workload summary and achieved throughput;
   - per-stage latency/throughput/lag;
   - query and lock findings with direct evidence;
   - correctness results for each group;
   - yEnc admission and yield;
   - verified-clean interval lengths and any corruption quarantine events;
   - repairs made during the audit with before/after evidence and commit IDs;
   - index usage/maintenance observations;
   - prioritized unresolved findings;
6. a reviewed follow-up implementation plan split into incremental commits.

Each finding must name the stage, service method, Store method, SQL query ID,
source file, observed plan/runtime, production consequence, and proposed options.

## Execution Order

1. Record the clean branch/commit and create the isolated run directory.
2. Start and verify the checksummed diagnostic PostgreSQL database.
3. Create the isolated application configuration and settings backup.
4. Configure exactly the two groups and verify applicable stage settings.
5. Capture schema/index inventory and reset statistics.
6. Start normal `serve` mode with the built-in supervisor.
7. Collect warm-up and sustained workload evidence at defined checkpoints.
8. Stop the supervisor cleanly and preserve the live database.
9. Run integrity checks and create the plan-analysis clone.
10. Complete safe `EXPLAIN ANALYZE` coverage for every Store query.
11. Validate binary grouping, releases, NZBs, inspections, and yEnc outcomes.
12. Apply and independently commit narrow evidence-backed fixes as they are
    found, repeating the affected measurements.
13. Write the findings document with completed fixes and unresolved decisions.
