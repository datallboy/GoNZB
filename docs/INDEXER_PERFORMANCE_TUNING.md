# Indexer Performance Tuning

This document records the pre-v0.8.0 indexer performance audit process, live baseline observations, and follow-up tuning guidance. The baseline is measurement-only: do not change runtime batch sizes, concurrency, retention, indexes, or PostgreSQL cost settings during the soak unless a crash or deadlock blocks the audit.

## Baseline Capture

Audit target:

- Database: local live PostgreSQL `gonzb` database from `config.yaml`
- Service: `go run cmd/gonzb/main.go serve --config config.yaml`
- Capture date: 2026-06-22 America/New_York
- Instrumentation reset: `2026-06-22 10:07:42-04`
- Corrected soak window: snapshot `0` at `2026-06-22 10:08:06-04` through snapshot `12` at `2026-06-22 11:17:03-04`
- Soak log: `.tmp/perf-audit/pre-v0.8.0-soak-20260622-100806.log`
- EXPLAIN log: `.tmp/perf-audit/pre-v0.8.0-explain-20260622-100623.log`

Instrumentation state:

- `shared_preload_libraries=pg_stat_statements`
- `track_io_timing=on`
- `pg_stat_statements` extension installed
- `SELECT pg_stat_statements_reset()` run immediately before the corrected soak window

The compose-only instrumentation change is in `docker-compose.postgres.yml`. It does not tune workload behavior.

## Methodology

1. Enable `pg_stat_statements` through PostgreSQL `shared_preload_libraries`.
2. Restart PostgreSQL and `serve`.
3. Run `CREATE EXTENSION IF NOT EXISTS pg_stat_statements`.
4. Reset statement stats and record the timestamp.
5. Run the service under the current runtime configuration.
6. Capture snapshots every five minutes for one hour:
   - exact backlog counts, including values that the dashboard caps at `1000`
   - `indexer_stage_runs` stage durations and primary item counts
   - `pg_stat_statements` ordered by total execution time
   - `pg_stat_activity` state and wait events
   - table sizes, index sizes, dead tuples, and autovacuum/analyze timestamps
   - NNTP runtime snapshots where present
7. Run targeted `EXPLAIN (ANALYZE, BUFFERS, WAL, VERBOSE, SETTINGS)` on safe read-only selectors and counters. Mutating query paths should be explained inside `BEGIN; ... ROLLBACK;` only when the write is safe to replay.

## Exact Backlog Definitions

Use exact SQL counts for audit reporting and admin dashboard cache refreshes. Dashboard reads should use `indexer_dashboard_stats`; the `maintenance.dashboard_stats_refresh` maintenance task owns periodic exact-count refreshes.

| Backlog | Exact definition |
| --- | --- |
| Unassembled headers | `article_header_assembly_queue` rows where `claim_until IS NULL OR claim_until < now()` |
| Claimed assembly headers | `article_header_assembly_queue` rows where `claim_until >= now()` |
| Release summary refresh | `count(*)` from `release_family_summary_refresh_queue` |
| yEnc ready | `yenc_recovery_work_items` rows where `status='ready' AND ready_at <= now()` and `message_id` is non-empty |
| yEnc running | `yenc_recovery_work_items` rows where `status='running'` |
| Inspect backlog | exact stage-specific claimable candidate count using the same inspection selection predicates as the stage |
| Archive waiting for purge | exact `release_archive_state` counts by archive status |

## Live Baseline Results

The corrected soak captured 13 snapshots. The wall-clock sample span was about 69 minutes because exact yEnc-ready counts and size/dead-tuple snapshots add visible overhead to each five-minute sample. That overhead is itself a result: exact counts on the largest hot queues should be refreshed on an admin-cache cadence, not during ordinary dashboard GET requests.

### Backlog Delta

| Backlog | Start exact count | End exact count | Delta | Observed rate |
| --- | ---: | ---: | ---: | ---: |
| yEnc ready | 18,249,458 | 19,615,184 | +1,365,726 | growing by about 19,800/min |
| Unassembled headers | 343,048 | 335,595 | -7,453 | draining by about 108/min |
| Claimed assembly headers | 20,153 | 21,057 | +904 | roughly flat |
| Release summary refresh queue | 0 | 0 | 0 | drained during window |
| Inspect pending rows | 0 for archive/discovery/media/par2 | 0 for archive/discovery/media/par2 | 0 | no exact pending backlog in `binary_inspections` |

The largest exact backlog is `yenc_ready`, not the dashboard-capped `1000+` value.

### Stage Throughput

