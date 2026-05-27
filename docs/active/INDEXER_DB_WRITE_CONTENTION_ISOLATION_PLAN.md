# Indexer DB Write Contention Isolation Plan

Snapshot date: 2026-05-26

Status: active follow-up for the remaining assemble/release write-overlap work now that the PAR2 batching and yEnc work-item items are complete enough to stop blocking it.

This doc captures the longer-run database plan that surfaced from the serve-vs-once assemble analysis. It is intentionally separate from the current sprint docs so the active execution backlog stays focused.

## Why This Exists

Current evidence shows that the main live slowdown is shared Postgres write pressure, not NNTP capacity and not supervisor overhead.

- direct `assemble lane-b --once` runs are materially faster than serve-mode overlap runs
- the largest live slowdowns happen when `assemble_lane_b`, `recover_yenc`, `inspect_par2`, and `release` all write into related binary and summary state at the same time
- `inspect_par2` has already shown real deadlock behavior in logs

This is the planning track for reducing row-lock overlap and derived-summary churn before considering Redis, an external queue, or a separate worker topology.

## Current Scope

This plan now owns the remaining cross-stage write-contention work that was left over after the PAR2 and yEnc execution items landed:

1. `assemble_lane_b` still regresses materially under serve-mode overlap even after batched binary upserts and set-based binary-stat refreshes.
2. The remaining hot surfaces are derived-summary refreshes and overlapping write paths, not NNTP transport.
3. The active question is no longer whether assemble needs batching. It does, and that landed. The active question is how much of the remaining slowdown is still inline rollup churn and lock overlap with `recover_yenc`, `inspect_par2`, and `release`.

## Shared Hot Surfaces

The current commands most likely fight over these tables and rows:

- `binaries`
- `binary_parts`
- `release_family_readiness_summaries`
- release rollup tables such as `releases` and related readiness/update paths

## Database And Query Goals

We need commands to stop fighting over the same rows during normal serve-mode overlap. The database and DBO layer should move toward these goals:

- primary fact writes stay narrow and stage-local
- derived rollup writes move out of hot ingestion/recovery loops where practical
- transactions hold fewer locks for less time
- query ordering is deterministic when multiple workers can touch the same logical entities
- summary refresh work is set-based or deferred, not one-row-at-a-time inside unrelated write paths

## Required Database-Side Changes

### 1. Separate primary facts from derived rollups

- keep binary identity and part facts in their canonical tables
- stop treating readiness/summary rows as part of every hot-path write transaction
- prefer marking affected families dirty or enqueueing refresh work instead of refreshing summaries inline

### 2. Reduce transaction scope on hot write paths

- keep chunked transactions for large binary upserts
- avoid holding advisory locks across oversized multi-row operations
- keep compute/parse work outside the transaction when possible

### 3. Make lock acquisition more predictable

- preserve deterministic row ordering for `INSERT ... ON CONFLICT` and related update batches
- avoid multiple code paths taking overlapping locks in different orders
- review whether any remaining advisory-lock usage can be narrowed further

### 4. Isolate derived refresh ownership

- define which stage owns binary-stat refresh, PAR2 coverage refresh, readiness-summary refresh, and release rollup refresh
- remove duplicate or opportunistic refreshes from unrelated stages where the same result can be produced by one dedicated path

## Required DBO / Repository Query Changes

### 1. Batch write paths that still commit per candidate

- PAR2 inspect needs chunked artifact/set/target/coverage/completion persistence
- any remaining one-row-at-a-time summary or coverage update paths need consolidation into set-based repository calls

### 2. Split "record facts" from "refresh summaries"

- repository APIs should make it explicit when a call is writing canonical facts versus derived rollups
- the DBO layer should support a lightweight dirty-marker or refresh-queue write without forcing immediate summary recomputation

### 3. Add explicit hot-path telemetry in the repository layer

- chunk count
- rows written per chunk
- chunk duration
- deadlock retry count
- lock/conflict failure count
- summary refresh batch size and duration

### 4. Review expensive update joins against hot tables

- identify repository calls that repeatedly join `binaries`, `binary_parts`, and readiness-summary state in the same transaction
- move those joins to indexed staging tables, work-item tables, or deferred refresh passes where practical

## What This Likely Means In Practice

The likely direction is:

1. write canonical facts first
2. mark downstream rollups dirty or enqueue them for refresh
3. let a dedicated summarizer or bounded refresh path update derived state separately

That should reduce commands stepping on the same summary rows during serve-mode overlap.

## Future Sprint Entry Criteria

Open this as an active execution sprint only after:

- PAR2 batching is implemented and measured
- yEnc work-item design is implemented or at least concretely specified
- assemble telemetry is complete enough to attribute remaining hot SQL and refresh costs

## Future Sprint Exit Criteria

- serve-mode overlap no longer causes large assemble lane B regressions relative to direct `--once` runs
- PAR2, yEnc, assemble, and release do not routinely update the same derived summary rows inside separate hot-path transactions
- deadlock retries and lock-related failures are rare, measured, and attributable when they do happen
- dashboard/runtime counts remain exact without requiring the heaviest cross-table derived queries on every refresh

## 2026-05-26 Initial Execution Findings

First focused change on the sprint branch:

- added chunk-level `UpsertBinaries` telemetry to assemble metrics and logs
- added an assemble-only context path that defers inline release-family summary refreshes during binary upsert chunk commits
- added a regression test proving deferred assemble upserts leave readiness rows dirty without forcing an inline recompute

Targeted validation:

- `go test ./internal/store/pgindex ./internal/indexing/assemble`

Baseline for comparison:

- the most recent pre-branch direct `assemble lane-b --once` sample in the archived NNTP/inspection sprint recorded worker `binary_upsert_ms` in the rough `64s-72s` band, with `binary_refresh_ms` around `3.2s-5.8s` in the best sample and much higher under serve overlap
- the last persisted serve-mode regression sample remained `stage_run=61510`, where aggregate `binary_upsert_duration_ms=365471.659` and `binary_refresh_duration_ms=304367.373`

Post-change direct `assemble lane-b --once` sample on this branch:

- worker 1: `binaries_refreshed=6972`, `binary_upsert_ms=31562.96`, `binary_refresh_ms=43929.14`, `binary_upsert_chunk_count=28`, `binary_upsert_chunk_retries=0`, `binary_upsert_chunk_max_ms=1466.07`
- worker 2: `binaries_refreshed=12509`, `binary_upsert_ms=54132.33`, `binary_refresh_ms=50786.86`, `binary_upsert_chunk_count=51`, `binary_upsert_chunk_retries=0`, `binary_upsert_chunk_max_ms=1466.90`
- worker 3: `binaries_refreshed=13334`, `binary_upsert_ms=58184.44`, `binary_refresh_ms=92715.99`, `binary_upsert_chunk_count=54`, `binary_upsert_chunk_retries=0`, `binary_upsert_chunk_max_ms=1285.15`
- worker 4: `binaries_refreshed=9515`, `binary_upsert_ms=84330.06`, `binary_refresh_ms=106438.16`, `binary_upsert_chunk_count=39`, `binary_upsert_chunk_retries=2`, `binary_upsert_chunk_retry_deadlocks=2`, `binary_upsert_chunk_max_ms=43555.24`

Key finding from the new telemetry:

- all four workers reported `binary_upsert_deferred_summary_chunks=0` and `binary_upsert_deferred_summary_keys=0`
- that means the current lane-B slices are usually not hitting the old inline `UpsertBinaries` summary-refresh path at all
- the remaining dominant tail is still `RefreshBinaryStatsBatch`, plus occasional chunk deadlocks inside the upsert path

Implication for the next patch:

- keep the new `UpsertBinaries` telemetry and assemble-only defer path because they are low-risk and now measurable
- shift the next isolation change toward `RefreshBinaryStatsBatch` summary ownership/deferral and any lock ordering inside that path, because that is where the live lane-B tail still sits

Second focused change on the sprint branch:

- `RefreshBinaryStatsBatch` now also supports the assemble-only deferred summary path
- deferred summary marking now uses batched readiness-row upserts instead of one `markReleaseFamilyDirty` call per key
- this keeps canonical binary stat updates in place while reducing per-family derived row churn inside assemble

Post-change direct `assemble lane-b --once` sample after refresh deferral plus batched dirty markers:

