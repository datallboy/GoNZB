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

Serve-mode remeasurement after the refresh query rewrite, sampled on `2026-05-27 07:33-07:40 America/New_York` with the same stage mix as the prior benchmark:

- sampled `assemble_lane_b` stage rows:
  - `66874` completed in about `81.0s`: `binary_upsert_duration_ms=54255`, `binary_refresh_duration_ms=50322`
  - `66893` failed in about `92.4s`: `binary_upsert_duration_ms=51881`, `binary_refresh_duration_ms=65618`, error `refresh binary stats batch: ... deadlock detected`
  - `66908` completed in about `112.0s`: `binary_upsert_duration_ms=94196`, `binary_refresh_duration_ms=99125`
  - `66928` was still running when the temporary serve sample was shut down cleanly
- sampled overlap during those lane-B runs remained real:
  - `recover_yenc`: `1-2` overlapping runs
  - `inspect_par2`: `1-3` overlapping runs
  - `release`: `1-3` overlapping runs
  - `assemble_lane_a`: `1-2` overlapping runs

Worker log detail from the same serve window shows how much the refresh query rewrite helped:

- early lane-B slices:
  - `binaries_refreshed=4135`, `binary_upsert_ms=9531.10`, `binary_refresh_ms=6303.84`
  - `binaries_refreshed=11319`, `binary_upsert_ms=20266.02`, `binary_refresh_ms=14319.36`
  - `binaries_refreshed=13706`, `binary_upsert_ms=24356.49`, `binary_refresh_ms=29535.46`
- later lane-B slices:
  - `binaries_refreshed=5040`, `binary_upsert_ms=10315.31`, `binary_refresh_ms=13379.97`
  - `binaries_refreshed=10038`, `binary_upsert_ms=27858.96`, `binary_refresh_ms=28951.38`
  - `binaries_refreshed=15000`, `binary_upsert_ms=38195.31`, `binary_refresh_ms=41986.12`

Comparison to the pre-rewrite serve baseline:

- before the refresh query rewrite, persisted overnight lane-B runs commonly recorded `binary_refresh_duration_ms` in the `120k-320k` band, with the worst overnight sample at `818564`
- after the rewrite, the sampled serve rows dropped into a roughly `50k-99k` aggregate refresh band
- that is still slower than the fresh direct `--once` baseline, but it is a large real reduction in serve-mode refresh cost

Current conclusion after the serve remeasurement:

- the refresh query rewrite materially improved both direct `lane-b --once` and serve-mode lane-B
- lane-B is still slower under overlap than direct runs, but the remaining gap is no longer dominated by the old full-table `article_headers` scan
- deadlocks still occur inside `RefreshBinaryStatsBatch` under serve overlap, which means the remaining next target is not the aggregate read shape; it is the shared write surface inside the refresh transaction
- the most likely remaining contention point is the batched dirty-marker/update work on `release_family_readiness_summaries` and any other summary/queue writes that still happen in the same refresh transaction while `recover_yenc`, `inspect_par2`, and `release` are active

Seventh focused change on the sprint branch:

- added deterministic `FOR UPDATE` locking of requested `binaries` rows inside `refreshBinaryStatsIDsInTx`
- changed `RefreshBinaryStatsBatch` from one large transaction to per-chunk transactions
- added new refresh subphase telemetry:
  - `binary_refresh_tx_count`
  - `binary_refresh_stats_update_ms`
  - `binary_refresh_summary_mark_ms`
  - `binary_refresh_yenc_sync_ms`
  - corresponding `*_max_ms` fields

Why this change was necessary:

- the previous deadlock string was still `refresh binary stats batch`, which meant the conflict was happening in the `binaries` refresh/update phase, not in the later dirty-marker write
- `assemble_lane_b` claims headers, not binaries, so separate lane-B workers can still touch and refresh the same binary rows
- without deterministic binary row locking, overlapping workers and cross-stage refreshes could still deadlock even after the aggregate query rewrite

Post-change direct `assemble lane-b --once` sample on `2026-05-27`:

- worker 1: `binaries_refreshed=709`, `binary_upsert_ms=822.53`, `binary_refresh_ms=664.41`
  - `binary_refresh_tx_count=1`
  - `binary_refresh_stats_update_ms=225.19`
  - `binary_refresh_summary_mark_ms=189.32`
  - `binary_refresh_yenc_sync_ms=237.81`
- worker 2: `binaries_refreshed=1356`, `binary_upsert_ms=1944.34`, `binary_refresh_ms=1007.27`
  - `binary_refresh_tx_count=1`
  - `binary_refresh_stats_update_ms=301.76`
  - `binary_refresh_summary_mark_ms=336.14`
  - `binary_refresh_yenc_sync_ms=363.76`
- worker 3: `binaries_refreshed=4816`, `binary_upsert_ms=6170.52`, `binary_refresh_ms=3399.06`
  - `binary_refresh_tx_count=1`
  - `binary_refresh_stats_update_ms=1539.70`
  - `binary_refresh_summary_mark_ms=903.62`
  - `binary_refresh_yenc_sync_ms=952.33`
- worker 4: `binaries_refreshed=14340`, `binary_upsert_ms=14480.96`, `binary_refresh_ms=10870.55`
  - `binary_refresh_tx_count=2`
  - `binary_refresh_stats_update_ms=6059.09`
  - `binary_refresh_summary_mark_ms=2901.68`
  - `binary_refresh_yenc_sync_ms=1899.01`

What the direct sample says:

- the lock-scope changes preserved correctness and kept direct lane-B stable
- the new subphase metrics show `stats_update` is still the largest refresh component, but `summary_mark` is now large enough to measure and no longer hidden inside one total
- `yenc_sync` remains bounded and is not the dominant tail anymore

Serve-mode remeasurement after deterministic locking and chunked refresh transactions, sampled on `2026-05-27 09:38-09:45 America/New_York`:

- persisted lane-B stage rows before the database incident:
  - `66951` completed in about `138.5s`
    - `binary_upsert_duration_ms=146393.735`
    - `binary_refresh_duration_ms=115799.781`
    - `binary_refresh_tx_count=8`
    - `binary_refresh_stats_update_ms=97850.98`
    - `binary_refresh_summary_mark_ms=14161.078`
    - `binary_refresh_yenc_sync_ms=3636.558`
  - `66985` completed in about `123.7s`
    - `binary_upsert_duration_ms=118967.045`
    - `binary_refresh_duration_ms=92789.457`
    - `binary_refresh_tx_count=7`
    - `binary_refresh_stats_update_ms=70827.661`
    - `binary_refresh_summary_mark_ms=14816.567`
    - `binary_refresh_yenc_sync_ms=7041.916`
