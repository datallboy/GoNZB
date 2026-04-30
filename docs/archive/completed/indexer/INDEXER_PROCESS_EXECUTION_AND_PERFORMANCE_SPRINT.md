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

- [x] complete

Tasks:

- [x] review the current path A binary-priority selector against the live backlog characteristics
- [x] measure how many lane A candidates are available and how long the current lane A discovery query takes
- [x] redesign path A if the current normalized-filename match strategy is too sparse for the real workload
- [x] evaluate whether path A should pivot from current file-name identity matching toward binary-progress or multipart-readiness heuristics
- [x] ensure the new selector still preserves deterministic ordering and avoids starvation of fresh work
- [x] keep lane-level metrics so we can compare old and new path A contribution

Acceptance criteria:

- [x] path A contributes a meaningful portion of the batch on the live dev backlog, or is intentionally replaced by a better prioritization strategy
- [x] selector cost remains acceptable at current backlog scale
- [x] assemble gets a measurable throughput improvement even before multi-worker fan-out lands

### Milestone 2 path A selector pass

Implemented on `2026-04-29`.

Decision:

- Keep the two-lane selector model.
- Path A remains the completion lane for binaries/releases that already exist.
- Path B remains the fresh-work lane that forms new binaries/releases from recent pending headers.
- Keep the adjustable lane split in `internal/store/pgindex/assembly_store.go`; the current setting gives path A about `70%` of the batch and path B about `30%`.

Reason:

- The model is still correct for backlog burn-down: as incomplete binary/release backlog grows, path A should receive more attention.
- The problem found in Milestone 1 was not the path A concept. The old selector ranked incomplete binaries first, then looked for matching pending headers, which spent seconds on many binaries that had no available pending parts.

Selector change:

- Path A now starts from a bounded recent window of pending structured headers, then joins those headers to incomplete binaries by normalized file identity.
- It ranks matching headers by main-payload preference, binary completion ratio, observed parts, binary id, and header id.
- It still falls back to path B for the rest of the batch, so fresh release formation is not starved.

Measurement:

- pre-change instrumented run: `lane_a_selected=3`, `lane_b_selected=2497`, selector `3.529s`, total `25.085s`, `99.68` headers/sec
- path A availability query against a `70,000` pending-header window found `9,505` usable path A candidates in about `1.4s`
- post-change instrumented run: `lane_a_selected=1750`, `lane_b_selected=750`, selector `2.407s`, total about `23.39s`, `106.88` headers/sec

Milestone 2 conclusion:

- Path A is worth keeping and is now doing the intended job on the live backlog.
- The current `70/30` lane split is appropriate for the measured backlog because path A can fill its allocation.
- If path A availability drops below its allocation, the existing fallback naturally gives unused capacity back to path B.
- Further runtime reduction should now focus on DB-safe claiming and write-path amplification, not more selector churn.

Milestone 2 sign-off:

- Complete. The selector now materially contributes to completing existing binaries while preserving fresh-work progress.

## Later Milestone Action Plan From Milestones 1 And 2

Upsert tuning direction:

- The hot cost is write amplification, especially `UpsertBinary` repeated once per header even when a batch touches only a small number of binaries.
- In the measured Milestone 2 run, `2,500` headers touched only `32` binaries, but the service still called the binary upsert path per header.
- Completed before Milestone 3: batch-local binary identity caching now upserts each unique `(provider_id, newsgroup_id, binary_key)` once per batch or claimed chunk, then reuses the returned binary id for that chunk's parts.
- Completed before Milestone 3: binary-part writes and article-header assembled marks now use one batch transaction with set-based `INSERT ... ON CONFLICT` and set-based `UPDATE article_headers`.
- The second target should be deferring release-family summary refresh out of per-header binary upserts. Mark dirty families cheaply during assemble, then refresh summaries once per touched family after the chunk or in release processing.
- Keep `RefreshBinaryStats` batch-level or chunk-level; it is cheap today, but it must remain correct under concurrency.

Write-path batching measurement:

- run id: `61941`
- lane A selected: `1,750`
- lane B selected: `750`
- processed headers: `2,500`
- unique binary upserts: `25`
- binary upsert cache hits: `2,475`
- binary part batch size: `2,500`
- total runtime: `3.577s`
- headers per second: `701.08`
- candidate selection: `1.292s`
- binary upsert: `0.159s`
- binary-part batch upsert plus assembled mark: `0.366s`
- binary refresh: `0.143s`

