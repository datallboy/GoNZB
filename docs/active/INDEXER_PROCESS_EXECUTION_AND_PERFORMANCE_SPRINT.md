# Indexer Process Execution And Performance Sprint

Snapshot date: 2026-04-29

This is the active execution plan for the current indexer performance sprint.

The sprint goal is to reduce assemble runtime, improve pending-header backlog burn-down, and safely introduce real concurrency for indexer stages where it helps. The primary focus is `assemble`, especially candidate selection quality and execution throughput. `release` concurrency is in scope for evaluation and follow-up work, but it is not the current dominant bottleneck.

## Current Baseline

Baseline gathered from the current repo state and the live local `gonzb-postgres` dev database on `2026-04-29`.

### Architecture and runtime baseline

- GoNZB is currently a modular monolith with a single binary and an internal runtime supervisor.
- The usenet indexer runtime starts one supervisor inside the main process.
- Stage-level `concurrency` exists in config and runtime state, but it is not yet used to fan out actual concurrent workers for `assemble` or `release`.
- Stage runtime claims are currently stage-wide, single-owner claims. That means multiple OS processes would still serialize per stage unless claim behavior changes.

### Current live workload baseline

- pending unassembled headers: `18,257,429`
- dirty release families: `5`
- complete binaries: `33,789`
- incomplete binaries: `110,666`

### Current recent stage timing baseline

- recent successful `assemble` runs:
  - about `34s` to `39s`
  - current batch size `2500`
- recent successful `release` runs:
  - about `0.3s` to `4.1s`
  - current batch size `1000`
- one failed long-running `assemble` run lasted about `16.5` hours and ended on `context canceled`

### Current stage behavior observations

- `assemble` is the active throughput bottleneck
- `release` is currently cheap enough that it is not the lead problem
- recent `assemble` metrics show path A is barely contributing on the current dataset:
  - `lane_a_selected` around `2` to `3`
  - `lane_b_selected` around `2497` to `2498`
- this means the current path A strategy is not materially moving the backlog and must be reevaluated

## Sprint Decisions

### Decision 1. Keep the single-binary architecture for now

The codebase should remain a single binary and modular monolith during this sprint.

Reason:

- current evidence points to `assemble` query shape, execution flow, and missing worker parallelism
- current evidence does not yet show that the main blocker is the single-process architecture itself
- the repo already has clean module boundaries and a runtime supervisor that can be extended

### Decision 2. Add real concurrency inside the current runtime first

Use goroutine-based worker concurrency inside the current Go process before introducing cross-process worker scaling.

Reason:

- it is the lowest-risk way to make the existing `concurrency` setting real
- goroutines are cheap and allow concurrent DB work, NNTP work, and CPU work inside one process
- if the work is DB-safe and claim-safe, the same model can later be extended to multiple OS processes

### Decision 3. Make concurrency claim-driven, not optimistic

Do not let multiple workers race on the same pending headers or dirty families.

Required safety model:

- work must be partitioned by database-backed claiming or leasing
- workers must only process rows they have explicitly claimed
- claim semantics must work both in-process and across multiple processes

### Decision 4. Assemble is Milestone 1 priority

The first implementation focus is `assemble`.

Reason:

- `assemble` dominates current runtime cost
- pending backlog is very large
- path A selection is currently too weak to matter
- release throughput improvements will not help much if assemble remains the long pole

## Milestone 1. Baseline And Instrument Assemble Properly

Goal:

- make the hot assemble path measurable enough to know where time is going before changing execution topology

Status:

- [x] complete

Tasks:

- [x] add stage metrics for assemble candidate-selection time, header-match time, binary upsert time, binary-part upsert time, and binary-refresh time
- [x] add derived throughput metrics for headers per second and refreshed binaries per second
- [x] capture `EXPLAIN (ANALYZE, BUFFERS)` for the current assemble selector queries on the current dev DB
- [x] capture `EXPLAIN (ANALYZE, BUFFERS)` for the binary refresh path if it remains a significant share of runtime
- [x] document the new baseline numbers in this file after the first measurement pass
- [x] verify whether path A is expensive but low-yield, or cheap but irrelevant, on the current `18M+` pending-header backlog

Acceptance criteria:

- [x] we can separate selector cost from row-processing cost
- [x] we can explain where the majority of assemble time is spent
- [x] we have enough evidence to choose between query work, worker concurrency, and write-path batching

### Milestone 1 measurement pass

Measured on `2026-04-29` against the live local `gonzb-postgres` database after adding assemble timing metrics.

Instrumented assemble run:

- run id: `61938`
- pending headers before run: `18,257,429`
- selected headers: `2,500`
- processed headers: `2,500`
- binaries refreshed: `19`
- total runtime: `25.085s`
- headers per second: `99.68`
- refreshed binaries per second: `0.76`
- lane A selected: `3`
- lane B selected: `2,497`

Runtime split from saved stage metrics:

- pending count: `1.312s`
- candidate selection: `3.529s`
- header match, including yEnc recovery attempt: `0.923s`
- binary upsert: `12.569s`
- binary-part upsert plus article-header assembled mark: `6.644s`
- binary refresh: `0.099s`

Selector `EXPLAIN (ANALYZE, BUFFERS)` findings:

- path A priority-binary selector scanned `110,670` incomplete binaries, produced `91,654` ranked file identities, and returned the top `2,000` in `817ms`; plan used a sequential scan on `binaries`, window ranking, and top-N sort.
- path A pending-header discovery across the top `2,000` priority binaries returned only `32` available headers in `2.736s`; most work was repeated indexed lookup by normalized `subject_file_name` followed by article-header checks.
- lane B recent pending-header selector returned a `49,940`-row recent window in `243ms`; this path is much cheaper and feeds almost all selected work today.
- binary refresh was only `99ms` total for `19` touched binaries, so the refresh-path `EXPLAIN` is not required for this milestone unless later concurrency or batching changes make it significant.

Milestone 1 conclusion:

- `assemble` remains the primary bottleneck; current release timings do not justify moving focus away from assemble.
- The majority of measured assemble time is row-processing and write-path work, not selector time: binary upsert plus binary-part upsert accounted for about `19.2s` of the `25.1s` run.
- Path A is expensive relative to its yield: the current normalized-filename progress selector spent about `2.7s` to find only dozens of usable candidates and selected only `3` headers in the measured run. It should be reworked in Milestone 2, but it is not the main runtime sink.
- Safe multi-worker assemble is not release-ready with the current selector because workers would share unclaimed `assembled_at IS NULL` rows. Milestone 3 must add database-backed ownership, preferably `FOR UPDATE SKIP LOCKED` chunk claiming or equivalent lease columns/claim table, before enabling `assemble.concurrency > 1`.
- Single-binary modular-monolith design remains valid; current evidence points to selector quality, DB-safe claiming, and write-path batching before any process split.

Milestone 1 sign-off:

- Complete. The next safe work is Milestone 2 path A redesign plus Milestone 3 claim-model design; do not release multi-worker assemble until DB-backed claiming is implemented.

## Milestone 2. Rework Assemble Candidate Selection With Focus On Path A

Goal:

- materially improve selection quality so assemble spends more time on work that accelerates binary completion and release readiness

Status:

- [ ] not started

Tasks:

- [ ] review the current path A binary-priority selector against the live backlog characteristics
- [ ] measure how many lane A candidates are available and how long the current lane A discovery query takes
- [ ] redesign path A if the current normalized-filename match strategy is too sparse for the real workload
- [ ] evaluate whether path A should pivot from current file-name identity matching toward binary-progress or multipart-readiness heuristics
- [ ] ensure the new selector still preserves deterministic ordering and avoids starvation of fresh work
- [ ] keep lane-level metrics so we can compare old and new path A contribution

Acceptance criteria:

- path A contributes a meaningful portion of the batch on the live dev backlog, or is intentionally replaced by a better prioritization strategy
- selector cost remains acceptable at current backlog scale
- assemble gets a measurable throughput improvement even before multi-worker fan-out lands

## Milestone 3. Make Assemble Concurrency Real In One Process

Goal:

- turn the unused `assemble.concurrency` setting into real concurrent workers inside the current process

Status:

- [ ] not started

Tasks:

- [ ] define a database-backed claim model for pending assembly work
- [ ] choose one of these safe claim patterns:
  - row claiming on `article_headers` with lease columns
  - dedicated assembly-claim side table
  - transaction-scoped selection with `FOR UPDATE SKIP LOCKED`
- [ ] implement a worker pool driven by goroutines for assemble
- [ ] ensure each worker only receives claimed rows and cannot process the same header as another worker
- [ ] batch work into explicit claimed chunks so cancellation and restart behavior remains understandable
- [ ] preserve stage-level metrics while adding per-worker metrics where useful
- [ ] make sure stage shutdown cancels workers cleanly and does not leave claims stuck forever
- [ ] add maintenance or claim-repair behavior if claims can become stale

Acceptance criteria:

- `assemble.concurrency > 1` causes real parallel work
- duplicate processing of the same header does not occur
- cancellation, restart, and stale-claim recovery are deterministic
- throughput improves without corrupting binary or part state

