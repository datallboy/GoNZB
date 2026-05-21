# Indexer NNTP And Inspection Capacity Plan

Snapshot date: 2026-05-21

This doc tracks operational-capacity work that surfaced during the obfuscated-payload hardening sprint. Keep payload-specific findings in `docs/archive/completed/indexer/2026-05-21-obfuscated-payload-hardening/INDEXER_OBFUSCATED_PAYLOAD_FINDINGS.md`; use this doc for PAR2/yEnc throughput, exact backlog accounting, and NNTP pool/backpressure work.

## Current Sprint Scope

Branch: `sprint/nntp-manager-capacity-2026-05-21`

This sprint focuses on NNTP transport ownership and capacity enforcement. PAR2 batch persistence and the yEnc work-item table remain in this doc, but they should wait until the shared manager/backpressure baseline is in place.

Sprint tasks:

- [x] Add a manager-owned wait-queue capacity policy alongside the existing `ErrProviderBusy` policy.
- [x] Extend the manager-facing API so indexer callers can use the same transport for `Fetch`, `FetchBodyPrefix`, `GroupStats`, and `XOver`.
- [x] Route indexer scrape, assemble, yEnc recovery, and inspect fetches through `nntp.Manager` instead of a standalone provider.
- [x] Preserve downloader behavior first, then decide whether downloader can switch from caller-managed busy retry to manager-owned wait queue.
- [x] Add tests proving manager capacity is enforced for indexer-style calls.
- [x] Add basic manager stats for capacity, active/in-use slots, waiting requests, busy returns, and wait duration.
- [x] Document any deferred shared-pool reservation work if module share enforcement is too large for the first pass.

Exit criteria:

- [x] Indexer no longer creates an unbounded standalone NNTP provider path for normal scrape/recovery/inspection work.
- [x] Manager capacity cannot exceed configured provider `max_connections` under concurrent indexer calls.
- [x] Existing downloader tests and behavior still pass.
- [x] `go test ./...` passes after UI assets are built.
- [x] `go run cmd/gonzb/main.go` command checks relevant to indexer NNTP callers are run or explicitly documented as skipped with reason.
- [x] The active doc records which NNTP items were completed and which items remain for later capacity-dashboard or module-reservation work.

Sprint validation:

- `go test ./...` passed on 2026-05-21.
- `go run cmd/gonzb/main.go indexer scrape --help` passed.
- `go run cmd/gonzb/main.go indexer recover-yenc --help` passed.
- `go run cmd/gonzb/main.go indexer inspect par2 --help` passed.
- `npm --prefix ui run build` passed.
- Live one-shot scrape/recovery/inspection commands were not run during this pass to avoid consuming NNTP provider quota while validating transport wiring; the manager capacity behavior is covered by unit tests.

## Current PAR2 Inspect Pipeline

`inspect_par2` currently does the following per selected binary:

- selects a batch with `ListBinaryInspectionCandidates(ctx, "inspect_par2", batchSize)`
- starts the binary inspection claim
- prepares an inspection workspace
- samples the first article prefix, capped at `min(max_bytes, 256 KiB)`
- parses PAR2 `FileDesc` packets from the prefix
- for plain non-volume `.par2` files with no prefix targets, may materialize the full binary and parse the full manifest
- writes one inspection artifact row
- replaces PAR2 set rows for that binary
- replaces PAR2 target rows for that binary
- applies PAR2 target coverage updates
- completes the binary inspection
- applies a release rollup update only when a release id is still present

The progress log every 10 records is only a progress log. It is not the database flush boundary. Today each completed candidate writes its own artifact/set/target/coverage/inspection results before later progress lines are printed.

## PAR2 Bottleneck Findings

Known behavior:

- live `batch_size=1000`, `concurrency=1` processed 386 candidates in the bounded 120 second run budget
- live `batch_size=1000`, `concurrency=4` has been observed around the 640 range before the run timeout
- PAR2 exact backlog count is already fast enough for dashboard use, measured around 33 ms with the indexed count path

Likely bottlenecks to prove with metrics:

- NNTP fetch latency for the first article, including slow or missing articles
- connection churn from prefix fetches, because prefix readers intentionally discard the underlying NNTP connection instead of returning a partially read dot body to the pool
- per-candidate repository round trips and small transactions
- repeated catalog metadata loads for each candidate
- occasional full-manifest fallback for plain `.par2` files when the prefix lacks `FileDesc` packets

Redis is not the first fix for this lane. The hot path needs step timing and batched persistence first. A separate cache/queue service should only be considered if measured Postgres batching and NNTP backpressure still cannot keep up.

