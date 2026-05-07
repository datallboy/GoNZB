# Indexer Backlog Burn-Down Performance Plan

Snapshot date: 2026-04-22

This is the current active execution plan for indexer performance work after the initial assemble/release refinement pass.

Use this plan as the day-to-day execution guide for:

- assemble backlog burn-down
- release queue throughput and queue quality
- PostgreSQL and runtime tuning for indexing throughput
- schema and repository changes that reduce repeated selector work

Use `docs/archive/completed/indexer/INDEXER_ASSEMBLE_AND_RELEASE_REFINEMENT_PLAN.md` as the baseline history and completed-context reference for the refinement work already landed on 2026-04-21.

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
- reference doc: `docs/archive/development/indexer/INDEXER_POSTGRES_RUNTIME_TUNING.md`

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
- `docs/archive/development/indexer/INDEXER_TEST_QUERIES.md` for the release/backlog state queries already in active use
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
- before/after settings, stage timings, backlog snapshot, and `EXPLAIN (ANALYZE, BUFFERS)` observations were recorded in `docs/archive/development/indexer/INDEXER_POSTGRES_RUNTIME_TUNING.md`
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

Status:

- WorkStream 3 completed and signed off on `2026-04-22`
- current live queue snapshot before summary-state landing:
  - dirty families total: `112`
  - `release_family`: `63`
  - `base_stem`: `49`
  - actionable families in a representative queue window: `91`
  - fragment-only families in the same window: `21`
  - zero-binary stale-cleanup rows in that window: `0`
- interpretation:
  - the release queue is no longer obviously blocked by sheer queue width on the dev system
  - the remaining avoidable cost is repeated per-family aggregation from `binaries`
  - we should move that work to incremental write-time maintenance before relying on more runtime tuning

WorkStream 3 completion note:

- landed implementation:
  - added `release_family_readiness_summaries` as a release-family summary-state table
  - backfilled summary rows for existing `release_family` and `base_stem` keys during schema migration
  - moved `ListReleaseCandidates` from per-family lateral aggregate scans over `binaries` to dirty-queue plus keyed summary lookups
  - refreshed summary state transactionally from:
    - `UpsertBinary`
    - `RefreshBinaryStats`
  - when a binary identity moves families, refresh now covers:
    - old `release_family`
    - new `release_family`
    - old `base_stem` when applicable
    - new `base_stem` when applicable
  - runtime schema support was advanced from `5` to `6` so the new migration is accepted during startup validation
- repository validation completed:
  - `go test ./internal/store/pgindex ./internal/indexing/release`
  - targeted coverage now includes:
    - summary-row refresh after `RefreshBinaryStats`
    - old/new summary cleanup when a binary moves family identity
    - preserved queue ordering for:
      - complete versus fragment-only families
      - expected-file-count evidence
      - zero-binary stale cleanup candidates
- live schema and DB validation after landing:
  - `module_schema_version.pgindex = 6`
  - summary rows materialized successfully:
    - total rows: `123627`
    - `release_family`: `61884`
    - `base_stem`: `61743`
  - summary bucket distribution immediately after backfill:
    - `release_family actionable = 485`
    - `release_family fragment_only = 61399`
    - `base_stem actionable = 406`
    - `base_stem fragment_only = 61337`
  - representative dirty-queue join before the validated release pass:
    - `release_family actionable = 33`
    - `release_family fragment_only = 13`
    - `base_stem actionable = 29`
    - `base_stem fragment_only = 8`
  - `EXPLAIN` on the summary-backed selector showed:
    - bounded scan of `release_stage_dirty_families`
    - indexed lookup on `release_family_readiness_summaries`
    - no repeated per-family aggregate over `binaries`
- live operator validation:
  - `2026-04-22 19:38:18`: `gonzb --config config.yaml indexer release --once` completed normally
  - release stage logged:
    - `candidate_families=112`
    - `formed=99`
    - `cooled_down_fragment_only_families=21`
    - `stale_cleanup_only_families=0`
    - `skipped_fragments=4`
  - post-run dirty queue state:
    - dirty families total: `0`
    - `release_family`: `0`
    - `base_stem`: `0`