- worker log samples in the same serve window:
  - `binaries_refreshed=8716`, `binary_refresh_ms=16095.11`, `stats_update_ms=12782.22`, `summary_mark_ms=2615.09`, `yenc_sync_ms=649.12`
  - `binaries_refreshed=15000`, `binary_refresh_ms=24987.66`, `stats_update_ms=19619.79`, `summary_mark_ms=4203.19`, `yenc_sync_ms=1117.20`
  - `binaries_refreshed=11984`, `binary_refresh_ms=38261.91`, `stats_update_ms=33617.21`, `summary_mark_ms=3684.13`, `yenc_sync_ms=923.17`
  - `binaries_refreshed=12991`, `binary_refresh_ms=36455.09`, `stats_update_ms=31831.76`, `summary_mark_ms=3658.66`, `yenc_sync_ms=947.07`

Important result from this serve sample:

- no `refresh binary stats batch` deadlock was recorded before the benchmark ended
- the sample terminated because the local Postgres instance hit a separate `unexpected EOF` / `database system is in recovery mode` event, which also interrupted unrelated stages
- so this sample is not a perfect apples-to-apples long soak, but it is still useful because the prior repeated lane-B deadlock did not reproduce before the database incident

Current conclusion after the lock-ordering and chunked refresh change:

- the aggregate query rewrite fixed the largest self-inflicted read cost
- deterministic binary row locking plus chunked refresh transactions appear to have reduced the assemble refresh deadlock pressure substantially
- the remaining measured serve-mode tail is now mostly split between:
  - `binary_upsert_query_ms`
  - `binary_refresh_stats_update_ms`
  - and a smaller but real `binary_refresh_summary_mark_ms`
- if deadlocks recur in a longer stable serve soak, the next specific target should be summary dirty-marker writes, because that subphase is now isolated and measurable

Eighth focused change on the sprint branch:

- `markReleaseFamiliesDirtyBatch` now skips `ON CONFLICT DO UPDATE` rewrites when the target summary row is already dirty
- `upsertBinaryGroupingEvidenceBatch` now skips `ON CONFLICT DO UPDATE` rewrites when the evidence payload is unchanged

Why this was worth doing:

- table stats on the live database showed:
  - `release_family_readiness_summaries`: about `13 GB`
  - `binary_grouping_evidence`: about `40 GB`
  - `binaries`: about `48 GB`
- even when subphase timings looked modest, unnecessary rewrites against those table sizes still amplify write latency and lock hold time
- the new refresh telemetry had already isolated `binary_refresh_summary_mark_ms` as a real contributor, so the next safe reduction was to stop touching rows that were already queued

Additional trace on `binary_refresh_stats_update_ms`:

- sampled the current `refreshBinaryStatsIDsInTx` query again on `8000` recently updated binaries
- with the aggregate rewrite and deterministic binary locking in place, the sampled plan now finished in about `232 ms`
- the same sample updated only `93` binaries, which confirms the current query already avoids a large amount of unnecessary work once the row facts are unchanged
- that means `stats_update` is still expensive in serve mode mainly when a run is genuinely refreshing many changed binaries, not because it has fallen back to another catastrophic full-table scan

Post-change direct `assemble lane-b --once` sample after the no-op summary/evidence writes:

- worker 1: `binaries_refreshed=64`, `binary_upsert_ms=61.21`, `binary_refresh_ms=93.45`
  - `binary_refresh_stats_update_ms=76.86`
  - `binary_refresh_summary_mark_ms=3.66`
  - `binary_refresh_yenc_sync_ms=8.10`
- worker 2: `binaries_refreshed=10020`, `binary_upsert_ms=4345.37`, `binary_refresh_ms=12037.52`
  - `binary_refresh_stats_update_ms=8468.12`
  - `binary_refresh_summary_mark_ms=2429.08`
  - `binary_refresh_yenc_sync_ms=1103.00`
- worker 3: `binaries_refreshed=10024`, `binary_upsert_ms=11447.25`, `binary_refresh_ms=10534.87`
  - `binary_refresh_stats_update_ms=7388.87`
  - `binary_refresh_summary_mark_ms=2180.92`
  - `binary_refresh_yenc_sync_ms=934.77`
- worker 4: `binaries_refreshed=13423`, `binary_upsert_ms=15157.14`, `binary_refresh_ms=13403.93`
  - `binary_refresh_stats_update_ms=9113.40`
  - `binary_refresh_summary_mark_ms=3162.55`
  - `binary_refresh_yenc_sync_ms=1109.50`

What changed relative to the previous direct checkpoint:

- previous comparable large-slice direct sample:
  - `binaries_refreshed=14340`, `binary_refresh_ms=10870.55`, `summary_mark_ms=2901.68`
- latest comparable large direct sample:
  - `binaries_refreshed=13423`, `binary_refresh_ms=13403.93`, `summary_mark_ms=3162.55`
- so the direct improvement is not dramatic; the no-op suppression looks more like a contention/bloat reduction than a big standalone latency drop

Serve-mode sample after the no-op summary/evidence writes, sampled on `2026-05-27 10:15-10:18 America/New_York`:

- this sample was cut short by a repeated local Postgres `unexpected EOF / recovery mode` event, so persisted stage rows are not reliable enough for a clean benchmark comparison
- lane-B worker log samples before the database incident still show the new subphase split:
  - `binaries_refreshed=4121`, `binary_upsert_ms=7968.56`, `binary_refresh_ms=7934.39`, `stats_update_ms=6120.72`, `summary_mark_ms=1207.82`, `yenc_sync_ms=600.76`
  - small lane-B slice `binaries_refreshed=73`, `binary_refresh_ms=158.78`, `summary_mark_ms=8.86`
- that supports the same conclusion as the direct run:
  - `binary_refresh_summary_mark_ms` is real, but not as large as `binary_refresh_stats_update_ms`
  - `binary_upsert_query_ms` and `binary_refresh_stats_update_ms` remain the two biggest measured assemble write costs

Current best conclusion:

- the refresh-side full-table scan and row-lock ordering issues have been addressed
- the dirty-marker and evidence no-op suppressions are correct and low-risk, but they are incremental gains, not the next major step-change
- the largest remaining measured assemble cost is still the core `UpsertBinaries` query path, with `binary_refresh_stats_update_ms` next behind it during heavy refresh slices
- if another optimization pass is warranted, the next likely high-value target is `UpsertBinaries` query shape or readback strategy rather than more work on `summary_mark`

## Upsert And Storage Trace

Snapshot taken on `2026-05-27 10:27-10:36 America/New_York`:

- `pg_database_size('gonzb')` was about `192 GB`
- largest heap/index footprints at that point:
  - `binaries`: `49 GB` total, `32 GB` heap, `17 GB` indexes
  - `article_headers`: `47 GB` total, `19 GB` heap, `29 GB` indexes
  - `binary_grouping_evidence`: `41 GB` total, `40 GB` heap, `637 MB` indexes
  - `article_header_ingest_payloads`: `24 GB` total, `17 GB` heap, `6560 MB` indexes
  - `binary_parts`: `18 GB` total, `9072 MB` heap, `9492 MB` indexes
  - `release_family_readiness_summaries`: `13 GB` total, `8162 MB` heap, `4836 MB` indexes
