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

Status:

- initial WorkStream 1 baseline completed and signed off on `2026-04-22`
- reference doc: `docs/INDEXER_POSTGRES_RUNTIME_TUNING.md`

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

WorkStream 1 execution plan:

- start by treating the current host as a developer workstation first, not as the production sizing target
- keep production guidance SSD-first and RAM-positive, but make lower-end system guidance explicit so operators can still run the indexer safely with reduced expectations
- separate:
  - baseline measurement
  - PostgreSQL settings changes
  - table-specific maintenance policy
  - runtime concurrency validation

#### WorkStream 1a. Baseline and evidence capture

Capture a before-state before changing settings.

Record:

- host profile:
  - CPU core count
  - total RAM
  - whether storage is NVMe, SATA SSD, or HDD
  - approximate free disk space
- PostgreSQL runtime settings:
  - `shared_buffers`
  - `effective_cache_size`
  - `work_mem`
  - `maintenance_work_mem`
  - `random_page_cost`
  - `effective_io_concurrency`
  - `track_io_timing`
  - `jit`
  - `default_statistics_target`
- hot-table state:
  - row counts for `article_headers`, `binaries`, and `release_stage_dirty_families`
  - last analyze / vacuum timestamps from `pg_stat_user_tables`
  - dead-tuple counts and autovacuum activity for those same tables
- operator throughput:
  - `assemble --once` runtime across several repeated passes
  - `release --once` runtime across several repeated passes
  - pending header count
  - near-complete release count
  - dirty-family composition

Use:

- `SHOW ALL` or focused `SHOW` commands for PostgreSQL settings
- `EXPLAIN (ANALYZE, BUFFERS)` on the current assemble and release hot paths
- `docs/INDEXER_TEST_QUERIES.md` for the release/backlog state queries already in active use
- `VACUUM (ANALYZE)` only after the before-state is captured

Success criteria for this baseline step:

- we can compare before and after with more than one stage run
- we know whether the bottleneck is plan quality, buffer churn, I/O latency, or queue-policy cost
- we have enough evidence to avoid over-tuning the dev laptop for a workload that will later live on stronger hardware

#### WorkStream 1b. Environment tiers and default tuning posture

This plan should be documented and validated against three operating tiers.

Tier 1: dev laptop

- purpose:
  - practical local development
  - repeatable performance validation
  - not full production throughput
- hardware posture:
  - SSD strongly preferred
  - `16 GB` to `32 GB` RAM is comfortable
  - `6` to `8+` CPU cores is enough for local indexing and inspection work
  - preserve thermal and battery headroom instead of trying to saturate the machine
- PostgreSQL starting posture:
  - `shared_buffers`: start at `1 GB`, move toward `2 GB` only if the machine remains comfortable
  - `effective_cache_size`: start around `25%` to `40%` of host RAM
  - `work_mem`: start at `16 MB`
  - `maintenance_work_mem`: start at `512 MB`
  - `random_page_cost`: `1.1` to `1.5` on SSD/NVMe
  - `effective_io_concurrency`: `64` for SATA SSD, `128` to `256` for NVMe
  - `track_io_timing`: `on`
  - `default_statistics_target`: `250`
  - session `jit`: keep `off`
- runtime posture:
  - prefer steady single-instance validation over aggressive parallel stage scheduling
  - keep enough RAM free for the OS page cache and normal desktop use

Tier 2: lower-end self-hosted system

- purpose:
  - safe operation on constrained hardware without pretending it is production-grade
- hardware posture:
  - SSD is still the recommendation
  - HDD should be treated as functional-but-degraded, not preferred
  - `8 GB` RAM is the practical floor for a meaningful PostgreSQL-backed indexer workload
  - `4` CPU cores is workable if expectations are modest
- PostgreSQL starting posture:
  - `shared_buffers`: `512 MB` to `1 GB`
  - `effective_cache_size`: `2 GB` to `4 GB`
  - `work_mem`: `8 MB` to `16 MB`
  - `maintenance_work_mem`: `256 MB`
  - `random_page_cost`: `1.25` to `1.75` on SSD, higher only if truly on HDD
  - `effective_io_concurrency`: `32` to `64` on SSD, low values on HDD
  - `track_io_timing`: `on`
  - `default_statistics_target`: `100` to `250`
  - session `jit`: keep `off`