- current interpretation after sign-off:
  - release selection is now driven by dirty rows plus precomputed readiness summary state instead of repeated family aggregation work
  - release-stage behavior remained consistent with the prior queue policy:
    - actionable families formed releases
    - fragment-only families cooled down
    - no stale-cleanup regression was observed
  - WorkStream 3 removed the primary remaining structural selector cost center identified after WorkStream 2

WorkStream 3 objective:

- make release candidate selection proportional to dirty-queue width plus a single keyed summary lookup
- preserve current release safety behavior and candidate ordering semantics while removing repeated lateral aggregate scans
- keep stale cleanup and fragment cooldown visible without letting them dominate normal actionable passes

Summary-state design to land:

- add a family-readiness summary table keyed by:
  - `provider_id`
  - `newsgroup_id`
  - `key_kind`
  - `family_key`
- store enough materialized state to answer candidate ordering cheaply:
  - `source_release_key`
  - compatibility `release_key`
  - representative `release_name`
  - `binary_count`
  - `complete_binary_count`
  - `incomplete_binary_count`
  - expected-file-count evidence present or absent
  - `total_bytes`
  - earliest posted-at value
  - summary `updated_at`
  - readiness bucket
- readiness bucket definitions:
  - `actionable`
    - at least one complete binary exists for the family
  - `fragment_only`
    - one or more binaries exist but none are complete yet
  - `stale_cleanup_only`
    - no binary rows currently remain for the dirty family key

Maintenance rules:

- maintain summary state transactionally from the binary write paths that already control queue dirtiness:
  - `UpsertBinary`
  - `RefreshBinaryStats`
- refresh both key shapes used by release selection:
  - `release_family`
  - `base_stem`
- when a binary moves families because identity improves, refresh both:
  - the old summary key
  - the new summary key
- continue to dirty-queue both old and new family keys when a move happens so stale cleanup and release reform stay correct
- when a refreshed family no longer has any binaries, remove its summary row and let dirty-queue processing treat it as `stale_cleanup_only`

Selector behavior to preserve:

- `ListReleaseCandidates` should read:
  - a bounded dirty-family queue window
  - one keyed summary lookup per queued family
- candidate ordering should remain intentionally biased:
  - actionable families first
  - then zero-binary stale-cleanup families
  - then fragment-only cooldown rows
- within actionable work, continue preferring:
  - more complete binaries
  - expected-file-count evidence
  - oldest dirty rows first
- do not change:
  - release persistence thresholds
  - fragment-only cooldown behavior
  - stale release cleanup rules

Backfill and migration scope:

- add a schema migration that:
  - creates the summary table
  - seeds summary rows from existing `binaries`
  - supports both `release_family` and `base_stem` key kinds
- use grouped backfill SQL in the migration so the selector can switch to summary-backed reads immediately after migration
- keep the new table small and replaceable:
  - it is operational summary state, not new catalog identity

Validation plan for this workstream:

- repository-level validation:
  - candidate ordering remains the same for:
    - complete versus fragment-only families
    - expected-file-count evidence tie-breaks
    - zero-binary stale cleanup rows
  - summary rows are created and refreshed from:
    - `UpsertBinary`
    - `RefreshBinaryStats`
  - family-move cases refresh old and new summary keys safely
- live DB validation after landing:
  - compare `EXPLAIN (ANALYZE, BUFFERS)` for `ListReleaseCandidates` before and after
  - confirm queue-window selection no longer performs repeated family aggregates from `binaries`
  - query dirty rows joined to summary state and verify bucket distribution matches the prior aggregate-driven view
- operator validation:
  - rerun repeated `gonzb --config config.yaml indexer release --once`
  - compare:
    - candidate-family throughput
    - formed-release totals
    - cooled-down fragment-only family counts
    - stale-cleanup-only family counts
    - wall-clock runtime across several repeated release passes

Expected result:

- release selection cost becomes more proportional to queue rows than to repeated family aggregation work
- actionable families surface more reliably even when the dirty queue is large
- fragment-only cooldown remains intact without dominating normal release passes
- summary maintenance overhead stays bounded and localized to already-hot binary update transactions

WorkStream 3 exit criteria:

- `ListReleaseCandidates` reads dirty rows plus summary rows rather than recomputing readiness from `binaries`
- current repository tests covering queue ordering still pass, with new coverage for summary maintenance
- live DB inspection confirms summary rows correctly represent actionable, fragment-only, and stale-cleanup outcomes
- repeated `release --once` validation shows no behavioral regression in formed-release, fragment cooldown, or stale cleanup accounting
- the remaining release-stage cost center is no longer repeated family aggregation work

### 4. Queue-quality validation and backlog burn-down operations

This plan assumes active validation while performance changes land.

Status:

- WorkStream 4 completed and signed off on `2026-04-22`
- this workstream is responsible for converting the prior selector and queue changes into:
  - measurable operator evidence
  - backlog-burn-down checkpoints
  - a go or no-go call on the remaining refinement-phase exit criteria

WorkStream 4 objective:

- prove that the combined WorkStream 1 through 3 changes produce sustained backlog movement rather than one-off anecdotal wins
- validate queue quality using the current live DB state, not just repository tests or isolated `EXPLAIN`
- document whether the broader refinement loop is now ready for sign-off or still needs more soak

Operator loop:

- validate repeated `assemble --once` and `release --once` runs instead of relying on single-pass anecdotes
- compare:
  - processed headers per minute
  - refreshed binaries per minute
  - completed binaries per hour
  - formed releases per hour
  - dirty-family composition by readiness bucket
- continue using operator queries from `docs/archive/development/indexer/INDEXER_TEST_QUERIES.md` to inspect:
  - near-complete releases
  - missing binary parts
  - pending matching headers
  - stale or partially unlinked release materialization

Execution approach:

- capture a before-state at the start of the validation window:
  - pending headers
  - lane-A-actionable pending headers
  - near-complete release count
  - dirty-family count and readiness-bucket composition
- run a bounded repeated operator loop:
  - `assemble --once`
  - `release --once`
  - repeat enough times to observe whether throughput and queue behavior remain stable after the first drain
- capture after-state and compare:
  - pending-header delta
  - near-complete-release delta
  - dirty-queue delta
  - release pass outcomes:
    - formed
    - cooled-down fragment-only families
    - stale-cleanup-only families
- use the current stage logs as the authoritative runtime evidence for:
  - lane A versus lane B selection
  - processed headers
  - binaries refreshed
  - release candidate counts
  - formed release counts

What this workstream must answer:

- is the assemble backlog still moving down after the earlier targeted fixes, not just during a single cherry-picked pass
- is lane A still materially active when pending matching headers exist
- do release passes still spend most of their effort on actionable work instead of fragment churn
- is the near-complete release pool shrinking, stabilizing, or merely cycling
- can the active refinement loop now be signed off with honest evidence

Documentation changes expected from this workstream:

- record a concrete before/after snapshot and repeated-run evidence directly in this plan
- update `docs/archive/development/indexer/INDEXER_TEST_QUERIES.md` if any operator queries are still missing for:
  - backlog rate checks
  - summary-backed dirty-queue composition
  - near-complete release follow-up
- update the baseline refinement plan if the broader phase exit criteria are now met

Exit criteria for WorkStream 4:

- repeated live `assemble --once` and `release --once` passes are recorded with timing and outcome evidence
- the active plan contains before/after backlog snapshots and queue-quality interpretation
- refinement-phase exit criteria are explicitly evaluated against the live evidence
- the next action is unambiguous:
  - either refinement is signed off
  - or the remaining blocker is named and bounded

WorkStream 4 completion note:

- before-state at the start of the bounded validation window:
  - pending headers: `640932`
  - lane-A-actionable pending headers: `71849`
  - near-complete releases (`90%` to `99%`): `92`
  - dirty families: `0`
  - complete binaries: `7386`
  - incomplete binaries: `63506`