- sampled row-width checks show the storage pressure is not primarily in `binaries` string columns:
  - sampled `binaries.grouping_evidence_json` averaged about `10 B`
  - sampled `binary_grouping_evidence.payload_json` averaged about `1427 B` with sample max about `1722 B`
  - sampled `article_header_ingest_payloads.subject` averaged about `42 B`
  - sampled `article_header_ingest_payloads.xref` averaged about `64 B`
- storage follow-up should prioritize:
  - `binary_grouping_evidence` retention and payload shape
  - `article_header_ingest_payloads` retention policy
  - `article_headers` and `binary_parts` index footprint review

Live `UpsertBinaries` `EXPLAIN ANALYZE` on a synthetic `2000`-row no-op sample:

- current upsert statement:
  - about `99 ms` total
  - `Rows Removed by Conflict Filter: 2000`
  - `Conflicting Tuples: 2000`
  - the statement still dirtied about `422` shared buffers even though every row was a no-op conflict
- current persisted readback query:
  - about `92 ms` total on the same `2000`-row sample
  - does two keyed passes through `binaries_provider_id_newsgroup_id_binary_key_key`
- conclusion:
  - the persisted readback is a real cost
  - but it is not large enough, by itself, to justify a risky SQL rewrite without stronger evidence

Safety finding from live `assemble lane-b --once` tracing on PostgreSQL `17.9`:

- a merged single-statement `requested/existing/upserted` experiment was reverted after it caused a backend `SIGSEGV`
- a fresh `lane-b --once` run on the reverted two-statement path also hit a backend `SIGSEGV` inside the existing `WITH requested ... INSERT ... ON CONFLICT DO UPDATE` statement
- the failing backend log consistently points at the large `VALUES (...)` + `ON CONFLICT` `UpsertBinaries` statement, not at `RefreshBinaryStatsBatch`
- during the same window, Postgres was forcing extremely frequent checkpoints:
  - `max_wal_size = 1024 MB`
  - checkpoint log lines were occurring about every `4-6s`
  - Postgres explicitly logged `checkpoints are occurring too frequently`

Current implication:

- the next safe optimization target is not another more complex CTE merge around the persisted readback
- the bigger immediate risk is the current `UpsertBinaries` statement shape itself on this PostgreSQL build
- best next path:
  - reduce lane-B `binary_upsert_db_chunk_size` and remeasure stability/throughput
  - if crashes persist, replace the inline `VALUES` upsert path with a staging-table or `COPY`-backed path so the hot query stops carrying thousands of scalar parameters and large JSON payloads per chunk

Postgres container tuning follow-up:

- the local container was still running with `max_wal_size = 1GB`, `checkpoint_timeout = 5min`, and `wal_compression = off`
- after the crash trace, the compose defaults were updated to:
  - `max_wal_size = 8GB`
  - `checkpoint_timeout = 15min`
  - `wal_compression = on`
- this should reduce forced checkpoint churn during heavy assemble writes, but it is not a substitute for fixing the `UpsertBinaries` statement shape

Follow-up validation on `2026-05-27 10:48-11:10 America/New_York`:

- runtime setting changed: `assemble_lane_b.binary_upsert_db_chunk_size = 100`
- query-path experiments:
  - inline `VALUES` + `ON CONFLICT` still crashed
  - temp staging table + `ON CONFLICT` still crashed
  - temp staging table + explicit `UPDATE existing` / `INSERT missing` still crashed before reindex
- that means the crash was not caused only by the wrapper query shape (`VALUES`, `ON CONFLICT`, or CTE usage)
- the stronger signal was the `binaries` write surface itself, likely including index maintenance on the local PostgreSQL instance

Corrective action:

- `REINDEX TABLE binaries` completed cleanly
- Postgres container was recreated so the new WAL settings became live:
  - `max_wal_size = 32GB`
  - `checkpoint_timeout = 15min`
  - `wal_compression = on`

Post-reindex / post-recreate direct `assemble lane-b --once` sample:

- command completed successfully with no backend crash
- all four worker slices completed their `15000`-header claims
- worker samples:
  - `binary_upsert_ms=8260.22`, `binary_refresh_ms=38748.03`, `binary_upsert_query_ms=3089.78`
  - `binary_upsert_ms=11088.79`, `binary_refresh_ms=35815.16`, `binary_upsert_query_ms=5847.35`
  - `binary_upsert_ms=11442.26`, `binary_refresh_ms=35342.11`, `binary_upsert_query_ms=6245.86`
  - `binary_upsert_ms=16307.98`, `binary_refresh_ms=30664.85`, `binary_upsert_query_ms=10999.12`
- all workers showed:
  - `binary_upsert_chunk_retries=0`
  - `binary_upsert_chunk_rows=15000`
  - `unique_binary_upserts=15000`

Current interpretation:

- yes, the earlier crash behavior was more likely tied to `binaries` write/index maintenance on this local database than to any one SQL wrapper pattern
- the query-shape reductions were still worthwhile:
  - they lowered successful-chunk `binary_upsert_query_ms`
  - they removed reliance on the more complex crash-prone paths first
- after the `binaries` reindex and container recreate, the current safest state is:
  - keep the staging-table plus explicit `UPDATE` / `INSERT missing` path
  - keep `assemble_lane_b.binary_upsert_db_chunk_size = 100`
  - keep the larger WAL budget so checkpoints are not driven by the old `1GB` cap

Serve-mode measurement after the `binaries` reindex and Postgres recreate, sampled on `2026-05-27 15:08-15:11 America/New_York`:

- `assemble_lane_b` persisted run `67169` completed cleanly in about `130.1s`
- serve-mode lane-B aggregate metrics:
  - `selected_headers=60000`
  - `binaries_refreshed=37632`
  - `binary_upsert_duration_ms=77553.943`
  - `binary_upsert_query_ms=45080.888`
  - `binary_part_upsert_duration_ms=19573.898`
  - `binary_refresh_duration_ms=83621.332`
  - `binary_refresh_stats_update_ms=65934.033`
  - `binary_refresh_summary_mark_ms=15065.864`
  - `binary_refresh_yenc_sync_ms=2569.192`
  - `binary_upsert_chunk_retries=0`
- serve-mode lane-B worker log samples from the same run:
  - `binaries_refreshed=75`, `binary_upsert_ms=391.44`, `binary_refresh_ms=218.28`
  - `binaries_refreshed=7557`, `binary_upsert_ms=28265.35`, `binary_refresh_ms=8928.01`
  - `binaries_refreshed=15000`, `binary_upsert_ms=22860.44`, `binary_refresh_ms=38865.80`
  - `binaries_refreshed=15000`, `binary_upsert_ms=26036.71`, `binary_refresh_ms=35609.24`