- worker 1: `binaries_refreshed=6908`, `binary_upsert_ms=36834.49`, `binary_refresh_ms=34940.83`, `binary_refresh_summary_key_count=6850`
- worker 2: `binaries_refreshed=8737`, `binary_upsert_ms=45546.35`, `binary_refresh_ms=64832.09`, `binary_refresh_summary_key_count=8674`
- worker 3: `binaries_refreshed=13458`, `binary_upsert_ms=77827.79`, `binary_refresh_ms=65101.33`, `binary_refresh_summary_key_count=13451`
- worker 4: `binaries_refreshed=9675`, `binary_upsert_ms=115304.76`, `binary_refresh_ms=59702.19`, `binary_upsert_chunk_retries=1`, `binary_upsert_chunk_retry_deadlocks=1`

What changed relative to the previous refresh-deferral-only sample:

- `binary_refresh_ms` dropped from roughly `95.9s` and `110.1s` worst-case workers to roughly `59.7s-65.1s` in the latest run
- the first worker dropped to `34.9s` refresh time, which is materially below the prior `40.3s`
- the remaining worst tail is now more clearly the upsert path when it hits deadlock/retry or a very slow chunk, not the refresh-summary loop itself

Current conclusion:

- `RefreshBinaryStatsBatch` was absolutely part of the assemble contention path, not a dashboard-only concern
- moving assemble off inline summary recompute and then batching the dirty-marker writes reduced the refresh-side contention cost meaningfully
- the next likely technical target is deterministic lock behavior / chunk retry pressure inside `UpsertBinaries`, plus a serve-mode overlap remeasurement after these assemble-only changes

Third focused change on the sprint branch:

- traced the full `assemble lane-b` write path and found that `binary_refresh_ms` still included `syncYEncRecoveryWorkItemsForBinariesInTx`
- the retirement half of that yEnc sync was catastrophically shaped: on a 2,000-binary sample it scanned about `1.47M` `yenc_recovery_work_items`, seq-scanned about `20.8M` `binaries`, and took about `23.97s`
- rewrote the retirement update to join only requested binary ids instead of scanning global work-item state
- batched binary-key advisory locks so `UpsertBinaries` no longer issues one `pg_advisory_xact_lock` statement per binary key
- batched `binary_grouping_evidence` deletes/upserts so `UpsertBinaries` no longer issues one evidence statement per binary

Plan evidence after the yEnc sync rewrite:

- pre-change sample query: yEnc retirement for a 2,000-binary requested set took about `23970 ms`
- post-change sample query: the same retirement shape took about `15 ms`

Post-change direct `assemble lane-b --once` sample after the yEnc retirement rewrite:

- worker 1: `binaries_refreshed=110`, `binary_upsert_ms=722.11`, `binary_refresh_ms=1053.56`
- worker 2: `binaries_refreshed=1754`, `binary_upsert_ms=9365.40`, `binary_refresh_ms=1340.70`
- worker 3: `binaries_refreshed=8319`, `binary_upsert_ms=33396.44`, `binary_refresh_ms=7233.60`
- worker 4: `binaries_refreshed=12245`, `binary_upsert_ms=48537.77`, `binary_refresh_ms=10163.17`

Post-change direct `assemble lane-b --once` sample after batched lock/evidence writes:

- worker 1: `binaries_refreshed=83`, `binary_upsert_ms=553.01`, `binary_refresh_ms=278.47`
- worker 2: `binaries_refreshed=4497`, `binary_upsert_ms=20072.43`, `binary_refresh_ms=4026.24`
- worker 3: `binaries_refreshed=8172`, `binary_upsert_ms=34890.86`, `binary_refresh_ms=8320.24`
- worker 4: `binaries_refreshed=13370`, `binary_upsert_ms=51950.00`, `binary_refresh_ms=12229.76`

What this means:

- the hidden yEnc sync query was one of the biggest real assemble costs, even in direct `--once` runs with no other stages active
- after that rewrite, `binary_refresh_ms` is no longer the dominant tail; it has moved into a much smaller `0.3s-12.2s` band depending on slice composition
- the remaining direct-run bottleneck is now mostly the core `UpsertBinaries` path itself, not downstream refresh/yEnc summary churn
- the remaining upsert path still performs a heavyweight `INSERT ... ON CONFLICT` with existing-row comparison logic; the next likely optimization target is reducing what that query has to read/return when assemble does not need immediate old-family summary cleanup

Fourth focused change on the sprint branch:

- added subphase telemetry inside `UpsertBinaries` so `binary_upsert_ms` is now split into:
  - `binary_upsert_lock_ms`
  - `binary_upsert_query_ms`
  - `binary_upsert_evidence_ms`

Post-change direct `assemble lane-b --once` sample with phase timing:

- worker 1: `unique_binary_upserts=84`, `binary_upsert_ms=401.13`, `lock_ms=2.25`, `query_ms=388.99`, `evidence_ms=3.23`
- worker 2: `unique_binary_upserts=2688`, `binary_upsert_ms=10826.86`, `lock_ms=29.75`, `query_ms=10378.23`, `evidence_ms=132.61`
- worker 3: `unique_binary_upserts=3232`, `binary_upsert_ms=13110.00`, `lock_ms=28.97`, `query_ms=12522.78`, `evidence_ms=278.51`
- worker 4: `unique_binary_upserts=6561`, `binary_upsert_ms=23382.06`, `lock_ms=44.29`, `query_ms=22454.15`, `evidence_ms=321.10`

Current conclusion from phase timing:

- the worker values are not cumulative; each worker is processing a different 15k-header slice with a different number of unique binary keys
- the first worker is usually faster because it often has far fewer unique binary upserts and far more cache hits
- after batching locks and evidence maintenance, neither lock time nor evidence time is the main problem
- the dominant remaining cost is the main `INSERT ... ON CONFLICT` query itself, including the pre-read/current-row comparison work around it

Fifth focused change on the sprint branch:

- rewrote `UpsertBinaries` to avoid rewriting unchanged `binaries` rows
- kept correctness by splitting the operation into:
  - one upsert statement with `DO UPDATE ... WHERE` only when facts materially change
  - one readback query in the same transaction to fetch canonical ids/final identity state
- added a regression test proving identical upserts preserve the same row without bumping `updated_at`, while stronger updates still apply

Post-change direct `assemble lane-b --once` sample after no-op row rewrite avoidance:

- worker 1: `unique_binary_upserts=6619`, `binary_upsert_ms=8733.33`, `query_ms=7488.33`, `refresh_ms=5255.34`
- worker 2: `unique_binary_upserts=6655`, `binary_upsert_ms=8710.10`, `query_ms=7568.84`, `refresh_ms=8220.54`
- worker 3: `unique_binary_upserts=7164`, `binary_upsert_ms=9170.73`, `query_ms=7779.04`, `refresh_ms=13573.32`
- worker 4: `unique_binary_upserts=11263`, `binary_upsert_ms=14203.82`, `query_ms=11878.34`, `refresh_ms=13482.17`

Compared to the prior phase-timed sample:

- previous large-slice workers were at `query_ms` about `10378`, `12523`, and `22454`
- after skipping unchanged row rewrites, comparable/high-volume workers dropped into about `7488`, `7569`, `7779`, and `11878`
- per-chunk max query time also dropped into about `639 ms-644 ms`, with no retries in the validation run

Current state after this change:

- direct `assemble lane-b --once` is much healthier than the branch baseline
- refresh-side summary/yEnc overhead is no longer the dominant problem
- assemble still needs a serve-mode overlap remeasurement, because the direct run bottleneck is now substantially reduced and the next question is what still regresses under concurrent stage pressure

Direct baseline after the no-op upsert rewrite, using three additional fresh `assemble lane-b --once` runs on `2026-05-26`:

- worker slices were consistently in the `13.9k-15.0k` claimed-header range
- `unique_binary_upserts` ranged from about `6345` to `14183`
- `binary_upsert_ms` ranged from about `6337` to `16199`
- `binary_upsert_query_ms` ranged from about `5305` to `13361`
- `binary_upsert_lock_ms` stayed small at about `43-89`
- `binary_upsert_evidence_ms` stayed modest at about `339-902`
- `binary_refresh_ms` ranged from about `4452` to `16190`
- all baseline workers completed with `binary_upsert_chunk_retries=0`

Interpretation of the direct baseline:

- direct `lane-b --once` is now stable enough that retry/deadlock noise is largely gone in the sampled runs
- the dominant remaining direct-run cost is still the main `UpsertBinaries` query path, not lock acquisition and not evidence maintenance
- the spread between workers is driven primarily by per-slice `unique_binary_upserts`, not cumulative timing