- bounded live soak executed on `2026-04-22`:
  - assemble passes:
    - `19:45:20` to `19:45:43`: `22s`, `lane_a_selected=410`, `lane_b_selected=2090`, `processed_headers=2500`, `binaries_refreshed=422`
    - `19:47:37` to `19:48:02`: `25s`, `lane_a_selected=399`, `lane_b_selected=2101`, `processed_headers=2500`, `binaries_refreshed=411`
    - `19:49:30` to `19:49:51`: `21s`, `lane_a_selected=399`, `lane_b_selected=2101`, `processed_headers=2500`, `binaries_refreshed=410`
  - release passes:
    - `19:46:03` to `19:47:36`: `94s`, `candidate_families=161`, `formed=137`, `cooled_down_fragment_only_families=43`, `stale_cleanup_only_families=0`
    - `19:48:03` to `19:49:27`: `84s`, `candidate_families=157`, `formed=136`, `cooled_down_fragment_only_families=42`, `stale_cleanup_only_families=0`
    - `19:49:52` to `19:51:21`: `88s`, `candidate_families=158`, `formed=137`, `cooled_down_fragment_only_families=42`, `stale_cleanup_only_families=0`
- measured throughput across the bounded soak:
  - assemble average runtime: `22.67s`
  - assemble throughput: about `6618` headers/minute
  - binary refresh throughput: about `1097` binaries/minute
  - release average runtime: `88.67s`
  - formed-release throughput across the three release passes: about `5549` releases/hour
- after-state at the end of the bounded validation window:
  - pending headers: `633432`
  - pending-header delta: `-7500`
  - lane-A-actionable pending headers: `70119`
  - near-complete releases (`90%` to `99%`): `93`
  - dirty families: `0`
  - complete binaries: `7418`
  - incomplete binaries: `63505`
  - formed releases currently at `100%` completion: `223`
- operator interpretation after sign-off:
  - assemble backlog is moving down at a sustained rate under repeated live one-shot passes
  - lane A remains materially active and continues to consume progress-improving work while tens of thousands of matching pending headers still exist
  - release passes are consistently spending most of their effort on actionable work:
    - `136` to `137` formed releases per pass
    - `42` to `43` fragment-only cooldowns per pass
    - dirty families drained back to `0` after the bounded soak
  - fragment-only queue churn is no longer dominating normal release runs
  - the near-complete release pool did not shrink during this short validation window:
    - `92` to `93`
  - that near-complete pool now looks more like a follow-up inspection and catalog-quality task than a throughput or queue-policy blocker
- result of the WorkStream 4 decision point:
  - the refinement-phase exit criteria from `docs/archive/completed/indexer/INDEXER_ASSEMBLE_AND_RELEASE_REFINEMENT_PLAN.md` are now satisfied from live validation
  - no new selector or queue-policy blocker was discovered during the bounded soak
  - follow-up work, if any, should target near-complete release inspection rather than reopening the completed throughput workstreams

## Immediate Action Order

1. completed on `2026-04-22`: PostgreSQL/runtime tuning baseline plus fresh `VACUUM ANALYZE` on the hot tables
2. completed on `2026-04-22`: assemble lane A reworked into a bounded binary-driven completion lane with a separate fresh-work lane and live exit validation
3. completed on `2026-04-22`: added release family summary state and switched `ListReleaseCandidates` to summary-backed selection
4. completed on `2026-04-22`: reran broader live validation and updated the refinement plan status based on measured churn
5. next: decide whether near-complete release follow-up belongs in a narrow inspection-quality plan or can be deferred behind the next active phase

## Relationship To Other Docs

- `docs/active/INDEXER_FOUNDATION_DOCS.md`
  - source of truth for which docs are active
- `docs/active/INDEXER_NEXT_PHASE_ROADMAP.md`
  - top-level sequencing and Phase 3 gate
- `docs/archive/completed/indexer/INDEXER_ASSEMBLE_AND_RELEASE_REFINEMENT_PLAN.md`
  - baseline refinement history and previously landed work
- `docs/archive/development/indexer/INDEXER_TEST_QUERIES.md`
  - operator validation commands and DB inspection queries

## Exit Direction

This plan should be considered successful when:

- assemble backlog is burning down at a sustained rate
- assemble is clearly spending most priority work on binary-completion progress
- release queue passes regularly find actionable work without large fragment-only scan overhead
- the near-complete release pool trends toward `100%` completion or clear cooldown outcomes
- the active refinement loop can be signed off and moved out of the active-doc set