- conclusion:
  - the earlier `30-38s` refresh band is not a fixed cost for every lane-B slice
  - refresh time still scales sharply with how many binaries a worker actually refreshes
  - small or sparse slices now land in sub-second to single-digit-second refresh bands
  - heavy `15000`-binary slices still spend most of their time in `stats_update` and then `summary_mark`, so there is still optimization headroom there

Refresh trace follow-up on `2026-05-27 15:12-15:20 America/New_York`:

- a dense `15000`-binary `EXPLAIN ANALYZE` of the pre-patch `refreshBinaryStatsIDsInTx` query showed the planner had regressed back to a full `article_headers` seq scan:
  - scanned about `112.2M` `article_headers` rows
  - execution time about `60-65s`
  - most of the time sat in a hash join between `part_rows` and the whole header table
- the attempted lateral lookup form was flattened back into the same bad hash join by the planner
- changing the header lookup to correlated scalar subqueries against `article_headers_pkey` fixed it:
  - the same `15000`-binary `EXPLAIN ANALYZE` dropped to about `1.9s`
  - the full `article_headers` seq scan disappeared
  - the plan performed about `38.7k` PK lookups instead of scanning all headers

Direct `assemble lane-b --once` rerun after the scalar-lookup patch:

- worker samples:
  - `binaries_refreshed=10026`, `binary_refresh_ms=5329.12`, `binary_refresh_stats_update_ms=1369.34`, `summary_mark_ms=3331.60`
  - `binaries_refreshed=10296`, `binary_refresh_ms=5138.34`, `binary_refresh_stats_update_ms=1352.39`, `summary_mark_ms=3301.37`
  - `binaries_refreshed=15000`, `binary_refresh_ms=6755.11`, `binary_refresh_stats_update_ms=1868.59`, `summary_mark_ms=4364.61`
- comparison to the pre-patch post-reindex direct run:
  - heavy slices were previously `binary_refresh_ms` about `30.7s-38.7s`
  - `binary_refresh_stats_update_ms` is now about `1.35s-1.87s` on similarly heavy slices

Current interpretation after the trace:

- the old heavy refresh cost was not fundamental
- the largest remaining refresh component is now `summary_mark`, not `stats_update`
- the next likely refresh-side optimization target is reducing or deferring `markReleaseFamiliesDirtyBatch` work on very large binary sets

Summary-mark and upsert follow-up on `2026-05-27 15:20-15:39 America/New_York`:

- isolated `summary_mark` measurement with concrete keys:
  - current `INSERT ... ON CONFLICT` dirty-mark path on `15000` concrete readiness keys: about `208 ms`
  - split `UPDATE existing clean` + `INSERT missing` version on the same keys: about `305 ms`
  - interpretation: the multi-second `binary_refresh_summary_mark_ms` seen in live lane-B runs is not caused by a planner disaster in the dirty-mark SQL itself; it is more likely lock overlap between concurrent assemble workers touching the same readiness rows
- while tracing `UpsertBinaries`, the persisted readback query exposed a correctness issue:
  - it was re-reading `binaries` after the update and comparing post-update rows to post-update rows
  - that meant old-vs-new identity changes were effectively invisible in the upsert path
  - this also explained why the earlier lane-B telemetry often showed `binary_upsert_deferred_summary_keys=0`
- corrective change:
  - stage existing matched binary rows into `tmp_existing_binaries` before the update
  - drive the update from that snapshot by `b.id`
  - insert only unmatched rows
  - build the persisted id/identity result from the pre-update snapshot plus inserted rows
- regression coverage:
  - added a repository test that updates one binary from `original-family` / `original-stem` to `renamed-family` / `renamed-stem` under deferred summary refresh and verifies all four readiness rows are marked dirty

Live readback trace after the `tmp_existing_binaries` rewrite:

- old persisted-readback shape on a dense `15000`-row sample: about `1389 ms`
- new persisted-readback shape on the same sample: about `33 ms`
- interpretation:
  - the extra duplicate `binaries` lookups in the old readback query were real overhead
  - the rewrite restores identity-change correctness and materially reduces the readback cost itself

Direct `assemble lane-b --once` rerun after the upsert snapshot fix:

- worker samples:
  - `binaries_refreshed=12123`, `binary_upsert_ms=30285.19`, `binary_upsert_query_ms=24032.83`, `binary_refresh_ms=7578.73`
  - `binaries_refreshed=12763`, `binary_upsert_ms=31632.97`, `binary_upsert_query_ms=24913.19`, `binary_refresh_ms=11485.15`
  - `binaries_refreshed=14731`, `binary_upsert_ms=36013.58`, `binary_upsert_query_ms=28349.52`, `binary_refresh_ms=7839.43`
- comparison to the post-refresh-fix direct baseline:
  - refresh remains far below the old `30s-38s` band for heavy slices
  - the remaining dominant direct-run cost is still `binary_upsert_query_ms`, not `binary_refresh_stats_update_ms`

Serve-mode sample after the upsert snapshot fix, `2026-05-27 15:34:42-15:39:02 America/New_York`:

- completed persisted lane-B run `67192`:
  - `selected_headers=60000`
  - `binaries_refreshed=36110`
  - `binary_upsert_duration_ms=130775.188`
  - `binary_upsert_query_ms=98556.795`
  - `binary_refresh_duration_ms=36057.732`
  - `binary_refresh_stats_update_ms=14643.402`
  - `binary_refresh_summary_mark_ms=18609.082`
- worker log samples from the same serve window:
  - `binaries_refreshed=8286`, `binary_upsert_query_ms=23401.87`, `binary_refresh_summary_mark_ms=4085.91`
  - `binaries_refreshed=12587`, `binary_upsert_query_ms=33800.15`, `binary_refresh_summary_mark_ms=6770.64`
  - `binaries_refreshed=13793`, `binary_upsert_query_ms=35043.74`, `binary_refresh_summary_mark_ms=7173.87`
- interpretation:
  - the refresh-side catastrophic scan is gone in both direct and serve mode
  - serve-mode lane-B is now dominated by `binary_upsert_query_ms`, with `summary_mark` as the next largest overlapping write cost
  - the next worthwhile trace should split `UpsertBinaries` further into temp-stage load, update-existing, insert-missing, and evidence phases so the remaining `binary_upsert_query_ms` can be attacked directly

`UpsertBinaries` subphase split follow-up on `2026-05-27 15:43-15:55 America/New_York`:

- new lane-B telemetry now splits binary upsert work into:
  - `binary_upsert_stage_ms`
  - `binary_upsert_existing_snapshot_ms`
  - `binary_upsert_update_ms`
  - `binary_upsert_insert_ms`
  - `binary_upsert_readback_ms`
  - `binary_upsert_evidence_ms`
- direct `assemble lane-b --once` dense samples show the remaining cost is overwhelmingly the final insert path, not update or readback:
  - `binaries_refreshed=15000`
    - `binary_upsert_stage_ms=3096.13`
    - `binary_upsert_existing_snapshot_ms=2424.73`
    - `binary_upsert_update_ms=114.68`
    - `binary_upsert_insert_ms=26774.22`
    - `binary_upsert_readback_ms=313.06`
    - `binary_upsert_query_ms=27202.32`