Write-path conclusion:

- Batching/de-amplification removed the dominant Milestone 1 bottleneck before adding worker concurrency.
- Milestone 3 can now focus on DB-backed row ownership and real worker fan-out; otherwise multiple workers would have amplified the old per-header write cost.

Milestone 3 action:

- Add DB-backed ownership before enabling multiple assemble workers.
- Prefer transaction-scoped chunk claiming with `FOR UPDATE SKIP LOCKED` for the first implementation because it avoids stale lease columns while proving worker safety.
- If workers need long-lived claims across NNTP recovery or large chunks, promote to lease columns or a side table with maintenance cleanup.
- Preserve the path A/path B selector semantics inside the claim query so workers claim disjoint rows from the same lane policy.

Milestone 4 action:

- Implement binary upsert de-amplification and part-write batching after claims exist, so each worker owns a chunk and can safely batch within it.
- Measure whether release-family summary refresh or dirty-family writes become the next bottleneck after binary upsert calls are reduced.
- Keep unique constraints and idempotent upserts as the final correctness guard under worker concurrency.

## Milestone 3. Make Assemble Concurrency Real In One Process

Goal:

- turn the unused `assemble.concurrency` setting into real concurrent workers inside the current process

Status:

- [x] complete

Tasks:

- [x] define a database-backed claim model for pending assembly work
- [ ] choose one of these safe claim patterns:
  - row claiming on `article_headers` with lease columns
  - dedicated assembly-claim side table
  - transaction-scoped selection with `FOR UPDATE SKIP LOCKED`
- [x] choose one of these safe claim patterns:
  - row claiming on `article_headers` with lease columns
  - dedicated assembly-claim side table
  - transaction-scoped selection with `FOR UPDATE SKIP LOCKED`
- [x] implement a worker pool driven by goroutines for assemble
- [x] ensure each worker only receives claimed rows and cannot process the same header as another worker
- [x] batch work into explicit claimed chunks so cancellation and restart behavior remains understandable
- [x] preserve stage-level metrics while adding per-worker metrics where useful
- [x] make sure stage shutdown cancels workers cleanly and does not leave claims stuck forever
- [x] add maintenance or claim-repair behavior if claims can become stale

Acceptance criteria:

- [x] `assemble.concurrency > 1` causes real parallel work
- [x] duplicate processing of the same header does not occur
- [x] cancellation, restart, and stale-claim recovery are deterministic
- [x] throughput improves without corrupting binary or part state

### Milestone 3 assemble claim and worker pass

Implemented on `2026-04-29`.

Claim model:

- Added `article_headers.assembly_claimed_by` and `article_headers.assembly_claimed_until`.
- Assemble now claims rows before processing them and clears claims when batched part writes mark headers assembled.
- Active claims are excluded from path A and path B selection.
- Claims are lease based, so canceled or crashed workers do not leave rows stuck forever; expired claims naturally become selectable again.

Worker model:

- `assemble.concurrency` now drives real goroutine fan-out inside the current process.
- The service claims the full batch once with database-backed ownership, then splits the claimed headers across workers.
- Workers only receive already claimed rows, so worker ownership does not rely on in-memory selection races.
- Stage metrics now include `worker_count`, `unique_binary_upserts`, `binary_upsert_cache_hits`, and `binary_part_batch_size`.

Validation:

- A first claim-per-worker test run proved the safety model but underfilled the batch because workers raced on the same top candidate window.
- The implementation was adjusted to claim the full batch once, then fan out claimed rows.
- Live `assemble.concurrency=4` validation run:
  - run id: `61945`
  - selected headers: `2,500`
  - processed headers: `2,500`
  - worker count: `4`
  - lane A selected: `1,750`
  - lane B selected: `750`
  - unique binary upserts: `49`
  - binary upsert cache hits: `2,451`
  - total runtime: `3.135s`
  - headers per second: `800.89`
  - candidate selection: `1.824s`
  - binary upsert: `0.342s`
  - binary-part batch upsert plus assembled mark: `0.284s`