- operator expectation:
  - slower backlog burn-down
  - tighter disk-space monitoring
  - more conservative release and inspect concurrency

Tier 3: production server

- purpose:
  - sustained backlog burn-down and steady indexing throughput
- hardware recommendation:
  - NVMe or strong SSD storage
  - plenty of free disk space for ongoing header, binary, and release churn
  - `8+` real CPU cores
  - `32 GB+` RAM
  - enough spare capacity that autovacuum and analyze can keep up during active ingestion
- PostgreSQL starting posture:
  - `shared_buffers`: begin near `25%` of RAM and adjust with measurement
  - `effective_cache_size`: begin near `50%` to `75%` of RAM depending on what else runs on the host
  - `work_mem`: start at `16 MB` to `32 MB`
  - `maintenance_work_mem`: `1 GB` or higher when memory headroom allows
  - `random_page_cost`: `1.1`
  - `effective_io_concurrency`: `128` to `256` on NVMe
  - `track_io_timing`: `on`
  - `default_statistics_target`: `250` or higher for hot selector tables if measurement justifies it
  - session `jit`: keep `off`
- operator expectation:
  - this is the tier that should be used for final throughput sign-off
  - dev-laptop numbers are useful for direction, but not the final ceiling

#### WorkStream 1c. PostgreSQL settings changes to land first

First-pass changes to apply before deeper code or schema work:

- make session-level `jit=off` observable in operator notes and validation output
  - the app already sets `jit=off` in the PostgreSQL connection runtime parameters for the indexer store
  - this step is about documenting and verifying it, not rediscovering it later
- raise cache-related settings away from stock conservative defaults
- tune planner cost assumptions for SSD or NVMe
- enable `track_io_timing`
- raise statistics quality enough to stabilize selector plans during churn

For the dev laptop baseline represented in this doc, the first pass should be:

- `shared_buffers = 1GB`
- `effective_cache_size = 8GB`
- `work_mem = 16MB`
- `maintenance_work_mem = 512MB`
- `random_page_cost = 1.1`
- `effective_io_concurrency = 64`
- `track_io_timing = on`
- `default_statistics_target = 250`

Do not treat these values as universal defaults:

- lower-end systems may need smaller cache and maintenance memory
- larger production servers should be tuned upward from evidence, not copied from the laptop profile

#### WorkStream 1d. Table-specific maintenance and statistics policy

The hot tables in this phase need more aggressive maintenance than generic PostgreSQL defaults.

Apply and validate tighter autovacuum/analyze behavior for:

- `article_headers`
- `binaries`
- `release_stage_dirty_families`

Starting policy direction:

- lower `autovacuum_vacuum_scale_factor`
- lower `autovacuum_analyze_scale_factor`
- use higher per-table statistics targets on the selector-critical columns instead of only relying on the global default
- run a fresh `VACUUM (ANALYZE)` on the hot tables immediately after the settings change so the first comparison is not distorted by stale stats

Acceptance check:

- the hot tables should show recent analyze activity during live indexing churn
- dead tuples should not grow unchecked between repeated operator runs
- release and assemble selectors should stop bouncing between obviously different plans for similar queue states

#### WorkStream 1e. Runtime and concurrency posture

This workstream is not only about PostgreSQL GUCs. We also need a stable runtime posture while testing.

Validation posture:

- compare repeated `assemble --once` runs instead of only one run after each DB change
- compare repeated `release --once` runs under a dirty queue that is large enough to matter
- do not stack aggressive background scrape, assemble, release, and inspect churn on the laptop while trying to learn which DB change helped
- if runtime pool or stage concurrency becomes a visible bottleneck later, change it after the PostgreSQL baseline is measured, not before

Operator note:

- the goal for the laptop is a trustworthy tuning baseline
- the goal for production is sustained throughput under continuous churn
- those are related, but they should not be conflated

#### WorkStream 1f. Deliverables for sign-off

WorkStream 1 should produce:

- a recorded before/after settings snapshot
- a short operator tuning guide for:
  - dev laptop
  - lower-end self-hosted system
  - production SSD/NVMe server
- measured before/after `assemble --once` and `release --once` timing notes
- fresh `VACUUM (ANALYZE)` confirmation on hot tables
- confirmation that stats freshness and planner behavior improved before moving on to WorkStream 2

WorkStream 1 completion note:

- this baseline has now been completed for the current dev laptop
- PostgreSQL defaults were replaced with a laptop-safe low-latency profile in `docker-compose.postgres.yml`
- hot-table autovacuum and statistics settings were applied live to:
  - `article_headers`
  - `binaries`
  - `release_stage_dirty_families`
- a fresh `VACUUM (ANALYZE)` pass completed on the hot tables
- before/after settings, stage timings, backlog snapshot, and `EXPLAIN (ANALYZE, BUFFERS)` observations were recorded in `docs/INDEXER_POSTGRES_RUNTIME_TUNING.md`
- isolated manual reruns after clearing background schedulers produced a cleaner validation sample:
  - `assemble --once` average `20.07s` across `3` runs
  - `release --once` average `64.92s` across `3` runs
- current interpretation after sign-off:
  - PostgreSQL is no longer running on obviously conservative defaults for this workload
  - release selection still has structural aggregation cost that belongs to WorkStream 3
  - assemble selection still has structural pending-window cost that belongs to WorkStream 2

### 2. Assemble completion-first candidate selection

The current recent-header-first lane A helped query cost, but it is still not the best throughput policy for backlog burn-down.

Live validation after WorkStream 1 showed that the completion-first idea is directionally correct, but trying to express the whole split path through one blended selector makes the lane-A query too unstable and too expensive on real backlog data.

Status:

- WorkStream 2 completed and signed off on `2026-04-22`

Next direction:

- keep the split policy:
  - lane A = completion-first progress work
  - lane B = smaller fresh-work lane
- do not make operators run separate commands for A and B
- stop trying to make one large SQL selector own both policies at once
- instead, implement two separate internal repository paths:
  - `ListProgressAssemblyHeaders(limit int)`
  - `ListRecentUnassembledHeaders(limit int, excludeIDs...)`
- make lane A start from incomplete binaries, not from a recent pending-header window
- rank lane-A candidate binaries by:
  - main payload before auxiliary
  - higher completion ratio before lower
  - higher observed-part count before lower
  - newer binary id last as a tiebreak only
- keep lane A tightly bounded:
  - small ranked binary candidate set
  - at most `1` pending header per priority binary per pass unless later measurement justifies more
  - merge and dedupe in memory after the two repository calls
- keep lane B simple:
  - recent unassembled headers by id desc
  - fill the remainder of the batch after lane A

Implementation guidance:

- treat lane A and lane B as separate internal code paths in the assemble service
- prefer simple bounded queries over one clever multi-CTE query
- if lane A still stays too expensive after the split-query refactor, escalate to persisted progress state instead of continuing SQL guesswork:
  - a small progress queue or side table keyed by incomplete binary identity
  - or lightweight persisted fields that say pending matching headers exist for that binary

Expected result:

- more of each assemble pass should complete or materially improve known binaries
- near-complete releases should move to `100%` faster when the missing headers already exist in the DB
- the `90%` to `99%` pool should shrink through actual completion rather than just queue churn
- lane A should become operationally understandable and cheap enough to validate repeatedly

Validation direction:

- compare repeated `assemble --once` runs after the split-query refactor
- verify that `lane_a_selected` is regularly non-zero when matching progress work exists
- verify that lane A no longer dominates stage runtime just by selecting work
- verify that lane B still keeps fresh arrivals moving when lane A has little to do
- if lane A remains structurally expensive, stop tuning the selector in place and move directly to persisted progress state as the next WorkStream 2 implementation step

WorkStream 2 completion note:

- the assemble selector now uses separate internal paths:
  - a binary-ranked lane A that starts from incomplete binaries
  - a recent-header lane B that fills the remainder