- low-risk ordered-insert experiment:
  - changed the `insert-missing` statement to emit rows ordered by `(provider_id, newsgroup_id, binary_key)`, matching the `binaries` unique key
  - dense `15000`-binary rerun after that change:
    - `binary_upsert_stage_ms=2899.56`
    - `binary_upsert_existing_snapshot_ms=2030.82`
    - `binary_upsert_update_ms=114.69`
    - `binary_upsert_insert_ms=26321.17`
    - `binary_upsert_readback_ms=313.93`
    - `binary_upsert_query_ms=26750.11`
- interpretation:
  - ordered inserts helped a little, but not enough to change the overall picture
  - lane B is behaving exactly like the design intended: it is mostly paying for inserting new `binaries` rows
  - the next meaningful optimization will need to target the insert-heavy path itself, most likely by reducing temp-stage cost with `pgx` bulk copy and/or introducing a more explicit lane-B new-row fast path

Aggressive lane-B staging rewrite on `2026-05-27 16:00-16:03 America/New_York`:

- assemble deferred chunks now use a dedicated connection with a manual transaction and `pgx.CopyFrom` into `tmp_upsert_binaries`
- the previous `database/sql` transaction path remains the fallback for non-deferred callers
- this keeps the lane-B fast path scoped to the assemble workload while leaving the rest of the repository path stable

Direct `assemble lane-b --once` after the `pgx.CopyFrom` staging change:

- dense worker `binaries_refreshed=11180`:
  - `binary_upsert_stage_ms=689.57`
  - `binary_upsert_existing_snapshot_ms=1608.45`
  - `binary_upsert_insert_ms=20248.35`
  - `binary_upsert_query_ms=20590.33`
  - `binary_upsert_ms=24509.34`
- dense worker `binaries_refreshed=13845`:
  - `binary_upsert_stage_ms=818.40`
  - `binary_upsert_existing_snapshot_ms=1873.04`
  - `binary_upsert_insert_ms=24524.29`
  - `binary_upsert_query_ms=24928.56`
  - `binary_upsert_ms=29656.94`
- dense worker `binaries_refreshed=14526`:
  - `binary_upsert_stage_ms=853.73`
  - `binary_upsert_existing_snapshot_ms=1985.60`
  - `binary_upsert_insert_ms=25783.17`
  - `binary_upsert_query_ms=26217.37`
  - `binary_upsert_ms=31046.29`

Comparison to the prior ordered-insert direct baseline:

- previous dense `14500-15000` workers:
  - `binary_upsert_stage_ms` about `2899.56-3106.32`
  - `binary_upsert_existing_snapshot_ms` about `2030.82-2315.99`
  - `binary_upsert_insert_ms` about `26321.17-26906.49`
  - `binary_upsert_query_ms` about `26750.11-27344.33`
  - `binary_upsert_ms` about `33743.73-36690.88`
- current dense workers:
  - `binary_upsert_stage_ms` about `689.57-853.73`
  - `binary_upsert_existing_snapshot_ms` about `1608.45-1985.60`
  - `binary_upsert_insert_ms` about `20248.35-25783.17`
  - `binary_upsert_query_ms` about `20590.33-26217.37`
  - `binary_upsert_ms` about `24509.34-31046.29`

Current interpretation:

- `pgx.CopyFrom` materially improved the lane-B fast path
- the temp-stage cost is no longer a major component
- the final `insert-missing` write into `binaries` is still the dominant subphase, but the overall dense-worker upsert total dropped by a meaningful margin
- the next worthwhile step is a serve-mode remeasurement on this staging rewrite before deciding whether a more invasive insert-only fast path is still necessary

Serve-mode remeasurement after the `pgx.CopyFrom` staging rewrite, `2026-05-27 16:05:13-16:10:33 America/New_York`:

- completed persisted lane-B run `67246`:
  - `selected_headers=60000`
  - `binaries_refreshed=30676`
  - `binary_upsert_duration_ms=105334.062`
  - `binary_upsert_stage_ms=3245.090`
  - `binary_upsert_existing_snapshot_ms=7463.895`
  - `binary_upsert_insert_ms=85567.812`
  - `binary_upsert_query_ms=87230.916`
  - `binary_refresh_duration_ms=34626.520`
  - `binary_refresh_stats_update_ms=19840.007`
  - `binary_refresh_summary_mark_ms=12413.861`
- same serve window lane-B worker log samples:
  - `binaries_refreshed=7467`, `binary_upsert_insert_ms=23156.56`, `binary_upsert_query_ms=23635.90`, `binary_refresh_ms=6684.98`
  - `binaries_refreshed=10452`, `binary_upsert_insert_ms=29211.26`, `binary_upsert_query_ms=29744.83`, `binary_refresh_ms=12199.68`
  - `binaries_refreshed=12696`, `binary_upsert_insert_ms=33111.80`, `binary_upsert_query_ms=33726.89`, `binary_refresh_ms=15401.96`
- timed-out persisted lane-B run `67266`:
  - `status=failed`
  - `error_text='refresh binary stats batch: refresh binary stats batch: context canceled'`
  - this was caused by the outer `timeout 320s` ending the serve sample, not by a new deadlock signature

Comparison to the previous serve-mode persisted baseline `67192`:

- `binary_upsert_duration_ms`: `130775.188` -> `105334.062`
- `binary_upsert_query_ms`: `98556.795` -> `87230.916`
- `binary_refresh_duration_ms`: `36057.732` -> `34626.520`
- `binary_refresh_summary_mark_ms`: `18609.082` -> `12413.861`
- caveat:
  - `67246` refreshed fewer binaries than `67192` (`30676` vs `36110`), so this is directional rather than perfectly normalized

Current interpretation after the serve remeasurement:

- the `pgx.CopyFrom` lane-B path improved serve-mode assemble as well as direct `--once`
- the remaining dominant serve-mode cost is still `binary_upsert_insert_ms`, not temp staging or readback
- refresh remains materially lower than the older pre-refresh-fix serve runs, but still large on heavy overlap slices
- if lane-B needs another meaningful gain, the next change has to attack the final `INSERT INTO binaries ... SELECT ...` path itself rather than more staging tweaks

Final `INSERT INTO binaries ... SELECT ...` trace on `2026-05-28 11:12-11:18 America/New_York`:

- direct `EXPLAIN (ANALYZE, BUFFERS, WAL)` of the real lane-B `insert-missing` shape against `2000` synthetic new rows, wrapped in a transaction and rolled back:
  - planner / source side of the query was cheap:
    - subquery produced `2000` rows in about `20.7 ms`
    - sort itself took about `16.7 ms`
  - full insert into the real `binaries` table took about `660.9 ms`
  - write footprint for only `2000` inserted rows:
    - `shared hit=69035`
    - `shared read=1210`
    - `dirtied=5385`
    - `WAL records=20713`
    - `WAL bytes=7047138`
  - foreign-key trigger time was measurable but not dominant:
    - `binaries_newsgroup_id_fkey`: about `13.2 ms`
    - `binaries_poster_id_fkey`: about `14.3 ms`
    - `binaries_provider_id_fkey`: about `14.0 ms`