Milestone 3 conclusion:

- Real assemble concurrency is now enabled without relying on in-memory ownership.
- The single-binary modular-monolith design remains appropriate; safe concurrency is handled by database claims and in-process workers.
- Further gains should come from Milestone 4 write-path refinement, especially reducing release-family summary work inside binary upsert.

Milestone 3 sign-off:

- Complete. `assemble.concurrency > 1` is safe to use with the lease-backed claim model.

### Batch size and concurrency tuning pass

Measured on `2026-04-29` after claimed workers and batched writes landed.

Implementation note:

- `assemble.batch_size` is the total stage-run batch size.
- Workers split the one DB-claimed batch; each worker does not independently claim its own full `batch_size`.
- Running unique selector/claim batches per worker was tested during Milestone 3 and underfilled before the claim-once model; it also repeats selector cost. The safer equivalent of "larger unique worker batches" is to increase total `batch_size`, claim once, and split the claimed rows.
- Large single-worker batches exposed PostgreSQL's extended protocol parameter limit, so `UpsertBinaryParts` now chunks large batched writes inside one transaction.

Tuning results:

| Batch size | Concurrency | Processed | Headers/sec | Selector ms | Notes |
| --- | ---: | ---: | ---: | ---: | --- |
| `2500` | `1` | `2500` | `675.52` | `1622.6` | current config baseline after batching |
| `5000` | `1` | `5000` | `992.37` | `2137.6` | better amortization |
| `7500` | `1` | `7500` | `1307.40` | `2293.8` | good single-worker scaling |
| `10000` | `1` | `10000` | `1532.20` | `2600.9` | requires chunked part writes |
| `2500` | `4` | `2500` | `866.18` | `1809.0` | modest gain, too little work per worker |
| `5000` | `4` | `5000` | `1405.28` | `2125.7` | strong improvement |
| `7500` | `4` | `7500` | `1565.68` | `2908.0` | moderate improvement |
| `10000` | `4` | `10000` | `1912.80` | `2687.2` | best measured balance |
| `15000` | `4` | `15000` | `1617.65` | `6551.0` | selector cost rose sharply |
| `20000` | `4` | `20000` | `1867.46` | `7236.6` | close, but not better than `10000/4` |
| `10000` | `2` | `10000` | `1423.80` | `3595.5` | worse than one worker in this run |
| `10000` | `8` | `10000` | `1823.26` | `3231.2` | below `10000/4`, more contention |
| `20000` | `8` | `20000` | `1807.26` | `7216.0` | no gain over `20000/4` |

Tuning conclusion:

- Increasing total batch size does improve overall throughput by amortizing fixed count/selector/run overhead.
- `batch_size=10000` and `concurrency=4` is the best measured balance so far on the live dev database.
- Higher concurrency is not automatically better. `8` workers increased aggregate write/match contention and did not beat `4`.
- Larger batches such as `20000/4` are viable, but the selector cost grows and measured throughput was slightly below `10000/4`.
- Recommended next config trial: set assemble to `batch_size=10000`, `concurrency=4`, then observe several scheduled runs for consistency before increasing further.

## Milestone 4. Make Assemble Writes And Refreshes Scale

Goal:

- remove write-path and refresh-path costs that would blunt the value of worker concurrency

Status:

- [x] complete

Tasks:

- [x] evaluate whether per-header `UpsertBinaryPart` plus per-binary refresh is the dominant cost once workers are added
- [x] batch binary-stat refreshes for all touched binaries instead of refreshing each binary in a separate transaction where possible
- [x] reduce transaction churn in the hot assemble path
- [x] confirm that dirty-family summary refresh work does not become the new bottleneck after assemble fan-out
- [x] re-measure assemble runtime after batching changes

Acceptance criteria:

- [x] worker concurrency produces net speedup instead of moving the bottleneck into write amplification
- [x] binary stats and release-family summaries stay correct under concurrent assemble workers

### Milestone 4 write and refresh scaling pass

Implemented on `2026-04-29`.

Changes already completed before this pass:

- Batch-local binary upsert caching reduced `UpsertBinary` calls from per-header to per-unique-binary.
- `UpsertBinaryParts` batches part writes and article-header assembled marks.
- Large part batches are chunked inside one transaction to stay under PostgreSQL's extended-protocol parameter limit.