- the selector tuning constants in `assembly_store.go` now have explicit meanings instead of inline magic numbers:
  - `assembleLaneARatioNumerator` and `assembleLaneARatioDenominator`:
    - target share of each assemble batch reserved for progress work
    - current value is `7/10`, so lane A tries to fill about `70%` of the batch before lane B fills the rest
  - `assemblePriorityBinaryMinScan`:
    - minimum ranked incomplete-binary window to inspect for actionable progress work
    - current value is `1000`
  - `assemblePriorityBinaryMaxScan`:
    - maximum ranked incomplete-binary window to inspect for actionable progress work
    - current value is `2000`
  - `assemblePriorityBinaryBatch`:
    - number of ranked binaries fetched per lane-A lookup batch
    - current value is `20`
- lane-A binary ranking remains bounded and binary-driven:
  - main payload before auxiliary
  - higher completion ratio before lower
  - higher observed parts before lower
  - only `1` pending header per priority binary per pass
- lane A was improved in two concrete ways:
  - the lane-A pending-header lookup was rewritten to match the existing structured-name payload index directly instead of scanning the payload table backward by primary key
  - the ranked binary candidate window was widened from an ineffective top `20` slice to a bounded `1000` to `2000` window so actionable progress binaries are actually reached on the live backlog
- live database validation before sign-off showed:
  - `783` incomplete binaries currently had pending matching headers
  - the first actionable progress binary appeared at rank `32`
  - actionable progress binaries within the ranked window:
    - `16` within top `50`
    - `54` within top `200`
    - `500` within top `1000`
    - `629` within top `2000`
- isolated manual `assemble --once` reruns after runtime repair completed normally and kept lane A active:
  - run 1: `14.65s`, `lane_a_selected=0`, `lane_b_selected=2500`
  - run 2 after widening the ranked window: `17.35s`, `lane_a_selected=614`, `lane_b_selected=1886`
  - run 3: `16.11s`, `lane_a_selected=597`, `lane_b_selected=1903`
  - run 4: `15.30s`, `lane_a_selected=578`, `lane_b_selected=1922`
- later live production-like churn continued to confirm the same pattern in the operator log:
  - `2026-04-22 16:42:42`: `lane_a_selected=566`, `lane_b_selected=1934`
  - `2026-04-22 16:43:11`: `lane_a_selected=554`, `lane_b_selected=1946`
  - `2026-04-22 16:43:26`: `lane_a_selected=529`, `lane_b_selected=1971`
  - `2026-04-22 16:43:41`: `lane_a_selected=521`, `lane_b_selected=1979`
  - `2026-04-22 16:43:47`: release stage logged `candidate_families=190 formed=149`
  - `2026-04-22 16:45:29`: release stage logged `candidate_families=183 formed=150`
- backlog movement during the validated reruns:
  - pending headers fell from `735861` to `725861`
- current live backlog shape after the additional validated runs:
  - pending headers: `699924`
  - incomplete binaries with pending matching headers still available to lane A: `636`
- operator interpretation:
  - `lane_a_selected` counts headers chosen for progress-improving binary work, not releases formed
  - lane A improves binary completion and release readiness indirectly; it is not currently instrumented as a per-release attribution metric
  - the `149` and `150` formed-release runs are real release-stage totals observed shortly after repeated high lane-A assemble passes, but they cannot be claimed as lane-A-only output
  - lane A is not expected to stay near `500` forever
  - while there are still many incomplete binaries with pending matching headers, lane A should remain materially active
  - as the stock of incomplete-but-actionable binaries is drained, lane A should trend downward and lane B should naturally take a larger share of the batch for fresh work
- current interpretation after sign-off:
  - lane A is now operationally understandable and cheap enough to validate repeatedly
  - the remaining primary structural cost center is release-family readiness work in WorkStream 3
  - persisted lane-A progress state is not required at this time

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

1. completed on `2026-04-22`: PostgreSQL/runtime tuning baseline plus fresh `VACUUM ANALYZE` on the hot tables
2. completed on `2026-04-22`: assemble lane A reworked into a bounded binary-driven completion lane with a separate fresh-work lane and live exit validation
3. next: add release family summary state and switch `ListReleaseCandidates` to summary-backed selection
4. next: rerun live validation and update the refinement plan status based on measured churn

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