## PAR2 Action Items

- [ ] Add per-step `inspect_par2` timing metrics: candidate selection, catalog metadata load, NNTP prefix fetch, PAR2 parse, full-manifest fallback, artifact/set/target writes, coverage writes, and completion writes.
- [ ] Track full-manifest fallback count and fallback bytes so slow candidates can be separated from normal prefix-only candidates.
- [ ] Add batch persistence for PAR2 inspection results. Workers should inspect/fetch/parse candidates and hand result objects to a flush loop that commits rows in chunks instead of forcing every candidate through separate small write transactions.
- [ ] Combine per-candidate PAR2 artifact/set/target/coverage/inspection writes into fewer transactional repository calls where chunked flushing is not practical.
- [ ] Add metrics for candidate result flush size, flush duration, rows written, and write failures.
- [ ] Keep run-budget exit non-fatal: partial committed progress is acceptable, remaining candidates should stay eligible for the next scheduler tick.
- [ ] Re-run `go run cmd/gonzb/main.go indexer inspect-par2 --once` with live `batch_size=1000` and `concurrency=4` after metrics land, then document the slowest step distribution before changing more code.

## yEnc Exact Backlog And Recovery Plan

Current state:

- yEnc recovery itself can process live batches, but exact dashboard counting is not safe with the current derived query shape
- selector-backed bounded measurement for 5000 candidates took about 1.2 seconds
- full exact yEnc count exceeded a 30 second statement timeout
- the blocker is schema shape: claimability is derived from weak/obfuscated binary state, readiness-summary state, first-part lookup, and article payload retry/name state

The right direction is a durable Postgres work queue or rollup, not a dashboard-only cache.

Proposed work-item shape:

- one row per claimable yEnc recovery unit, likely keyed by provider, binary id, and first article/message id
- `status`: `ready`, `claimed`, `done`, `stale`, `retry_after`, or equivalent
- `ready_at` for retry/backoff visibility
- priority fields for weak/obfuscated candidate ordering
- provider and newsgroup fields for fetch routing and dashboard grouping
- updated timestamps and optional lease owner/lease expiration if recovery is allowed to run concurrently across processes later

Expected benefits:

- dashboard exact count becomes an indexed `status='ready' AND ready_at <= now()` count
- recovery candidate selection no longer recomputes the expensive raw join every run
- stale candidate handling becomes explicit and measurable
- recovery can expose fast backlog, claimed, done, retry, and stale totals

## yEnc Action Items

- [ ] Design a migration-backed yEnc recovery work-item table or rollup table.
- [ ] Define which existing events create, update, retire, or stale a yEnc work item.
- [ ] Replace exact dashboard counting with indexed work-item totals.
- [ ] Move recovery candidate selection to the work-item table once the backfill/maintenance path is reliable.
- [ ] Add yEnc recovery metrics for selection duration, fetch duration, parse duration, write duration, stale candidates, retry candidates, and active worker count.
- [ ] Backfill work items with a bounded migration or maintenance command, not ad hoc live schema or data edits.

## Current NNTP Transport Shape

Downloader path:

- downloader builds `nntp.Manager`
- the manager creates providers and wraps each provider with a semaphore sized to `MaxConnection`
- downloader worker count is derived from manager `TotalCapacity()`
- when every provider semaphore is full, `Fetch` returns `ErrProviderBusy`
- downloader requeues busy jobs after a short delay

Indexer path:

- indexer runtime now creates an `nntp.Manager` with `wait_queue` capacity policy for the selected scrape server config
- scrape latest/backfill, assemble fetches, yEnc recovery, and inspection stages share the manager client
- the manager provides a hard semaphore around provider calls, including fetch body, fetch prefix, group stats, and XOVER
- indexer calls wait for capacity until their request context expires instead of creating unbounded extra provider connections

Implication:

- in all-in-one deployments, downloader and indexer can be configured against the same NNTP account but use separate transport objects
- downloader still uses caller-managed `ErrProviderBusy` behavior
- indexer now has a module-local semaphore-backed wait queue, but downloader and indexer do not yet share one process-wide reservation pool
- if both scopes use `max_connections=40`, the process can still reserve up to 40 downloader slots plus 40 indexer slots unless settings are scoped lower or shared-pool ownership is added

## Proposed NNTP Manager Direction