Serve-mode remeasurement on `2026-05-26`, with `scrape_*` and enrich stages disabled but `assemble_lane_a`, `assemble_lane_b`, `recover_yenc`, `release`, and inspect stages enabled:

- initial serve sample exposed a release SQL regression in `ListReleaseCandidates`: `invalid UNION/INTERSECT/EXCEPT ORDER BY clause`
- fixed by splitting the `UNION ALL` queue CTE into `combined_queue` plus ordered `next_queue`
- follow-up serve sample used a temporary config copy bound to `:18080` so the benchmark would not collide with the existing local `:8080` server

Persisted serve-mode stage metrics from the clean sample window:

- `release` `stage_run=61550`: completed in about `66.7s`, `candidate_families=20000`, `formed=176`, `candidate_list_duration_ms=50136.53`, `ack_candidate_duration_ms=8082.483`
- `recover_yenc` `stage_run=61554`: completed in about `111.4s`, `attempted=999`, `recovered=997`, `merged=975`, `fetch_failures=2`, `write_ms=29341.355`
- `inspect_par2` `stage_run=61557`: completed in about `50.9s`, `processed_count=384`, `prefix_fetch_ms=149638.337`, `result_flush_ms=4053.044`
- `assemble_lane_a` `stage_run=61535`: completed in about `31.2s`, `processed_headers=47742`, `binary_upsert_duration_ms=1195.78`, `binary_refresh_duration_ms=2567.889`

Serve-mode `assemble_lane_b` evidence from supervisor logs during the same run:

- worker sample 1: `unique_binary_upserts=11343`, `binary_upsert_ms=24373.31`, `binary_refresh_ms=20397.05`, `header_match_ms=9042.30`, `binary_part_upsert_ms=11296.60`
- worker sample 2: `unique_binary_upserts=13179`, `binary_upsert_ms=29493.16`, `binary_refresh_ms=22140.30`, `header_match_ms=8675.47`, `binary_part_upsert_ms=6346.64`
- those lane-B workers were still active when the `220s` serve timeout shut the process down, so the runtime repair marked the unfinished `assemble_lane_b` row abandoned

What the serve remeasurement says now:

- `assemble_lane_a` is healthy under overlap and no longer a meaningful concern
- `recover_yenc` and `inspect_par2` are both completing successfully in serve mode with current concurrency
- `assemble_lane_b` still regresses materially under overlap relative to the direct baseline:
  - direct baseline large slices now show roughly `8.7s-16.2s` `binary_upsert_ms`
  - serve-mode lane-B worker samples were roughly `24.4s-29.5s` `binary_upsert_ms`
  - direct baseline large slices show roughly `5.3s-16.2s` `binary_refresh_ms`
  - serve-mode lane-B worker samples were roughly `20.4s-22.1s` `binary_refresh_ms`
- the remaining sprint question is therefore no longer “can direct lane-b be made sane?”; it can
- the remaining question is what still causes lane-B to slow down when release/recovery/inspect stages are active at the same time

Overnight `assemble_lane_b` analysis from `2026-05-27 00:00-07:00 America/New_York` changed that conclusion:

- sampled overnight rows included `218` `assemble_lane_b` runs:
  - `202` completed, average wall time about `110.7s`, p95 about `235.4s`
  - `14` failed, average wall time about `160.1s`, p95 about `243.8s`
  - `2` were abandoned after lease expiry
- all `14` overnight failures except one were `refresh binary stats batch: ERROR: deadlock detected`
- one distinct failure at `stage_run=66150` was `upsert binaries batch missing id for ordinal 135`
- the worst overnight completed runs were not dominated by `binary_upsert_duration_ms`; they were dominated by `binary_refresh_duration_ms`
  - `stage_run=66202`: `binary_upsert_duration_ms=151013`, `binary_refresh_duration_ms=818564`
  - `stage_run=64309`: `binary_upsert_duration_ms=41788`, `binary_refresh_duration_ms=311764`
  - `stage_run=65655`: `binary_upsert_duration_ms=61322`, `binary_refresh_duration_ms=644505`
- overlap counts on the worst rows were real but fairly steady, usually `2-5` concurrent runs each of `recover_yenc`, `inspect_par2`, `release`, and `assemble_lane_a`
- that means overlap was amplifying the problem, but the base cost had moved back into lane-B itself

