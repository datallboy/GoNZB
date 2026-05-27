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

## Active Execution Backlog

- [x] Add chunk-level repository telemetry around `UpsertBinaries`: chunk count, rows per chunk, retry count, retry cause, and chunk duration, so lane-B regressions can be tied to actual lock/retry pressure instead of only wall-clock totals.
- [x] Remove or defer inline release-family summary refresh work from `UpsertBinaries` chunk transactions where practical. Assemble now uses a deferred path, and live telemetry shows current lane-B slices usually do not touch any upsert-time summary keys anyway.
- [ ] Remove or defer inline release-family summary refresh work from `RefreshBinaryStatsBatch` where practical. Current code still dedupes keys but refreshes each summary one at a time in the same transaction as the stats update.
- [ ] Decide which stage owns readiness/summary refresh for binaries touched by assemble, PAR2 coverage writes, yEnc recovery writes, and release updates so unrelated stages stop recomputing the same derived rows.
- [ ] Re-measure `assemble_lane_b` in serve mode after summary-refresh isolation changes and compare it directly to `assemble lane-b --once`.
- [ ] Decide whether temporary serve-mode concurrency caps or stage staggering are still needed once the write-overlap changes land, or whether they can be removed.