- control traces using the same `2000` staged rows:
  - insert into a temp clone with no secondary indexes or foreign keys: about `25.3 ms`
  - insert into a temp clone with the full current `binaries` index set: about `63.2 ms`
- the live `binaries` index surface at trace time:
  - `binaries_provider_id_newsgroup_id_binary_key_key`: `2672 MB`
  - `idx_binaries_yenc_recovery_backlog`: `2195 MB`
  - `idx_binaries_file_set_key`: `1691 MB`
  - `idx_binaries_normalized_file_identity`: `1691 MB`
  - `idx_binaries_release_family_key`: `1690 MB`

Current interpretation after the insert trace:

- the lane-B `insert-missing` SQL shape is no longer the main problem
- the expensive part is inserting into the real `binaries` table with its current large secondary-index surface and WAL churn
- foreign-key checks are real but small relative to the table/index maintenance cost
- more SQL-level tuning of the `INSERT ... SELECT ...` statement is unlikely to produce another step-change on its own
- the next meaningful lane-B gain will require reducing what `binaries` has to maintain per new row:
  - either prune or relocate stage-specific lookup indexes off `binaries`
  - or move some queue/backlog lookup responsibilities into dedicated work-item tables so `binaries` remains the canonical fact table without carrying every hot-stage selector index
- this re-aligns directly with the original plan goals:
  - keep primary fact writes narrow
  - isolate derived / stage-specific lookup ownership
  - stop forcing unrelated stages to share the same hot write surface

First `binaries` index-surface reduction on `2026-05-28 11:24-11:31 America/New_York`:

- reset `pg_stat_user_indexes`, then ran one representative overlap sample:
  - `assemble lane-b --once`
  - `recover-yenc --once`
  - `inspect par2 --once`
  - `release --once`
- `binaries` index scans after that sample:
  - actively used:
    - `binaries_provider_id_newsgroup_id_binary_key_key`: `64949` scans
    - `idx_binaries_release_family_key`: `39994` scans
    - `idx_binaries_normalized_file_identity`: `25721` scans
    - `idx_binaries_par2_inspection_backlog`: `1` scan
  - unused in the sample:
    - `idx_binaries_yenc_recovery_backlog`: `0` scans, `2205 MB`
    - `idx_binaries_file_set_key`: `0` scans, `1691 MB`
    - `idx_binaries_updated_at`: `0` scans, `186 MB`
    - `idx_binaries_identity_strength`: `0` scans, `190 MB`
- code-path review aligned with that usage:
  - `recover_yenc` now seeds and consumes `yenc_recovery_work_items`; it no longer needs the old `binaries` backlog selector index
  - no current repository path matched `idx_binaries_updated_at`
  - `idx_binaries_identity_strength` did not match the only current filter shape (`NOT IN ('weak', 'provisional')`)

Applied first pruning step:

- dropped:
  - `idx_binaries_yenc_recovery_backlog`
  - `idx_binaries_updated_at`
  - `idx_binaries_identity_strength`
- schema version advanced to `27`
- reclaimed hot-write index surface:
  - about `2.58 GB` total removed from the `binaries` index set

Measured effect of the first pruning step:

- control clone measurements on `2000` synthetic new rows:
  - full current index set: about `63.2 ms`
  - minus only `idx_binaries_yenc_recovery_backlog`: about `54.1 ms`
  - minus the full dropped set: about `50.3 ms`
- real-table `EXPLAIN (ANALYZE, BUFFERS, WAL)` on the same `2000`-row synthetic insert:
  - before dropping the three indexes:
    - execution time about `660.9 ms`
    - `WAL bytes=7047138`
    - `shared hit=69035`, `shared read=1210`, `dirtied=5385`
  - after dropping the three indexes:
    - execution time about `601.6 ms`
    - `WAL bytes=4210732`
    - `shared hit=53153`, `shared read=4461`, `dirtied=5058`
- interpretation:
  - the first index-surface reduction produced a real write-path gain on the hot fact table
  - the dominant cost is still maintaining the remaining large secondary indexes, but the result confirms this is the right optimization direction
  - `idx_binaries_file_set_key` was left intact at this step because recovered-identity release formation still depends on `file_set_key` semantics and needed a separate query-shape review first

Recovered-file-set follow-up on `2026-05-28 11:32-11:45 America/New_York`:

- first validated that the dropped indexes do not change yEnc recovery or recovered-identity semantics:
  - `recover_yenc` now uses `yenc_recovery_work_items`, so dropping `idx_binaries_yenc_recovery_backlog` removed only an obsolete selector path
  - no release/yEnc query semantics changed in that step
- traced the two remaining `file_set_key` release paths on the live database:
  - recovered-file-set hydration:
    - `SELECT b.id FROM binaries b WHERE b.provider_id = $1 AND b.file_set_key = $2`
    - before index work: parallel seq scan, about `20.6s`
  - recovered-file-set candidate aggregation:
    - `WHERE recovered_source='yenc_header' AND BTRIM(file_set_key)<>'' AND posted_at IS NOT NULL GROUP BY provider_id, file_set_key`
    - before index work: parallel seq scan, about `20.8s`
- root cause:
  - the existing `idx_binaries_file_set_key(provider_id, newsgroup_id, file_set_key)` order did not match the actual release query shape (`provider_id + file_set_key`)
  - the planner ignored it completely

Applied second `file_set_key` access-path correction:

- reordered `idx_binaries_file_set_key` to:
  - `(provider_id, file_set_key, newsgroup_id)`
  - same partial predicate: `WHERE btrim(file_set_key) <> ''`
- tightened recovered-file-set release queries to state `BTRIM(file_set_key) <> ''`, which is already a semantic requirement of the feature and allows the partial index to be considered
- added a dedicated recovered-file-set candidate index:
  - `idx_binaries_recovered_file_set_candidates`
  - key: `(provider_id, file_set_key, newsgroup_id)`
  - include columns needed by the aggregate
  - predicate limited to `recovered_source='yenc_header' AND file_set_key<>'' AND posted_at IS NOT NULL`

Measured effect:

- recovered-file-set hydration after the reordered `idx_binaries_file_set_key`:
  - about `24.8ms`
  - plan switched to `Index Scan using idx_binaries_file_set_key`
- recovered-file-set candidate aggregation after the dedicated candidate index:
  - about `1.11s`
  - plan switched to `Index Only Scan using idx_binaries_recovered_file_set_candidates`
  - previous baseline for the same aggregate was about `20.8s`