Use one semaphore-backed NNTP manager abstraction for every module that sends NNTP commands. Downloader, indexer, and future NNTP consumers should ask the transport layer for work such as fetch body, fetch prefix, group stats, or XOVER. They should not each own provider capacity, low-level retry behavior, or connection backpressure.

Manager capacity behavior should be configurable by policy:

- `return_busy`: current downloader-compatible behavior where a saturated manager returns `ErrProviderBusy` and the caller owns requeue timing
- `wait_queue`: manager-owned queue/backpressure behavior where requests wait for a capacity slot until the request context expires

Longer-term preference:

- move downloader toward manager-owned queue/backpressure too, so low-level NNTP capacity handling stays out of downloader and indexer domains
- keep domain stages responsible for their own business-level retries and failure decisions, not provider-slot scheduling
- expose queue wait as metrics so a module visibly slows down when NNTP is saturated instead of silently creating more connections

Shared-pool fairness model:

- combine downloader and indexer NNTP usage into one process-wide pool when they target the same provider account
- give each module dynamic reservations instead of fixed hard splits
- allow idle capacity borrowing, so indexer can use up to 100 percent of the pool when no downloader work is active
- when downloader work appears, reserve a configurable minimum share for downloader and cap indexer at the remaining share until download pressure clears
- make shares configurable per module, for example downloader minimum 20 percent and indexer maximum 80 percent while downloads are active
- expose active, reserved, borrowed, waiting, and denied counts per module

This model fits the DDD boundary better: NNTP transport owns provider mechanics, while downloader and indexer ask for NNTP operations and react to returned domain-relevant results.

## NNTP Action Items

- [x] Refactor indexer NNTP fetches onto the semaphore-backed `nntp.Manager` path instead of a standalone provider.
- [x] Add manager capacity policy options: caller-managed `ErrProviderBusy` and manager-owned wait queue.
- [ ] Move downloader toward manager-owned wait-queue behavior after metrics prove the new policy is stable.
- [ ] Add shared-pool module reservations with idle borrowing, so indexer can use the full pool when alone and yield a configured share when downloader work is active.
- [x] Add basic NNTP manager stats: configured capacity, active/in-use slots, waiting requests, busy returns, operation counts, wait count, total wait duration, and max wait duration.
- [x] Add provider-level stats access for idle pooled connections, dials, dial failures, reused connections, discarded connections, fetch retries, XOVER retries, and recoverable errors.
- [x] Add basic queue/backpressure stats: waiting requests, busy returns, wait count, total wait time, and max wait time.
- [x] Surface retry counters and wait timing in the admin API and dashboard. Average wait can be derived from total wait time and wait count, but is not yet precomputed as a separate field.
- [x] Add per-stage indexer NNTP demand stats: scrape, assemble lanes, yEnc recovery, and individual inspect stages each use scoped manager clients with active, waiting, request, and wait timing counters.
- [x] Surface indexer NNTP capacity stats in the admin dashboard so backlog growth can be tied to provider pressure instead of guessed from stage throughput.
- [x] Add a blocking acquire path for indexer NNTP calls so indexer work waits behind a measured queue instead of silently opening more connections.
- [x] Add provider-pressure counters for common NNTP failure classes visible to the indexer manager: busy returns, provider recoverable errors, operation errors, and article missing. More specific provider rate-limit classification remains future work if live provider responses expose a stable signal.
- [ ] Defer downloader queue/wait policy and downloader-specific active worker stats to a separate downloader-focused session.

## Sign-Off Checklist

Done:

- [x] Split operational capacity findings out of the obfuscated-payload hardening findings doc.
- [x] Record the current PAR2 persistence boundary: writes happen per candidate, not every 10 progress records.
- [x] Record the current NNTP ownership split between downloader manager and indexer provider.
- [x] Record the preferred shared NNTP manager direction with configurable busy/wait policies and dynamic module reservations.
- [x] Add manager wait-queue capacity policy and route indexer NNTP operations through it.
- [x] Add live indexer NNTP capacity stats to the admin API and WebUI dashboard.
- [x] Add scoped indexer NNTP demand stats alongside manager-level pressure.

Needs completion:

- [ ] PAR2 step timing metrics prove whether the next bottleneck is NNTP, parse, fallback materialization, or database writes.
- [ ] PAR2 result persistence is batched or consolidated enough that database round trips are not the dominant cost.
- [ ] yEnc work-item/rollup design provides fast exact dashboard counts and faster recovery candidate selection.
- [ ] Shared NNTP capacity can be reserved, borrowed, and reported by module.
- [ ] Downloader-specific NNTP wait policy, reservations, and active worker stats are handled in a separate session.
