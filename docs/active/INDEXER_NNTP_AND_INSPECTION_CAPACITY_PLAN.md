# Indexer NNTP And Inspection Capacity Plan

Snapshot date: 2026-05-21

This doc tracks operational-capacity work that surfaced during the obfuscated-payload hardening sprint. Keep payload-specific findings in `docs/active/INDEXER_OBFUSCATED_PAYLOAD_FINDINGS.md`; use this doc for PAR2/yEnc throughput, exact backlog accounting, and NNTP pool/backpressure work.

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

- indexer runtime creates a standalone `nntp.Provider` from the scrape server config
- scrape latest/backfill, assemble fetches, yEnc recovery, and inspection stages share that one indexer provider instance
- this indexer provider is not the downloader `nntp.Manager`
- the provider has an idle connection pool, but `getConn` dials immediately when no idle connection is available
- the provider pool does not currently act as a hard concurrency semaphore

Implication:

- in all-in-one deployments, downloader and indexer can be configured against the same NNTP account but use separate transport objects
- if both scopes use `max_connections=40`, the process can attempt more than 40 total account connections unless settings are scoped lower or transport ownership is unified
- the indexer stages are throttled mostly by each stage's own concurrency/batch settings, not by a shared NNTP wait queue

## NNTP Action Items

- [ ] Decide whether downloader and indexer should share one process-wide NNTP manager or keep separate scoped pools with explicit combined connection budgeting.
- [ ] Add NNTP provider/manager stats: configured capacity, active/in-use connections, idle pooled connections, dials, dial failures, reused connections, discarded connections, fetch retries, XOVER retries, and recoverable errors.
- [ ] Add queue/backpressure stats: waiting fetches, busy returns, retry count, average wait time, and max wait time.
- [ ] Add per-module NNTP demand stats: downloader queued segments and active workers, scrape active XOVER requests, yEnc active workers, PAR2 active workers, archive/media/NFO/password active workers.
- [ ] Surface NNTP capacity stats in the admin dashboard so backlog growth can be tied to provider pressure instead of guessed from stage throughput.
- [ ] Consider a blocking acquire/lease API for indexer NNTP fetches so indexer work waits behind a measured queue instead of silently opening more connections.
- [ ] Add rate-limit/provider-pressure counters for common NNTP failure classes, including busy, timeout, connection reset, and article missing.

## Sign-Off Checklist

Done:

- [x] Split operational capacity findings out of the obfuscated-payload hardening findings doc.
- [x] Record the current PAR2 persistence boundary: writes happen per candidate, not every 10 progress records.
- [x] Record the current NNTP ownership split between downloader manager and indexer provider.

Needs completion:

- [ ] PAR2 step timing metrics prove whether the next bottleneck is NNTP, parse, fallback materialization, or database writes.
- [ ] PAR2 result persistence is batched or consolidated enough that database round trips are not the dominant cost.
- [ ] yEnc work-item/rollup design provides fast exact dashboard counts and faster recovery candidate selection.
- [ ] NNTP pool/backpressure stats are visible enough to tune total concurrency against the provider account limit.
- [ ] The dashboard shows both stage backlog and NNTP capacity pressure.