Additional Milestone 4 change:

- `RefreshBinaryStatsBatch` now refreshes all touched binaries for a worker chunk inside one transaction instead of starting one transaction per binary.
- The old single-binary `RefreshBinaryStats` API remains as a wrapper for compatibility and tests.
- Follow-up concurrent backlog building exposed two PostgreSQL deadlocks in `release_family_readiness_summaries` while four assemble workers refreshed overlapping family rows.
- `RefreshBinaryStatsBatch` now updates binary rows in deterministic binary-id order, collects unique release-family summary keys, and refreshes/marks those family rows once per worker chunk in deterministic key order.

Measurement:

- Recommended measured setting remained `batch_size=10000`, `concurrency=4`.
- Post-refresh-batching assemble run:
  - run id: `61962`
  - processed headers: `10,000`
  - worker count: `4`
  - lane A selected: `7,000`
  - lane B selected: `3,000`
  - unique binary upserts: `122`
  - binary upsert cache hits: `9,878`
  - total runtime: `5.237s`
  - headers per second: `1,913.18`
  - candidate selection: `2.987s`
  - binary upsert: `0.632s`
  - binary-part batch upsert plus assembled mark: `0.850s`
  - binary refresh: `0.451s`

Conclusion:

- The old write amplification bottleneck is resolved for assemble.
- Binary refresh is no longer dominant after batching; selector and matcher/recovery work are now larger shares of assemble runtime.
- Dirty-family summary refresh is not the next assemble throughput bottleneck at the measured `10000/4` setting, but it did need deterministic ordering to be concurrency-safe.
- After deterministic ordering, three additional `10000/4` assemble passes completed without deadlocks.
- Release formation did become materially more expensive after assemble throughput improved: a follow-up release pass formed `120` of `122` candidate families and took `78.625s`, with the dirty-family queue drained to `0`.
- Milestone 5 should now evaluate release concurrency and release write/read costs because release, not assemble write amplification, is the next observed long pole.

Milestone 4 sign-off:

- Complete. Assemble write and refresh scaling is sufficient for the current sprint baseline; continue with release concurrency evaluation in Milestone 5.

## Milestone 5. Evaluate Release Multi-Worker Concurrency

Goal:

- determine whether release should also support multiple workers after assemble improvements land

Status:

- [x] complete

Tasks:

- [x] re-measure `release` after assemble throughput improves
- [x] determine whether release runtime becomes materially expensive as dirty-family volume grows again
- [x] if needed, define family-level claim semantics for release candidates
- [x] ensure workers cannot form the same family concurrently
- [x] verify that release claims remain compatible with stale-cleanup-only and fragment-cooldown behavior

Acceptance criteria:

- we have a clear yes or no on release multi-worker implementation for this sprint
- if implemented, `release.concurrency` becomes real and family-safe

Measurement:

- Backlog build: five assemble passes without release left `61` dirty release families.
- Pre-release-write-batching pass:
  - run id: `61984`
  - candidate families: `61`
  - formed releases: `64`
  - files built: `1,474`
  - file article rows: `297,117`
  - total runtime: `45.814s`
  - candidate listing: `0.008s`
  - binary listing: `7.950s`
  - article lookup: `0.346s`
  - replace release files/articles: `36.884s`
- The dominant bottleneck was not release candidate claiming or article lookup; it was row-by-row `release_file_articles` insertion inside `ReplaceReleaseFiles`.

Milestone 5 implementation:

- Release formation now records timing buckets in stage metrics:
  - candidate listing
  - binary listing
  - title candidate listing
  - file building
  - article lookup
  - release upsert
  - release file/article replacement
  - newsgroup replacement
  - NZB cache upsert
  - stale cleanup
  - dirty-family ack
- `buildReleaseFiles` now batches binary-part article lookup per cluster using `ListBinaryPartArticlesBatch`.
- `ReplaceReleaseFiles` now batches `release_file_articles` inserts instead of executing one insert per article row.

Post-change measurement:

- Backlog build: three assemble passes without release left `20` dirty release families.
- Release pass:
  - run id: `61988`
  - candidate families: `20`
  - formed releases: `18`
  - files built: `490`
  - file article rows: `99,889`
  - total runtime: `5.970s`
  - candidate listing: `0.004s`
  - binary listing: `2.715s`
  - article lookup: `0.102s`
  - replace release files/articles: `2.914s`
  - dirty-family queue after release: `0`