| Stage | Completed runs | Primary items | Average run | Notes |
| --- | ---: | ---: | ---: | --- |
| `assemble` | 96 | 1,920,000 headers | 43.25 s | about 452 headers/s over the soak wall clock; DB work dominates |
| `release_summary_refresh` | 347 | 402,235 summaries | 2.15 s | kept queue drained but tracks assemble dirty-key output |
| `release` | 76 | not emitted in generic metrics | 53.91 s | frequent long runs; top statement time points here |
| `poster_materialize` | 140 | 1,400,000 claims | 9.89 s | significant supporting write load |
| `recover_yenc` | 354 | 2,614 attempted | 0.71 s | severe under-utilization of configured concurrency |
| `inspect_discovery` | 11 | 11,000 candidates | 325.02 s | external/NNTP-heavy; one run exceeded 25 minutes |
| `inspect_par2` | 349 | 17 processed | 0.42 s | candidate selection still costs DB time |
| `inspect_archive` | 349 | 2 processed | 0.23 s | no material backlog |
| `inspect_media` | 349 | 0 processed | 0.25 s | no material backlog |
| `scrape_latest` | 30 | articles inserted in per-run JSON | 11.38 s | scrape was gated at times by assemble backlog |
| `scrape_backfill` | 17 | articles inserted in per-run JSON | 26.52 s | gated at times by assemble backlog |

### Statement Hotspots

Top `pg_stat_statements` findings from the corrected soak:

| Query family | Total time | Calls | Interpretation |
| --- | ---: | ---: | --- |
| Release formation binary-family load from `binary_identity_current` | about 3,561,892 ms | 1,707 | largest DB cost by total time; release formation is a major current pressure point |
| Scrape article-header upsert | about 1,087,590 ms | 3,871 | large ingest volume; expected during active scrape |
| Poster ref upsert | about 599,131 ms | 560 | supporting write amplification from scrape/poster materialization |
| Release catalog file sync | about 560,172 ms | 1,888 | release-side catalog maintenance is significant |
| Assemble Lane A/Lane B claim query | about 133,812 ms | 97 | `binary_completion_keys` broad ordered scan remains expensive |
| Inspect PAR2 candidate source | about 111,645 ms | 349 | repeated candidate scans with little processed work |

### Recover yEnc Detail

`recover_yenc` is not currently limited by raw NNTP concurrency. During the corrected window:

- configured concurrency in run metrics: `100`
- completed runs: `354`
- zero-candidate runs: `222`
- nonzero-candidate runs: `132`
- candidates/attempted: `2,614`
- recovered: `1,364`
- noops: `1,250`
- max effective concurrency: `42`
- average nonzero effective concurrency: about `19.8`

Specific evidence: run `58103` at `2026-06-22 10:41:26-04` selected a five-minute fairness bucket (`2026-06-22T12:10:00Z` to `2026-06-22T12:15:00Z`), attempted `19`, produced `19` noops and `0` recovered, and only reached effective concurrency `19`. That was a wasted recovery pass relative to the configured 100-connection intent.

### Post-Audit yEnc Recovery Fixes

Follow-up changes after the baseline addressed the two dominant `recover_yenc` issues found by the audit:

- Windowed/fairness selection now walks backward across adjacent windows until the requested batch is filled, and a zero-percent lane is skipped instead of queried.
- Recovered yEnc writes now stream in 250-row flushes while NNTP processing continues.
- `recover_yenc` no longer deletes merged source `binary_core` rows inline. It marks source rows superseded and records source-to-target lineage in `binary_superseded_sources`; purge remains the only terminal source-delete owner.
- Within each flush, repeated target binary identity/stat/recovery updates are deduplicated by target binary.
- Superseded-source bridge and lifecycle marker writes are batched per flush.

Short live validation after these changes:

| Run | Selected | Recovered | Merged | Write time | Main write notes |
| --- | ---: | ---: | ---: | ---: | --- |
| pre-delete-fix baseline run `65623` | 5,000 | 5,000 | 4,988 | about `130.1 s` | `source_delete_ms` about `103.5 s` |
| after purge-owned supersede marker | 5,000 | 5,000 | 4,993 | about `22.9 s` | `source_delete_ms=0`, `source_supersede_ms` about `2.7 s` |
| after target-update dedupe | 5,000 | 5,000 | 4,989 | about `11.9 s` | target update time fell from about `9.6 s` to about `0.24 s` |
| after batched supersede markers | 5,000 | 5,000 | 4,991 | about `9.0 s` | `source_supersede_ms` about `0.30 s` |

The current post-fix yEnc pressure is no longer the original selection underfill or source-delete path. Further `recover_yenc` optimization should be data-driven after a longer supervisor soak; the remaining write cost is mostly per-source part movement, work-item completion, payload updates, and stats/summary follow-up.

## Stage Query Inventory

### Scrape Latest And Backfill

Primary paths:

- NNTP range fetches by provider/newsgroup checkpoint.
- Header upserts into `article_headers`.
- Payload upserts into `article_header_ingest_payloads`.
- Assembly queue inserts into `article_header_assembly_queue`.
- Poster and cross-post queue inserts into `poster_materialization_queue` and `crosspost_popularity_refresh_queue`.
- Checkpoint updates in `scrape_checkpoints` and run history in `scrape_runs`.

