# Indexer Backlog Burn-Down Performance Plan

Snapshot date: 2026-04-22

This is the current active execution plan for indexer performance work after the initial assemble/release refinement pass.

Use this plan as the day-to-day execution guide for:

- assemble backlog burn-down
- release queue throughput and queue quality
- PostgreSQL and runtime tuning for indexing throughput
- schema and repository changes that reduce repeated selector work

Use `docs/active/INDEXER_ASSEMBLE_AND_RELEASE_REFINEMENT_PLAN.md` as the baseline history and completed-context reference for the refinement work already landed on 2026-04-21.

## Why This Plan Exists

The earlier refinement pass fixed the worst selector-query failures and established safer assemble/release behavior, but live DB inspection shows the system is not yet ready to call the phase complete:

- the backlog is still large
- near-complete release families still linger in the `90%` to `99%` range
- assemble lane A is still too dependent on a recent-header window
- release candidate selection still recomputes family readiness from `binaries` across a very large dirty queue

Current interpretation:

- the system does not look primarily hardware-bound
- the biggest remaining gains should come from queue policy, precomputed family readiness, and PostgreSQL tuning
- release safety rules should stay conservative while throughput improves

## Current Measured Direction

Recent observations that matter for this plan:

- host capacity is not obviously exhausted:
  - `8` CPU cores
  - `23 GiB` RAM
  - healthy filesystem headroom
- PostgreSQL is still conservatively tuned for this workload:
  - `shared_buffers = 128MB`
  - `effective_cache_size = 4GB`
  - `work_mem = 4MB`
  - `random_page_cost = 4`
  - `effective_io_concurrency = 1`
  - `track_io_timing = off`
- assemble selector execution is now cheap in isolation, but the current lane-A sample window can still miss useful binary-completion work
- release selector execution is still spending significant time probing dirty families that resolve to non-actionable fragment-only work

## Goals

- clear `article_headers.assembled_at IS NULL` backlog faster
- bias assemble toward finishing already-started main payload binaries
- make release candidate selection read cheap precomputed readiness instead of repeatedly aggregating from `binaries`
- reduce fragment-only queue churn without weakening release persistence rules
- create an operator-friendly tuning and validation loop for throughput work

## Non-Goals

- weakening release safety thresholds just to increase release counts
- reopening the completed stabilization/schema-baseline decisions without a new blocker
- broad Phase 3 API/UI work before throughput and queue quality are signed off

## Workstreams

### 1. PostgreSQL and runtime tuning baseline

Treat the database as a local low-latency workload and tune it accordingly.

Planned tuning direction:

- make `jit=off` explicit and observable for indexer DB sessions
- increase cache and sort memory so hot selector and refresh paths stop running on conservative defaults
- tune cost settings for local SSD/NVMe rather than generic spinning-disk assumptions
- enable `track_io_timing`
- increase statistics quality for the hot tables involved in selector planning
- tighten autovacuum/analyze thresholds on:
  - `article_headers`
  - `binaries`
  - `release_stage_dirty_families`

Expected result:

- more reliable query plans during live churn
- less planner drift from stale statistics
- better sustained performance under write-heavy indexer activity

### 2. Assemble completion-first candidate selection

The current recent-header-first lane A helped query cost, but it is still not the best throughput policy for backlog burn-down.

Next direction:

- make the progress-improving assemble lane start from incomplete binaries, not only from recent pending headers
- rank candidate binaries by:
  - incomplete before complete
  - main payload before auxiliary
  - higher completion ratio before lower
  - higher observed-part count before lower
- fetch matching unassembled headers for those binaries by structured file identity
- keep a smaller fresh-work lane so new releases do not starve entirely

Expected result:

- more of each assemble pass should complete or materially improve known binaries
- near-complete releases should move to `100%` faster when the missing headers already exist in the DB
- the `90%` to `99%` pool should shrink through actual completion rather than just queue churn

### 3. Release family readiness summary state

The current release selector still recomputes family readiness from `binaries` across a large dirty queue. That is now the main structural cost center.

Next direction:

- add a new incremental family-readiness summary table keyed by:
  - `provider_id`
  - `newsgroup_id`
  - `key_kind`
  - `family_key`
- store enough summary state to answer release-candidate ordering cheaply:
  - `binary_count`
  - `complete_binary_count`
  - `incomplete_binary_count`
  - expected-file-count evidence present or absent
  - `total_bytes`
  - representative source/release names
  - earliest posted-at value
  - last-updated value
  - readiness bucket
- maintain this state transactionally from binary update paths, especially:
  - `UpsertBinary`
  - `RefreshBinaryStats`
- make `ListReleaseCandidates` read dirty rows plus summary state instead of aggregating from `binaries` for every sampled family

Readiness buckets:

- `actionable`
- `stale_cleanup_only`
- `fragment_only`

Expected result:

- release selection cost becomes more proportional to queue rows than to repeated family aggregation work
- actionable families surface more reliably even when the dirty queue is large
- fragment-only cooldown remains intact without dominating normal release passes

### 4. Queue-quality validation and backlog burn-down operations

This plan assumes active validation while performance changes land.

Operator loop:

- validate repeated `assemble --once` and `release --once` runs instead of relying on single-pass anecdotes
- compare:
  - processed headers per minute
  - refreshed binaries per minute
  - completed binaries per hour
  - formed releases per hour
  - dirty-family composition by readiness bucket
- continue using operator queries from `docs/INDEXER_TEST_QUERIES.md` to inspect:
  - near-complete releases
  - missing binary parts
  - pending matching headers
  - stale or partially unlinked release materialization

## Immediate Action Order

1. confirm and implement PostgreSQL/runtime tuning plus a fresh `VACUUM ANALYZE` pass on the hot tables
2. rework assemble lane A into a binary-driven completion lane while preserving a smaller fresh-work lane
3. add release family summary state and switch `ListReleaseCandidates` to summary-backed selection
4. rerun live validation and update the refinement plan status based on measured churn

## Relationship To Other Docs

- `docs/active/INDEXER_FOUNDATION_DOCS.md`
  - source of truth for which docs are active
- `docs/active/INDEXER_NEXT_PHASE_ROADMAP.md`
  - top-level sequencing and Phase 3 gate
- `docs/active/INDEXER_ASSEMBLE_AND_RELEASE_REFINEMENT_PLAN.md`
  - baseline refinement history and previously landed work
- `docs/INDEXER_TEST_QUERIES.md`
  - operator validation commands and DB inspection queries

## Exit Direction

This plan should be considered successful when:

- assemble backlog is burning down at a sustained rate
- assemble is clearly spending most priority work on binary-completion progress
- release queue passes regularly find actionable work without large fragment-only scan overhead
- the near-complete release pool trends toward `100%` completion or clear cooldown outcomes
- the active refinement loop can be signed off and moved out of the active-doc set
