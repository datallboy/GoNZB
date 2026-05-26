# Indexer DB Write Contention Isolation Plan

Snapshot date: 2026-05-26

Status: on hold until the current PAR2 batching, yEnc work-item, and remaining assemble follow-up items are complete.

This doc captures the longer-run database plan that surfaced from the serve-vs-once assemble analysis. It is intentionally separate from the current sprint docs so the active execution backlog stays focused.

## Why This Exists

Current evidence shows that the main live slowdown is shared Postgres write pressure, not NNTP capacity and not supervisor overhead.

- direct `assemble lane-b --once` runs are materially faster than serve-mode overlap runs
- the largest live slowdowns happen when `assemble_lane_b`, `recover_yenc`, `inspect_par2`, and `release` all write into related binary and summary state at the same time
- `inspect_par2` has already shown real deadlock behavior in logs

This is the planning track for reducing row-lock overlap and derived-summary churn before considering Redis, an external queue, or a separate worker topology.

## On-Hold Scope

This plan is deferred behind:

1. PAR2 inspect batch persistence and write telemetry
2. yEnc exact backlog and candidate-selection work-item design
3. remaining assemble follow-up telemetry and release-summary review

This doc should not drive day-to-day sprint execution until those items are farther along.

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