Tables read:

- `newsgroups`
- `scrape_checkpoints`
- `usenet_providers`
- provider inventory tables

Tables written:

- `article_headers`
- `article_header_ingest_payloads`
- `article_header_assembly_queue`
- `poster_materialization_queue`
- `crosspost_popularity_refresh_queue`
- `scrape_checkpoints`
- `scrape_runs`

Locking:

- Normal upsert conflicts on article identity and queue keys.
- Stage-level runtime lease through `indexer_stage_state`.
- Backlog guard can pause scrape when assemble falls behind.

Throughput controls:

- `indexing.scrape_latest.batch_size`
- `indexing.scrape_latest.concurrency`
- `indexing.scrape_backfill.batch_size`
- `indexing.scrape_backfill.concurrency`
- provider connection capacity and NNTP latency

Baseline assessment:

- Scrape was gated by assemble catch-up during the observed run, not by NNTP alone. Logs showed `scrape paused for assemble catch-up` when unassembled headers exceeded the resume threshold.

### Assemble

Primary paths:

- `ClaimAssemblyQueueBatch` selects Lane A candidates from `binary_completion_keys` and Lane B candidates from `article_header_assembly_queue`.
- Claimed headers are hydrated from `article_headers`, `article_header_ingest_payloads`, `newsgroups`, `article_header_poster_refs`, and `posters`.
- `UpsertBinaries` writes v2 binary projections.
- `UpsertBinaryParts` writes `binary_parts` and clears assembly queue rows for successfully assembled headers.
- `RefreshBinaryStatsBatch` updates `binary_observation_stats`, refreshes `binary_completion_keys`, marks release-summary keys dirty, and syncs `yenc_recovery_work_items`.

Tables read:

- `binary_completion_keys`
- `article_header_assembly_queue`
- `article_headers`
- `article_header_ingest_payloads`
- `newsgroups`
- `article_header_poster_refs`
- `posters`
- `binary_parts`

Tables written:

- `article_header_assembly_queue`
- `binary_core`
- `binary_identity_current`
- `binary_observation_stats`
- `binary_completion_keys`
- `binary_recovery_current`
- `binary_lifecycle`
- `binary_parts`
- `release_family_summary_refresh_queue`
- `yenc_recovery_work_items`
- legacy `binary_grouping_evidence` only when detailed evidence is explicitly retained

Locking:

- `pg_try_advisory_xact_lock(hashtext('gonzb-assemble-claim'))` serializes claim selection.
- Lane selectors use `FOR UPDATE SKIP LOCKED`.
- Binary-key advisory locks are used while upserting binary identity.
- Stat refresh locks `binary_observation_stats` rows with `FOR UPDATE OF bos`.

Throughput controls:

- assemble batch size from runtime settings
- `indexing.assemble.binary_upsert_db_chunk_size`
- Lane A/B percentages
- scrape backlog guard thresholds

Baseline assessment:

- Current pressure is assemble-heavy but not exclusively assemble-bound. Representative logs showed 20,000-header batches with total run times in the tens of seconds and binary upsert/query time dominating many runs.
- EXPLAIN showed the broad `binary_completion_keys` ordered selector returned 140,000 rows in about `4.15 s` with `30,564` shared blocks read. General Lane B queue selection for 20,000 rows was much cheaper at about `57.5 ms`.
- Follow-up analysis on `2026-06-22` found the active capped Lane A selector had shifted from expensive broad scans to ineffective selection: with a 2,000-row ranked completion-key window, it found zero claimable structured queue rows while recent assemble runs selected `0` Lane A and `20,000` Lane B rows. The live table shape was about `10.4M` `binary_completion_keys` rows and `2.63M` completion file keys versus about `202k` claimable queue rows and only `1,512` claimable structured queue file keys.
- Completed post-audit: Lane A now starts from claimable queue file keys, then chooses the best matching completion key per queue key. Supporting indexes `idx_binary_completion_keys_match_rank` and `idx_article_assembly_queue_structured_latest` match this queue-first shape. Live EXPLAIN with those indexes returned `1,460` Lane A candidates in about `180 ms` and used index-only scans for both the queue key scan and completion match/rank lookup.
- Completed post-audit: Lane A is now time-aware instead of capped by a fixed candidate window. Completed binary samples had posting spans of `p99 ~= 1.4 minutes`; near-complete binaries had `p99 ~= 2.5 minutes`; worst sampled spans were about `30 minutes`. The runtime setting `indexing.assemble.lane_a_time_window_minutes` defaults to `15` as a conservative window around the incomplete binary's observed `posted_at`. The selector now uses `binary_completion_keys.posted_at`, maintained from `binary_observation_stats`, so recovered yEnc `name=` evidence that updates binary file identity can participate in Lane A through the same completion-key projection.

### Poster Materialize And Crosspost Popularity

Primary paths:

- Poster materialization claims pending rows from `poster_materialization_queue` with `FOR UPDATE SKIP LOCKED`.
- Poster names are upserted into `posters`; references are written to `article_header_poster_refs`.
- Crosspost refresh claims `crosspost_popularity_refresh_queue` rows and refreshes `article_header_crosspost_group_summary`.

Tables read:

- `poster_materialization_queue`
- `article_header_crosspost_groups`
- `crosspost_popularity_refresh_queue`

Tables written:

- `posters`
- `article_header_poster_refs`
- `poster_materialization_queue`
- `article_header_crosspost_group_summary`
- `crosspost_popularity_refresh_queue`

Locking:

- Queue claims use `FOR UPDATE SKIP LOCKED`.
- Upserts contend on poster and summary keys when scrape is ingesting heavily.

Baseline assessment:

- Poster materialization appeared in top statement time during early capture because scrape was still inserting large header batches. It is supporting load, not the primary indexer blocker unless poster queue backlog persists.

### Release Summary Refresh

Primary paths:

- Hot and cold queue branches delete claimed keys from `release_family_summary_refresh_queue`.
- Summary refresh aggregates binary identity and observation state into `release_family_readiness_summaries`.
- Candidate sync writes `release_ready_candidates` and `release_recovered_file_set_candidates`.

Tables read:

- `release_family_summary_refresh_queue`
- `release_family_readiness_summaries`
- `binary_core`
- `binary_identity_current`
- `binary_observation_stats`
- `binary_recovery_current`
- `release_ready_candidate_acks`
- `release_recovered_file_set_candidate_acks`

Tables written:

- `release_family_summary_refresh_queue`
- `release_family_readiness_summaries`
- `release_ready_candidates`
- `release_recovered_file_set_candidates`

Locking:

- Queue dequeue branches use `FOR UPDATE OF q SKIP LOCKED` and delete by `ctid`.
- The stage is still serialized by the supervisor lease.

Throughput controls:

- `indexing.release_summary_refresh.batch_size`
- maximum batches and duration budget from runtime settings
- candidate backlog limits

Baseline assessment:

- Refresh can drain thousands of queued keys quickly when assemble marks many families dirty, but several runs hit the time budget. This is pressure coupled to assemble output.

### Release Formation And Reform

Primary paths:

- Release candidates are selected from readiness summaries and dirty families.
- Existing release snapshots are checked and locked by `(provider_id, group_name)`.
- Release rows are upserted; release files/newsgroups and NZB cache status are replaced.
- Reform uses the same persistence path on previously formed catalog state.

Tables read:

- `release_family_readiness_summaries`
- `release_ready_candidates`
- `release_recovered_file_set_candidates`
- `binary_core`
- `binary_identity_current`
- `binary_observation_stats`
- `binary_recovery_current`
- `binary_parts`
- existing `releases`

Tables written:

- `releases`
- `release_files`
- `release_newsgroups`
- `nzb_cache`
- `release_ready_candidate_acks`
- `release_recovered_file_set_candidate_acks`

Locking:

- Existing release snapshots use `FOR UPDATE`.
- Release formation is downstream of binary assembly and must not lock upstream binary rows while writing release-owned catalog state.

Throughput controls:

- `indexing.release.batch_size`
- `indexing.release.auto_reform_batch_size`
- readiness/public policy thresholds

Baseline assessment:

- Release formation was active and became the largest DB consumer by total statement time during the corrected soak. The hottest statement family loaded binary candidates from `binary_identity_current` by provider/family and consumed about 3,562 seconds across 1,707 calls. This indicates release formation query shape is at least as important as assemble for v0.8.0 performance follow-up.
- Follow-up EXPLAIN on `2026-06-22` found the release-family fan-out selector was not using the existing `(provider_id, release_family_key)` partial index unless the query explicitly included `BTRIM(release_family_key) <> ''`. For representative family `leif billy s05e02 1080p h264 havsorn`, the current selector read about `294k` shared blocks and took `1.74 s` for 15 rows. The semantically equivalent selector with the explicit non-empty predicate used `idx_binary_identity_release_family_provider`, read 10 shared blocks, and took `0.98 ms`. The full hydration query showed the same shift, from `1.69 s` and about `294k` block reads to `0.62 ms` and 24 block reads.
- Any release formation query change must preserve cross-newsgroup binary selection for release-family and recovered-file-set candidates, the auto-reform path for more complete binary sets, inspect-derived title metadata, and public status gating. The first fix was implemented after this analysis by exposing the existing non-empty release-family predicate to the planner and making release-family fan-out ignore the representative candidate newsgroup. No new index was required because the adjusted query uses `idx_binary_identity_release_family_provider`.

### Recover yEnc

Primary paths:

- Ready work is selected from `yenc_recovery_work_items`.
- Fairness state is locked in `yenc_recovery_fairness_state`.
- Prefix payloads are fetched over NNTP.
- Successful recovery updates binary identity/recovery projections and may merge duplicate binary rows.