Concurrency decision:

- Do not implement release multi-worker concurrency in this sprint yet.
- Evidence points to batched release writes as the correct first fix, not workers.
- If release concurrency is later needed, it must add database-backed family claims on `release_stage_dirty_families` before multiple workers run. A safe shape would mirror assemble:
  - claim a bounded set of dirty family rows with a worker id and lease expiry, or use row-lock claiming with `FOR UPDATE SKIP LOCKED`
  - process only claimed family keys
  - ack/delete only after successful stale-cleanup, fragment-cooldown, or release formation
  - let stale claims expire so another process can retry
- Stale-cleanup-only and fragment-cooldown behavior remain compatible with claims because both paths already end in `AckReleaseCandidate`; the ack would simply need to verify ownership or operate only on claimed rows.

Milestone 5 sign-off:

- Complete. Release became temporarily expensive because assemble throughput fed it larger dirty-family batches, but timing showed the bottleneck was row-by-row article persistence. After batching, release drained a 20-family backlog in `5.970s`; release multi-worker concurrency is explicitly deferred until measurements show batching is insufficient.

## Milestone 6. Optional Cross-Process Worker Topology

Goal:

- support scale-out of assemble and release workers across multiple OS processes without splitting the codebase into separate products

Status:

- [x] signed off as deferred

Tasks:

- [x] keep the internal supervisor as the default all-in-one runtime
- [x] decide whether dedicated `assemble` or `release` worker processes are needed in this sprint
- [x] document the reason for deferring cross-process `assemble` and `release` topology work
- [x] preserve the database-backed claim/reservation requirement for any future multi-process stage workers

Acceptance criteria:

- [x] all-in-one remains the default runtime shape
- [x] cross-process `assemble` and `release` worker topology is explicitly deferred with evidence
- [x] future multi-process worker support must use database-backed ownership, not process-local coordination

### Milestone 6 sign-off

Decision:

- Do not add dedicated cross-process `assemble` or `release` workers in this sprint.
- The measured bottlenecks were fixed by in-process assemble workers, query improvements, and batched release writes.
- Adding separate OS-process topology now would add operational surface area without a current measured need.

Reason:

- `assemble` already has process-safe header leases, but the current all-in-one runtime with goroutine workers is sufficient for the measured workload.
- `release` improved materially after query and write batching, and release family claims are still deferred until release concurrency is justified by new measurements.
- The next likely bottleneck is inspect work, not cross-process assemble/release execution.

Sign-off:

- Complete as deferred. Reopen only if a later measurement shows one process cannot keep up even after stage-specific goroutine workers and batching are exhausted.

## Milestone 7. Inspect Concurrency With Database Reservations

Goal:

- make `inspect_archive` and `inspect_media` process-safe concurrent stages using goroutine workers backed by database reservations

Status:

- [x] complete

Scope:

- primary focus: `inspect_archive`
- secondary focus: `inspect_media`
- keep discovery, PAR2, NFO, and password inspection sequential unless measurements show they become material bottlenecks

Tasks:

- [x] add database-backed reservation fields for binary inspection work
- [x] reserve inspect candidates before workers process them
- [x] make reservations stage-specific so `inspect_archive` and `inspect_media` do not block unrelated inspect stages
- [x] include owner and lease expiry so crashed workers or canceled commands do not leave candidates stuck
- [x] update candidate selection to exclude actively reserved rows and include expired reservations
- [x] make `StartBinaryInspection` compatible with reserved candidates and avoid stealing work reserved by another owner through normal candidate selection
- [x] add goroutine worker pools for `inspect_archive` and `inspect_media`
- [x] wire worker count from the existing `indexing.inspect_archive.concurrency` and `indexing.inspect_media.concurrency` settings
- [x] keep each candidate workspace isolated per worker
- [x] make cancellation stop workers cleanly while leaving unfinished reservations retryable after lease expiry
- [x] add metrics for `worker_count`, `reserved_count`, `processed_count`, and `failed_count`
- [x] add maintenance cleanup or repair behavior for expired/stale inspection reservations
- [x] validate in code that same-stage claims are serialized and active reservations are excluded across processes