- interpretation:
  - this is a release-stage improvement, not an assemble write-cost reduction directly
  - it is still aligned with the active plan because it removes another large cross-stage seq-scan against `binaries`
  - the change preserves recovered-identity grouping semantics; it only replaces ineffective access paths with ones that match the real query shape

Readiness/summary ownership shift on `2026-05-28 11:46-11:57 America/New_York`:

- previous ownership problem:
  - `assemble` deferred most readiness summary recompute already
  - `inspect_par2` still refreshed affected release-family summaries inline at the end of its batch transaction
  - `yenc_recovery` still refreshed affected release-family / base-stem summaries inline inside its merge transaction
  - `release` read `release_family_readiness_summaries` directly but did not own recompute
- applied ownership change:
  - added `release_family_summary_refresh_queue`
  - `markReleaseFamiliesDirtyBatch` now:
    - keeps the existing placeholder dirty row behavior for compatibility
    - also enqueues the summary key into the dedicated refresh queue
  - `inspect_par2` now only dirties/enqueues summary keys; it no longer recomputes readiness summaries inline
  - `yenc_recovery` now only dirties/enqueues summary keys; it no longer recomputes readiness summaries inline
  - `release` now drains a bounded batch of queued summary keys before candidate selection and owns the actual recompute
  - `BackfillYEncRecoveryWorkItems` also drains a bounded batch before reading readiness buckets, because it is another readiness-summary reader
- live smoke:
  - queue before writer pass: `0`
  - after `inspect_par2 --once` (`stage_run=67377`, `processed_count=535`): queue count `32`
  - after `release --once` (`stage_run=67378`): queue count `0`
  - latest release runs after this change:
    - `67376`: `candidate_list_duration_ms=6650.655`, `candidate_families=20000`, `formed=6`
    - `67378`: `candidate_list_duration_ms=6983.923`, `candidate_families=20000`, `formed=18`
- interpretation:
  - summary recompute ownership has now moved off the hot PAR2 and yEnc writer transactions
  - readers (`release`, yEnc work-item seeding) refresh summaries on demand from a bounded queue
  - this preserves readiness semantics while narrowing hot write transactions and removing another source of cross-stage overlap

Serve-mode overlap remeasurement after the queue ownership shift on `2026-05-28 12:02-12:06 America/New_York`:

- runtime shape:
  - `scrape_latest` and `scrape_backfill` were enabled in this sample
  - `enrich_*` remained disabled
  - active overlap included `assemble_lane_a`, `assemble_lane_b`, `inspect_par2`, `recover_yenc`, and `release`
- persisted stage results:
  - `67380 release`: completed in `15.5s`, `candidate_list_duration_ms=8191.473`, `formed=8`
  - `67383 recover_yenc`: completed in `14.7s`, `attempted=0`
  - `67384 assemble_lane_a`: completed in `30.2s`, `selected_headers=6126`, `binaries_refreshed=35`, `binary_upsert_duration_ms=158.277`, `binary_refresh_duration_ms=501.753`
  - `67385 inspect_par2`: completed in `76.8s`, `processed_count=525`
  - `67386 assemble_lane_b`: completed in `64.5s`, `selected_headers=60000`, `binaries_refreshed=36801`, `binary_upsert_duration_ms=68669.519`, `binary_refresh_duration_ms=23898.231`, `binary_refresh_summary_mark_ms=12818.961`
- comparison to the earlier serve baseline before the queue shift and latest lane-B copy path:
  - earlier serve sample `67246` had `binary_upsert_duration_ms=105334.062`, `binary_refresh_duration_ms=34626.520`, `binary_refresh_summary_mark_ms=12413.861`
  - new sample `67386` dropped lane-B aggregate upsert time by about `35%`
  - new sample `67386` dropped lane-B aggregate refresh time by about `31%`
  - lane-B no longer looks like the stage that requires an immediate concurrency cap; it is materially closer to direct `--once` behavior now
- new bottleneck exposed by the queue ownership change:
  - during the same serve window, `release_family_summary_refresh_queue` grew instead of draining:
    - about `36.7k` queued keys at `12:04`
    - about `49.7k` queued keys at `12:05`
    - about `56.7k` queued keys at `12:06`
  - live `pg_stat_activity` showed:
    - `release` actively dequeuing queued summary keys with `FOR UPDATE SKIP LOCKED`
    - concurrent writer stages blocked in `INSERT ... ON CONFLICT` against `release_family_summary_refresh_queue`
  - `release` currently refreshes only `BatchSize*2` queued keys per run (`2000` with the current default) and then moves on to candidate selection
  - that bounded drain rate is now too small to keep up with serve-mode writer overlap
- interpretation:
  - the queue-based ownership shift did its intended job for hot writer transactions; `assemble_lane_b` improved materially under overlap
  - the remaining contention is not a lane-B cap problem first; it is a deferred-summary drain-capacity problem
  - if we add caps or staggering before fixing queue drain throughput, we will be treating the symptom instead of the now-visible bottleneck

Next practical direction after this remeasurement:

- raise summary refresh throughput before adding blunt stage caps:
  - `RefreshQueuedReleaseFamilySummaries` still dequeues a batch and then recomputes each summary one key at a time inside one transaction
  - that is now the clearest remaining overlap bottleneck
- if temporary staggering is needed before the queue drain path is improved, prefer targeted staggering:
  - do not cap `assemble_lane_a`
  - do not globally cap `assemble_lane_b` yet
  - instead, gate `release` candidate formation behind either:
    - a larger queued-summary drain budget, or
    - a backlog threshold where `release` performs refresh-only work until the queue is back under control
- if a queue-based scheduler is introduced, the first useful queue should be for deferred readiness-summary refresh work, not for lane-B itself

Release drain follow-up on `2026-05-28 12:12-12:24 America/New_York`:

- recommendation for `release`:
  - do not cap release candidate formation first
  - increase the deferred summary drain budget ahead of candidate selection
  - keep that drain budget separate from the release candidate batch size
  - use a bounded default of `10000` queued summaries per run unless explicitly overridden
- measured end-to-end `RefreshQueuedReleaseFamilySummaries` timings on the live backlog:
  - `limit=2000`: `refreshed=2000`, about `2446.60 ms`
  - `limit=5000`: `refreshed=5000`, about `5936.07 ms`
  - `limit=10000`: `refreshed=10000`, about `10443.56 ms`
- interpretation:
  - refresh cost scaled close to linearly
  - the old effective serve-time drain budget of `2000` was simply too small relative to writer throughput
  - `10000` is a practical default on this database: large enough to catch up meaningfully, still only about `10.4s` of refresh work at current data scale
- direct `release --once` sample after raising the drain budget:
  - queue before run: `3743`
  - latest persisted release run `67447`:
    - `summary_refresh_count=3743`
    - `summary_refresh_duration_ms=3381.028`
    - `candidate_list_duration_ms=6486.962`
    - `candidate_families=20000`
    - `formed=8`
    - queue after run: `0`