Tables read:

- `yenc_recovery_work_items`
- `yenc_recovery_fairness_state`
- `binary_core`
- `binary_identity_current`
- `binary_observation_stats`
- `binary_parts`
- `article_headers`
- `article_header_ingest_payloads`

Tables written:

- `yenc_recovery_work_items`
- `yenc_recovery_fairness_state`
- `binary_core`
- `binary_identity_current`
- `binary_observation_stats`
- `binary_completion_keys`
- `binary_recovery_current`
- `binary_parts`
- `binary_lifecycle` superseded-source markers
- `binary_superseded_sources`
- `release_family_summary_refresh_queue`

Locking:

- Ready work selection uses `FOR UPDATE SKIP LOCKED`.
- Fairness state uses `FOR UPDATE`.
- Binary seed and target rows use `FOR UPDATE OF bc, bic` or `FOR UPDATE`.

Throughput controls:

- `indexing.recover_yenc.batch_size`
- `indexing.recover_yenc.concurrency`
- target-window and newest/fairness percentages
- NNTP provider capacity and prefix fetch latency

Baseline assessment:

- Exact ready backlog grew from about 18.25 million to 19.62 million during the corrected soak, but the stage selected zero candidates in 222 of 354 runs. The dominant blocker is not raw yEnc worker or NNTP capacity; selection/windowing is sending the stage into tiny fairness buckets or empty work, so the 100 configured workers are usually idle.
- Run `58103` is a concrete example: 19 candidates in a five-minute bucket, 19 noops, 0 recovered, and effective concurrency 19 despite configured concurrency 100.
- Post-audit fixes filled repeated 5,000-row runs, used configured concurrency, removed inline source deletes, and reduced 5,000-row write time to about `9 s`.
- EXPLAIN showed a naive exact count of ready yEnc rows took about `27.84 s` and touched `811,532` shared blocks read. Use exact counts sparingly in UI paths; keep them in audit/admin paths.

### Inspection Stages

Primary paths:

- `inspect_discovery`, `inspect_archive`, `inspect_media`, `inspect_par2`, and `inspect_password` use stage-specific candidate filters over binary/release state.
- Claims are written to `binary_inspections`.
- Materialization reads `binary_parts` and `article_headers`, fetches article bodies, and runs external tools where applicable.
- Results are written to stage-owned evidence tables.

Tables read:

- `binary_core`
- `binary_identity_current`
- `binary_observation_stats`
- `binary_recovery_current`
- `release_files`
- `release_catalog_files`
- `binary_parts`
- `article_headers`
- `binary_inspections`

Tables written:

- `binary_inspections`
- `binary_inspection_artifacts`
- `binary_archive_entries`
- `binary_media_streams`
- `binary_text_evidence`
- `binary_par2_sets`
- `binary_par2_targets`
- `binary_password_candidates`
- release rollup fields after stage completion

Locking:

- `pg_advisory_xact_lock(hashtext('gonzb-inspect-claim-' || stage_name))` serializes claims per inspection stage.
- Claim insert/update uses `FOR KEY SHARE OF bc` on binary rows.
- External tool time is outside PostgreSQL but holds stage capacity.

Throughput controls:

- per-stage batch size
- per-stage concurrency
- inspect max bytes and tool timeouts
- NNTP prefix/body fetch capacity

Baseline assessment:

- Archive and media had no pending exact backlog in the initial capture. `inspect_discovery` and `inspect_password` dashboard counters were capped at `1000`, so use exact candidate SQL or stage metrics to distinguish true backlog from cached caps.
- PAR2 candidate selection showed up in `pg_stat_statements` even with zero processed rows, so candidate query shape is worth monitoring.

### Release NZB Generation, Archive, And Purge

Primary paths:

- Generate/archive candidate selectors read public-ready releases with completed archive/media inspections.
- Candidate claims lock release rows with `FOR UPDATE OF r SKIP LOCKED`.
- Archive state is written to `release_archive_state`.
- Purge validates `release_archive_state`, durable catalog files, and completed media inspection before deleting source lineage.

Tables read:

- `releases`
- `release_files`
- `release_newsgroups`
- `nzb_cache`
- `release_overrides`
- `release_archive_state`
- `binary_inspections`
- `release_catalog_files`
- release archive lineage tables

Tables written:

- `nzb_cache`
- `release_archive_state`
- `release_archive_detail_snapshots`
- `release_archive_detail_files`
- `release_archive_detail_subtitle_languages`
- `release_archive_lineage_binaries`
- `release_archive_lineage_article_headers`
- source lineage tables during purge only

Locking:

- Generate/archive candidate selection uses `FOR UPDATE OF r SKIP LOCKED`.
- Purge locks archive state with `FOR UPDATE` and deletes in dependency-safe order.
- The supervisor should not overlap purge with active assemble writers.

Throughput controls:

- `indexing.release_generate_nzb.batch_size`
- `indexing.release_archive_nzb.batch_size`
- `indexing.release_purge_archived_sources.batch_size`
- blob backend latency and retention policy

Baseline assessment:

- Archive and generate stages had little active work in the observed window. Purge was disabled in server mode for this run, so purge throughput was not measured.

## Stage Table Touch Matrix

| Stage | Main reads | Main writes | Shared exclusion |
| --- | --- | --- | --- |
| `scrape_latest` / `scrape_backfill` | `scrape_checkpoints`, `newsgroups`, NNTP | `article_headers`, `article_header_ingest_payloads`, `article_header_assembly_queue`, poster/crosspost queues | stage lease, upsert conflicts |
| `assemble` | `article_header_assembly_queue`, `binary_completion_keys`, header payload tables | v2 binary projections, `binary_parts`, summary queue, yEnc work items | advisory claim lock, `SKIP LOCKED`, binary-key advisory locks |
| `poster_materialize` | `poster_materialization_queue` | `posters`, `article_header_poster_refs`, queue status | `SKIP LOCKED` |
| `crosspost_popularity_refresh` | `crosspost_popularity_refresh_queue`, crosspost groups | crosspost summaries | `SKIP LOCKED` |
| `release_summary_refresh` | summary queue, v2 binary projections, acks | readiness summaries, ready/recovered candidates | `FOR UPDATE OF q SKIP LOCKED` |
| `release` | readiness candidates, v2 binary projections | `releases`, `release_files`, `release_newsgroups`, `nzb_cache` | release row `FOR UPDATE` |
| `recover_yenc` | yEnc work items, binary/header state, NNTP | yEnc work items, v2 binary projections, `binary_parts`, superseded-source markers, summary queue | `SKIP LOCKED`, fairness `FOR UPDATE`, binary row locks |
| `inspect_discovery` | binary/release state, article bodies | `binary_inspections`, recovery/evidence updates | per-stage advisory claim lock |
| `inspect_archive` | binary/release state, article bodies | `binary_inspections`, archive evidence | per-stage advisory claim lock, external tools |
| `inspect_media` | binary/release state, article bodies | `binary_inspections`, media evidence | per-stage advisory claim lock, external tools |
| `inspect_par2` | release files, binary state, article bodies | `binary_inspections`, PAR2 sets/targets | per-stage advisory claim lock, external tools |
| `release_archive_nzb` | public-ready releases, NZB cache, inspections | archive state and blob-backed snapshots | release row `SKIP LOCKED` |
| purge/maintenance | archive state, lineage, runtime queues | source cleanup, runtime cleanup, history cleanup | archive-state locks, supervisor stage grouping |

## EXPLAIN Findings

Representative safe-read EXPLAIN results from `2026-06-22 10:06:23-04`:

| Query path | Result |
| --- | --- |
| Exact unassembled count | `200 ms`, parallel index-only scan on `idx_article_assembly_queue_claim`, `4,898` shared blocks read, `134,631` heap fetches |
| Release summary queue count | `1.65 ms`, sequential scan, queue nearly empty by execution time |
| Exact yEnc ready count | `27.84 s`, parallel index-only scan on `idx_yenc_recovery_work_items_ready`, `811,532` shared blocks read, `3,485,140` heap fetches |
| Inspect archive pending count | `0.064 ms`, index-only scan on `idx_binary_inspections_stage_status` |
| Broad assemble Lane A source | `4.15 s`, index scan on `idx_binary_completion_keys_rank`, `30,564` shared blocks read for 140,000 rows |
| General assembly Lane B source | `57.5 ms`, backward primary-key scan, 20,000 rows |
| yEnc small ready selector | `0.18 ms`, index scan on `idx_yenc_recovery_work_items_ready_order`, 25 rows |
| Release-family binary fan-out selector | current shape `1.74 s`, `293,953` shared blocks read; with explicit non-empty release-family predicate `0.98 ms`, 10 shared blocks read, using `idx_binary_identity_release_family_provider` |
| Release-family binary fan-out hydration | current shape `1.69 s`, about `294k` shared blocks read; with explicit non-empty release-family predicate `0.62 ms`, 24 shared blocks read |
| Assemble Lane A queue-first selector | post-fix shape returned `1,460` structured candidates in about `180 ms`, using `idx_article_assembly_queue_structured_latest` and `idx_binary_completion_keys_match_rank` |
| Assemble Lane A time-aware selector | post-refinement shape returned `1,460` structured candidates in about `288 ms` with a 15-minute time window and no fixed candidate-key cap |

Recommendations:

- Keep dashboard GET paths cache-only. The admin dashboard can show exact counts, but `maintenance.dashboard_stats_refresh` should compute and persist them on a scheduled maintenance interval instead of page-load reads.
- Keep `recover_yenc` under soak observation after the post-audit selection/write fixes; do not retune concurrency until the new 5,000-row behavior is measured under normal supervisor load.
- Keep Lane A on the completed queue-first, time-aware selector and remeasure after a normal supervisor run. Tune `indexing.assemble.lane_a_time_window_minutes` downward only after confirming recovered and ordinary multipart posts remain covered.
- Watch heap fetches and dead tuples on queue/projection tables. Several index-only scans are not truly heap-free under current churn.