Acceptance criteria:

- [x] `inspect_archive.concurrency > 1` performs real parallel inspection work
- [x] `inspect_media.concurrency > 1` performs real parallel inspection work
- [x] two goroutines in one process cannot process the same `(stage_name, binary_id)` candidate
- [x] two separate processes cannot process the same `(stage_name, binary_id)` candidate while a reservation is active
- [x] expired reservations become retryable without manual cleanup
- [x] completed and failed inspection semantics remain unchanged
- [x] archive/media inspection throughput improves without corrupting artifacts, media rows, inspection rows, or release summary updates

### Milestone 7 sign-off

Implemented:

- `binary_inspections` now carries stage-specific claim owner and lease expiry fields.
- `ClaimBinaryInspectionCandidates` serializes same-stage claim batches with a short advisory transaction lock, excludes active reservations, and returns only reserved candidates to workers.
- `inspect_archive` and `inspect_media` use worker pools driven by their stage `concurrency` settings.
- completion, failure, and stale-running maintenance clear inspection claims so retries remain automatic.
- admin runtime UI/API/config now expose concurrency only where real worker support exists: `assemble`, `inspect_archive`, and `inspect_media`.

Validation:

- Go tests passed for `pgindex`, inspect packages, app settings, API controllers, runtime wiring, and config.
- UI production build passed.

Implementation notes:

- Prefer a reservation model on `binary_inspections` if it stays simple, because `(stage_name, binary_id)` already identifies inspection work.
- If reservation state makes `binary_inspections` too overloaded, use a side table keyed by `(stage_name, binary_id)` with owner, lease expiry, and reserved-at metadata.
- A safe claim query should mirror assemble's ownership rule: select a bounded batch, atomically reserve it, then fan out only reserved candidates to workers.
- Do not rely on `StartBinaryInspection` alone as the claim boundary. It marks work running after selection, but it does not currently prevent two processes from selecting the same candidate first.
- Start conservative runtime trials with `inspect_archive.concurrency=2` and `inspect_media.concurrency=2`; archive extraction, media probing, NNTP fetches, and workspace I/O can saturate shared resources quickly.

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
- each inspect candidate is reserved once per lease window
- claims expire or can be repaired after worker crashes
- release-family work is acknowledged exactly once after successful handling
- stale claims can be repaired by maintenance
- unique constraints and idempotent upserts remain the last line of defense if a claim edge case slips through

## Working Validation Checklist

- [x] baseline metrics captured after instrumentation lands
- [x] new path A or replacement prioritization validated on the live dev backlog
- [x] `assemble.concurrency` used by real workers
- [x] no duplicate header processing under concurrency tests
- [x] no binary corruption or release-family summary drift under concurrency tests
- [x] `release` concurrency evaluated after assemble improvements
- [x] optional cross-process worker path either implemented or explicitly deferred with evidence
- [x] `inspect_archive.concurrency` used by real workers with database reservations
- [x] `inspect_media.concurrency` used by real workers with database reservations
- [x] no duplicate inspect processing under goroutine or multi-process tests

## Final Sprint Sign-Off

Status:

- [x] complete

Decision:

- This performance and process-execution sprint is complete.
- Assemble throughput, release batching, and inspect concurrency were implemented with database-backed ownership.
- Cross-process assemble/release topology remains deferred because the measured bottlenecks were addressed by in-process workers and query/write batching.

Archive note:

- Future performance work should start from new measurements, not by extending this completed sprint checklist.
- Likely next areas are database storage retention/offloading, inspect runtime tuning after real workload measurements, and any release-family claim work only if release again becomes a measured bottleneck.

## References

- `docs/ARCHITECTURE.md`
- `docs/INDEXER_HOW_IT_WORKS.md`
- `docs/INDEXER_POSTGRES_RUNTIME_TUNING.md`
- `docs/INDEXER_TEST_QUERIES.md`
- `docs/archive/completed/indexer/INDEXER_ASSEMBLE_AND_RELEASE_REFINEMENT_PLAN.md`
- `docs/archive/completed/indexer/INDEXER_BACKLOG_BURNDOWN_PERFORMANCE_PLAN.md`