Fresh direct `assemble lane-b --once` validation on `2026-05-27` confirmed that:

- worker 1: `binaries_refreshed=53`, `binary_upsert_ms=51.87`, `binary_refresh_ms=84.04`
- worker 2: `binaries_refreshed=9973`, `binary_upsert_ms=9858.80`, `binary_refresh_ms=75176.12`
- worker 3: `binaries_refreshed=10019`, `binary_upsert_ms=9970.42`, `binary_refresh_ms=76692.84`
- worker 4: `binaries_refreshed=15000`, `binary_upsert_ms=14641.90`, `binary_refresh_ms=80612.59`

That direct run matters because no supervisor overlap was present. The `75s-80s` refresh band meant `RefreshBinaryStatsBatch` itself was still mis-shaped even after the earlier yEnc retirement and deferred-summary changes.

Root-cause trace of `RefreshBinaryStatsBatch`:

- sampled the current `refreshBinaryStatsIDsInTx` query on `8000` recently updated binaries using `EXPLAIN ANALYZE`
- the existing query spent about `56.5s`
- the dominant problem was a hash join that seq-scanned the full `article_headers` table:
  - about `111.4M` `article_headers` rows scanned
  - about `2.45M` shared buffers read
  - about `523k` temp buffers read and written
- this was happening because the query joined `requested -> binary_parts -> article_headers` in one aggregate, which gave the planner enough rope to build a full-table hash side on `article_headers`

Sixth focused change on the sprint branch:

- rewrote `refreshBinaryStatsIDsInTx` to materialize the requested `binary_parts` rows first, then join that bounded set to `article_headers` by primary key
- sampled `EXPLAIN ANALYZE` for the rewritten shape on the same `8000`-binary cohort:
  - old shape: about `56509 ms`
  - new materialized `part_rows` shape: about `919 ms`
- the new plan used:
  - index scans on `idx_binary_parts_binary_id`
  - index scans on `article_headers_pkey`
  - no global `article_headers` seq scan

Post-change direct `assemble lane-b --once` sample after the refresh query rewrite:

- worker 1: `binaries_refreshed=57`, `binary_upsert_ms=84.19`, `binary_refresh_ms=93.65`
- worker 2: `binaries_refreshed=5945`, `binary_upsert_ms=5643.32`, `binary_refresh_ms=4595.83`
- worker 3: `binaries_refreshed=14594`, `binary_upsert_ms=13801.28`, `binary_refresh_ms=16036.01`
- worker 4: `binaries_refreshed=15000`, `binary_upsert_ms=14047.72`, `binary_refresh_ms=16040.90`

What this changes in the diagnosis:

- lane-B was not only fighting other stages overnight; it was also paying a bad self-inflicted refresh query cost
- the best next path was not more `UpsertBinaries` tuning first; it was fixing the `RefreshBinaryStatsBatch` aggregate shape
- after this rewrite, the direct refresh tail drops back into the same rough band as `UpsertBinaries`, which should also shorten the window where overlap deadlocks can happen
- the next remeasurement should be serve-mode again, specifically to see how many `refresh binary stats batch` deadlocks disappear now that the refresh transaction is much shorter

## Active Execution Backlog

- [x] Add chunk-level repository telemetry around `UpsertBinaries`: chunk count, rows per chunk, retry count, retry cause, and chunk duration, so lane-B regressions can be tied to actual lock/retry pressure instead of only wall-clock totals.
- [x] Remove or defer inline release-family summary refresh work from `UpsertBinaries` chunk transactions where practical. Assemble now uses a deferred path, and live telemetry shows current lane-B slices usually do not touch any upsert-time summary keys anyway.
- [x] Remove or defer inline release-family summary refresh work from `RefreshBinaryStatsBatch` where practical. Assemble now defers this path too, and dirty-marker writes are batched instead of one summary row at a time.
- [ ] Decide which stage owns readiness/summary refresh for binaries touched by assemble, PAR2 coverage writes, yEnc recovery writes, and release updates so unrelated stages stop recomputing the same derived rows.
- [ ] Re-measure `assemble_lane_b` in serve mode after summary-refresh isolation changes and compare it directly to `assemble lane-b --once`.
- [ ] Decide whether temporary serve-mode concurrency caps or stage staggering are still needed once the write-overlap changes land, or whether they can be removed.