## Current Bottleneck Classification

The baseline points to these bottleneck classes:

| Area | Classification | Evidence |
| --- | --- | --- |
| Assemble | DB query shape and write amplification | large ordered `binary_completion_keys` scan, binary upsert chunks, stats refresh, summary/yEnc sync |
| Release formation | DB query shape | binary-family load from `binary_identity_current` was the top `pg_stat_statements` family |
| Release summary refresh | Largely addressed post-audit; monitor hot key quality | dirty-key queue contention was removed, base-stem refresh was batched, and opaque base-stem keys are now suppressed because they overgrouped tens of thousands of binaries |
| Recover yEnc | Largely addressed post-audit; monitor under normal load | selection now fills 5,000-row batches and write time is about `12-13 s` for roughly `2.6k-2.9k` recovered rows; remaining risk is backlog growth versus normal scrape/assemble input |
| Scrape | Stage gating by assemble backlog | scrape paused for assemble catch-up around the observed window |
| Inspection | Candidate query shape and external tool cost | no archive/media backlog, but PAR2/discovery selection can still consume DB time |
| Storage | Table/index size and churn | largest tables are tens of GB; queue/projection dead tuples are visible |

## Storage Guard And Emergency Purge Notes

The 2026-06-23 emergency disk check found the PostgreSQL data partition at `100%` used with about `64 MB` free and the live database at about `266 GB`. The normal application role could not read `current_setting('data_directory')`, and in Docker-style deployments the Postgres container path (`/var/lib/postgresql/data`) is not necessarily visible from the Go process even when the setting is readable. The previous storage guard treated this unknown filesystem state as allowed, which meant supervisor stages could continue writing while the Postgres partition was critically full.

The storage guard now fails closed when it is enabled and filesystem free space is unavailable. Operators should configure `indexing.storage_guard.data_directory` to the host-visible Postgres data path when the app and Postgres do not share the same filesystem namespace. For this audit host, the correct path is `/mnt/vm-store/docker-vols/gonzb_gonzb_postgres_data`. When the path is not configured, non-exempt supervisor stages are blocked until filesystem visibility is fixed.

Low-space exemptions are intentionally narrow. Only archive and purge-specific stages may bypass the low-space block:

| Stage | Reason |
| --- | --- |
| `release_archive_nzb` | can move ready releases into archived/purge-pending state |
| `release_purge_archived_sources` | legacy purge runner for terminal source cleanup |
| `maintenance.release_source_purge` | scheduled maintenance task for terminal source cleanup |

Broad `indexer_maintenance` and `release_generate_nzb` are not low-space exemptions. They can write runtime/cache rows and should not continue during a critical storage event unless an operator explicitly runs a command.

The admin maintenance task API/UI is the supported non-destructive estimate path for source purge. The live validation run completed without deleting rows and estimated one purge candidate. It reported 88 `release_archive_state` rows in `purge_pending` before the dry run. Dry-run estimation still has to stage candidate lineage inside a rollback transaction, so run it only after writers are stopped when the partition is critically full.

Important storage caveat: deleting rows from PostgreSQL usually makes space reusable inside tables, but it does not immediately return large amounts of disk space to the OS. `VACUUM FULL` or similar rewrite tools can return filesystem space, but they require extra free disk and exclusive locks. On a partition already at 100%, the immediate operational fix is to add/move storage or stop writers, then use archive/source purge to reduce future growth and internal table pressure.

## Tuning Guidance

### Homelab

- Keep `track_io_timing=on` and `pg_stat_statements` enabled during active indexer development.
- Prefer increasing scrape only after assemble drains below guard thresholds.
- Keep exact dashboard counts behind the scheduled `maintenance.dashboard_stats_refresh` cache refresh; avoid recomputing the largest counts directly from UI GET requests.
- Keep autovacuum healthy on `binary_completion_keys`, `article_header_assembly_queue`, `yenc_recovery_work_items`, `binary_parts`, and poster queues.
- Increase inspect concurrency only when DB claim/query time is low and NNTP/tool capacity is idle.

### VPS

- Use conservative scrape and assemble batch sizes; smaller disks will show queue churn quickly.
- Keep `work_mem` moderate because several stages can run concurrently.
- Schedule manual `VACUUM (ANALYZE)` windows for high-churn queue tables if autovacuum falls behind.
- Prefer faster storage over more CPU once `pg_stat_activity` shows IO waits and `pg_stat_statements` shows high shared block reads.

### Larger Instance