## Milestone 4. Make Assemble Writes And Refreshes Scale

Goal:

- remove write-path and refresh-path costs that would blunt the value of worker concurrency

Status:

- [ ] not started

Tasks:

- [ ] evaluate whether per-header `UpsertBinaryPart` plus per-binary refresh is the dominant cost once workers are added
- [ ] batch binary-stat refreshes for all touched binaries instead of refreshing each binary in a separate transaction where possible
- [ ] reduce transaction churn in the hot assemble path
- [ ] confirm that dirty-family summary refresh work does not become the new bottleneck after assemble fan-out
- [ ] re-measure assemble runtime after batching changes

Acceptance criteria:

- worker concurrency produces net speedup instead of moving the bottleneck into write amplification
- binary stats and release-family summaries stay correct under concurrent assemble workers

## Milestone 5. Evaluate Release Multi-Worker Concurrency

Goal:

- determine whether release should also support multiple workers after assemble improvements land

Status:

- [ ] not started

Tasks:

- [ ] re-measure `release` after assemble throughput improves
- [ ] determine whether release runtime becomes materially expensive as dirty-family volume grows again
- [ ] if needed, define family-level claim semantics for release candidates
- [ ] ensure workers cannot form the same family concurrently
- [ ] verify that release claims remain compatible with stale-cleanup-only and fragment-cooldown behavior

Acceptance criteria:

- we have a clear yes or no on release multi-worker implementation for this sprint
- if implemented, `release.concurrency` becomes real and family-safe

## Milestone 6. Optional Cross-Process Worker Topology

Goal:

- support scale-out of assemble and release workers across multiple OS processes without splitting the codebase into separate products

Status:

- [ ] not started

Tasks:

- [ ] keep the internal supervisor as the default all-in-one runtime
- [ ] make sure the claim model from Milestones 3 and 5 is process-safe, not just goroutine-safe
- [ ] support running dedicated `assemble` or `release` worker processes using the same binary and config
- [ ] define operator rules for when single-process is sufficient and when separate worker processes are useful
- [ ] document the recommended topology choices for dev, lower-end self-hosted, and stronger production environments

Acceptance criteria:

- the same binary can run either all-in-one or with dedicated worker processes
- concurrency safety comes from the database claim model, not from assumptions about process boundaries

## Concurrency Strategy Notes

### Why goroutines help in a single-process binary

Goroutines still allow real concurrency even when everything runs inside one binary.

They help because:

- while one worker is waiting on PostgreSQL, another worker can keep working
- while one worker is doing matcher or marshaling work, another can issue DB writes
- Go can schedule many independent workers across multiple CPU cores
- the single process keeps simpler deployment, logging, and configuration while still using available CPU and I/O parallelism

This is useful here because assemble is not purely CPU-bound and not purely DB-bound. It is a mixed workload with selection, matching, writes, and refresh work.

### Thread safety and race-condition rules

Goroutines are safe only if the work partitioning is safe.

The safety rule for this sprint:

- never rely on in-memory coordination alone for row ownership
- always rely on database-backed claims or row locks for ownership of pending work

That means:

- two goroutines in one process cannot process the same claimed header batch
- two separate processes also cannot process the same claimed header batch
- the correctness model is the same whether concurrency is in-process or cross-process

### Required database-safety properties

The final design must ensure:

- each pending header is claimed once per attempt window
- claims expire or can be repaired after worker crashes
- release-family work is acknowledged exactly once after successful handling
- stale claims can be repaired by maintenance
- unique constraints and idempotent upserts remain the last line of defense if a claim edge case slips through

## Working Validation Checklist

- [ ] baseline metrics captured after instrumentation lands
- [ ] new path A or replacement prioritization validated on the live dev backlog
- [ ] `assemble.concurrency` used by real workers
- [ ] no duplicate header processing under concurrency tests
- [ ] no binary corruption or release-family summary drift under concurrency tests
- [ ] `release` concurrency evaluated after assemble improvements
- [ ] optional cross-process worker path either implemented or explicitly deferred with evidence

## References

- `docs/ARCHITECTURE.md`
- `docs/INDEXER_HOW_IT_WORKS.md`
- `docs/INDEXER_POSTGRES_RUNTIME_TUNING.md`
- `docs/INDEXER_TEST_QUERIES.md`
- `docs/archive/completed/indexer/INDEXER_ASSEMBLE_AND_RELEASE_REFINEMENT_PLAN.md`
- `docs/archive/completed/indexer/INDEXER_BACKLOG_BURNDOWN_PERFORMANCE_PLAN.md`