- query-shape review for the release summary queue:
  - the `ORDER BY queued_at ... LIMIT n FOR UPDATE SKIP LOCKED` selector was already fine
  - the delete half was improved to target locked rows by `ctid`
  - queue dequeue SQL before the change:
    - `2000` rows: about `38.3 ms`
    - `5000` rows: about `43.6 ms`
    - `10000` rows: about `62.7 ms`
  - queue dequeue SQL after the `ctid` delete rewrite:
    - `2000` rows: about `17.9 ms`
    - `5000` rows: about `29.1 ms`
    - `10000` rows: about `52.7 ms`
  - interpretation:
    - this is a useful cleanup and removes one unnecessary full-key join
    - but it also confirms the dequeue SQL is not the main bottleneck; the dominant cost is still the per-summary recompute work itself

Current release conclusion:

- the next release-side improvement should focus on summary recompute throughput, not candidate selection ordering
- temporary staging advice, if needed before more code lands:
  - let `release` run with the larger summary drain budget
  - if queue backlog still grows under serve load, prefer a refresh-only pass or dedicated summary-refresh stage before introducing blunt assemble caps

Release summary recompute throughput follow-up on `2026-05-28 12:31-12:51 America/New_York`:

- traced the dirty workload shape before changing code:
  - dirty summary rows were overwhelmingly `release_family`
    - `release_family`: `391648`
    - `base_stem`: `10`
  - sampled dirty `release_family` keys were mostly one-binary families
    - on the first `100` dirty families: `p50=1`, `p90=1`, `max=1`
  - implication:
    - the old refresh loop was paying two binary reads plus one summary upsert per key even though many keys only describe one binary
    - a set-based `release_family` read path should help more than further work on `base_stem`
- implemented next step:
  - `RefreshQueuedReleaseFamilySummaries` now splits the queue by key kind
  - `release_family` keys use one set-based aggregate + dominant-row query for the whole batch
  - only the final summary upserts remain per-row
  - `base_stem` keys still use the old per-key path because they are currently negligible
- measured result against the earlier helper baseline:
  - previous `10000`-summary refresh baseline: about `10443.56 ms`
  - new `10000`-summary refresh measurement after the set-based read path: about `8295.61 ms`
  - improvement: about `20.6%%`
- direct stage-level sample after the change:
  - seeded queue: `5000`
  - persisted release run `67448`:
    - `summary_refresh_batch_size=10000`
    - `summary_refresh_count=5000`
    - `summary_refresh_duration_ms=4200.267`
    - `candidate_list_duration_ms=6714.167`
    - `candidate_families=20000`
    - `formed=6`
    - queue after run: `0`
- interpretation:
  - the set-based read path produced a real improvement and validates the direction
  - the remaining refresh cost is now mostly final summary upsert churn, not repeated binary aggregate reads
  - if more release-side throughput is needed, the next likely step is batching the final summary writes through a temp stage / merge path rather than returning to per-key read queries

Release final summary write batching follow-up on `2026-05-29 07:43-07:44 America/New_York`:

- implemented next step:
  - refreshed `release_family` summaries are now staged into a temp table and merged into `release_family_readiness_summaries` with one final `INSERT ... SELECT ... ON CONFLICT DO UPDATE`
  - this replaced the previous per-summary final upsert loop after the set-based read pass
- live backlog condition at measurement time:
  - queued refresh backlog was already very large before the run:
    - seeded count observed as about `3,416,708`
  - this means the sample is useful as a stress test, not just a small clean-room microbenchmark
- measured result:
  - persisted release run `69826`:
    - `summary_refresh_batch_size=10000`
    - `summary_refresh_count=10000`
    - `summary_refresh_duration_ms=6815.45`
    - `candidate_list_duration_ms=18122.94`
    - `candidate_families=20000`
    - `formed=6`
  - nearby prior release run before this merge-path change:
    - `69804`
    - `summary_refresh_duration_ms=7035.84`
    - `candidate_list_duration_ms=31190.405`
- interpretation:
  - batching the final summary write helped, but only modestly on the refresh subphase itself
  - the candidate-list wall clock improved more than the raw summary-refresh time, which suggests the staged merge reduced transaction friction, but it did not solve the backlog problem
  - the much more important current fact is backlog scale:
    - after one `10000`-summary drain run, the queue was still about `3,406,708`
  - conclusion:
    - release-side write batching is worth keeping
    - but the next bottleneck is no longer query shape first; it is drain capacity and refresh scheduling
    - one `10000`-summary refresh pass per release run is not enough when backlog is in the millions

Current practical direction after the final-write batching pass:

- keep the batched release-family read path
- keep the staged final summary merge path
- next highest-value change should be operational or scheduler-oriented:
  - allow `release` to execute multiple refresh batches before candidate formation when backlog exceeds a threshold
  - or split summary refresh into its own dedicated stage so release candidate formation is not the only drainer
  - or both

## Active Execution Backlog

- [x] Add chunk-level repository telemetry around `UpsertBinaries`: chunk count, rows per chunk, retry count, retry cause, and chunk duration, so lane-B regressions can be tied to actual lock/retry pressure instead of only wall-clock totals.
- [x] Remove or defer inline release-family summary refresh work from `UpsertBinaries` chunk transactions where practical. Assemble now uses a deferred path, and live telemetry shows current lane-B slices usually do not touch any upsert-time summary keys anyway.
- [x] Remove or defer inline release-family summary refresh work from `RefreshBinaryStatsBatch` where practical. Assemble now defers this path too, and dirty-marker writes are batched instead of one summary row at a time.
- [x] Re-test `assemble_lane_b` with a smaller `binary_upsert_db_chunk_size` to see whether PostgreSQL `17.9` stability improves without giving back too much throughput.
- [x] Decide whether `UpsertBinaries` should move to a staging-table / `COPY` path instead of giant inline `VALUES` upserts.
- [x] Review the first `binaries` secondary-index pruning set and remove clearly obsolete hot-write indexes (`yenc` backlog, generic `updated_at`, unused `identity_strength`).
- [x] Replace the ineffective `file_set_key` access path with index ordering that matches recovered-file-set release queries, plus a dedicated recovered-file-set candidate index.
- [ ] Continue reviewing the remaining large `binaries` lookup indexes and move stage-specific backlog / lookup responsibilities off the hot fact table where practical.
- [x] Move readiness-summary recompute ownership to readers: fact writers dirty/enqueue; `release` and yEnc work-item seeding drain and recompute from a bounded refresh queue.
- [x] Re-measure serve-mode overlap after the queue-based summary ownership shift and decide whether any temporary concurrency caps or stage staggering are still needed.
- [x] Re-measure `assemble_lane_b` in serve mode after summary-refresh isolation changes and compare it directly to `assemble lane-b --once`.
- [ ] Increase deferred readiness-summary drain throughput so `release_family_summary_refresh_queue` can keep up with serve-mode writer overlap before candidate formation becomes backlog-bound.