- Scale assemble and release-summary refresh carefully together. Assemble can create summary and yEnc work faster than downstream stages drain it.
- Consider testing higher `maintenance_work_mem` and autovacuum worker capacity in a separate tuning run.
- Use `pg_stat_statements` deltas per soak window before and after any index/query change.
- Evaluate partitioning or retention only as separate design work; do not mix it into baseline measurement.

## Follow-Up Work

These are recommendations only; they were not applied during the baseline. Items marked addressed were implemented after the audit and should be remeasured in the next long soak:

- Addressed post-audit: review and fix `recover_yenc` ready-window/fairness selection against the exact 18M+ ready backlog.
- Addressed post-audit: change `recover_yenc` selection/write path to make real use of configured concurrency and avoid inline source deletes.
- Re-rank release formation query work. The binary-family load from `binary_identity_current` was the largest statement-time consumer in the soak.
- Addressed post-audit: release-family fan-out now includes the explicit non-empty release-family predicate and keeps cross-newsgroup binary selection, allowing PostgreSQL to use `idx_binary_identity_release_family_provider` without reducing release accuracy semantics.
- Addressed post-audit: release summary refresh no longer serializes writers on a unique dirty-key queue. The refresh queue is append-only and refresh-time deduped, which keeps assemble summary marking in the tens of milliseconds.
- Addressed post-audit: release summary refresh batches `base_stem` keys with a matching `idx_binary_identity_base_stem_summary` index, but suppresses opaque base-stem tokens such as 32-character random names. The live root cause was not lock waits: opaque base-stem summaries had fanout up to `126,720` binaries under one key, causing hot refresh batches to spend `9-15 s` in aggregate/dominant sorting. After removing derived cache rows for those keys and preventing new opaque base-stem enqueues, post-cleanup refresh samples were about `0.12-1.99 s` for real work batches.
- Addressed post-audit: assemble Lane A now uses queue-first, time-aware structured-key selection with matching indexes and no fixed ranked-candidate cap.
- Addressed post-audit: dashboard backlog stats now prefer exact counts refreshed into `indexer_dashboard_stats` by the scheduled `maintenance.dashboard_stats_refresh` task, while dashboard GET remains cache-only.
- Investigate index-only scan heap fetches on high-churn queue tables and tune vacuum/analyze thresholds.
- Add a repeatable audit script under `scripts/` if this soak needs to be rerun regularly.

## Inspection Ready Queue Migration Notes

Discovery is the first inspection stage moved to an indexed ready queue. The queue preserves the existing discovery scope: unrecovered opaque `.bin` or extensionless binaries from `binary_identity_current`, restricted to main payload or non-auxiliary rows, with failed, stale-running, source-updated, and probe-error rows eligible for retry. Discovery still performs byte-signature/content-filter sampling only after claim; the queue only materializes claimability and does not broaden discovery into already identified archive/media/PAR2 files.

Dashboard discovery backlog should count `binary_inspection_ready_queue` rows where `stage_name='inspect_discovery'`, `status='ready'`, and `ready_at <= now()`. This makes the dashboard exact for the backlog the stage actually consumes without replaying the full selector on dashboard refresh.

Do not migrate the other inspection stages by copying discovery blindly. Preserve these selector semantics when each stage gets its own queued ready state:

| Stage | Current candidate-selection behavior to preserve |
| --- | --- |
| `inspect_par2` | PAR2 candidates come from release-linked PAR2 files, `.par2` names, or recovered PAR2 identity. The selector deduplicates by PAR2 set, prefers manifests over volume files, prioritizes release-linked complete sets, reruns failed/stale/source-updated rows, skips completed missing-article probes, and suppresses zero-target volume sets when appropriate. A queue key may need to be set-level rather than binary-only. |
| `inspect_archive` | Archive candidates require payload completion and representative archive names such as `.rar`, `.part01.rar`, `.r00`, `.7z`, `.7z.001`, `.zip`, or `.zip.001`. The selector dedupes archive families so later RAR volumes do not create duplicate work, and reruns probe-error/source-updated rows. |
| `inspect_media` | Media candidates require payload completion and either direct media names (`.mkv`, `.mp4`, `.avi`, `.ts`, `.flac`, `.mp3`, `.m4a`) or archive representatives that already have archive-entry metadata from `inspect_archive`. It must continue to rerun after refreshed archive metadata. |

Queue migrations for PAR2, archive, and media should follow the same pattern as discovery only where the stage's claimable unit is truly a binary. Where the stage naturally dedupes a family or set, the queue should store that family/set key plus the chosen representative binary so dashboard counts match work units, not raw file rows.

## Maintenance Expectations

- Large live datasets need regular autovacuum progress checks, not just table size checks.
- Dead tuple spikes are expected on claim queues and projection tables; persistent spikes after autovacuum indicate a tuning issue.
- Archive-source purge must remain serialized with assemble/purge boundaries and should not be enabled during baseline measurement.
- Performance fixes should be tested one at a time against a fresh `pg_stat_statements_reset()` window.
