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

## Next Session Focus

The NNTP manager merge is functionally in place. The next clean work session should focus on the remaining backlog items in this order:

1. PAR2 inspect write-path batching
   - add a flush loop / chunked persistence path
   - reduce per-candidate repository round trips
   - capture flush-size / flush-duration / rows-written metrics
2. yEnc exact backlog and candidate-selection rework
   - design the durable work-item table
   - define create/update/retire events
   - move exact dashboard counts and candidate selection onto indexed work items
3. Assemble follow-up measurement and remaining write hot spots
   - add chunk-level retry / chunk-count telemetry around `UpsertBinaries`
   - batch release-family summary refreshes where practical
   - investigate why some direct `--once` runs do not persist into `indexer_stage_runs`

Operational note:

- Fresh `serve` startup on 2026-05-26 did not reproduce NNTP pool stats logging when `enable_pool_logging=0` was persisted for both downloader and indexer server rows. If pool logs still appear after toggling the setting at runtime, treat that as a manager-reload issue first rather than a settings-persistence bug.

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
- live `batch_size=1000`, `concurrency=8` has completed 1000 candidates in roughly 93-115 seconds of processing time, with 3-9 seconds of candidate selection time before step timing metrics landed
- PAR2 exact backlog count is already fast enough for dashboard use, measured around 33 ms with the indexed count path
- code previously capped effective PAR2 workers at 8 even if runtime settings were raised higher; the cap is now 32 so a configured concurrency of 20 can actually run

Likely bottlenecks to prove with metrics:

- NNTP fetch latency for the first article, including slow or missing articles
- connection churn from prefix fetches, because prefix readers intentionally discard the underlying NNTP connection instead of returning a partially read dot body to the pool
- per-candidate repository round trips and small transactions
- repeated catalog metadata loads for each candidate
- occasional full-manifest fallback for plain `.par2` files when the prefix lacks `FileDesc` packets

Redis is not the first fix for this lane. The hot path needs step timing and batched persistence first. A separate cache/queue service should only be considered if measured Postgres batching and NNTP backpressure still cannot keep up.

## PAR2 Action Items

- [x] Add per-step `inspect_par2` timing metrics: candidate selection, NNTP/catalog prefix sampling, PAR2 parse, full-manifest fallback, artifact/set/target writes, coverage writes, completion writes, release rollup writes, and skipped-candidate writes.
- [x] Track full-manifest fallback count and fallback bytes so slow candidates can be separated from normal prefix-only candidates.
- [x] Add batch persistence for PAR2 inspection results. Workers now inspect/fetch/parse candidates and hand completed result objects to a flush loop that commits rows in chunks instead of forcing every candidate through separate small write transactions.
- [x] Combine per-candidate PAR2 artifact/set/target/coverage/inspection writes into fewer transactional repository calls where chunked flushing is not practical.
- [x] Add metrics for candidate result flush size, flush duration, rows written, and write failures.
- [x] Keep run-budget exit non-fatal: partial committed progress is acceptable, remaining candidates stay eligible for the next scheduler tick.
- [x] Re-run `go run cmd/gonzb/main.go indexer inspect par2 --once` with live `batch_size=1000` and `concurrency=4` after metrics land, then document the slowest step distribution before changing more code.

Live PAR2 measurements on 2026-05-26:

- Baseline before batching: `go run cmd/gonzb/main.go indexer inspect par2 --once` completed `1000/1000` in about `118.8s` wall time with `candidate_selection_ms=971.687`, `prefix_fetch_ms=433153.357`, `coverage_write_ms=33983.581`, `artifact_write_ms=2260.366`, `set_write_ms=1654.004`, `target_write_ms=405.950`, and `completion_write_ms=1299.761`.
- First flush-loop attempt used `result_flush_max_size=32` and exposed an unacceptable regression: one flush stretched to about `61059.8 ms`, total `result_flush_ms=61514.894`, and the run budget stopped at `408/1000`.
- Follow-up tuning reduced the flush cap to `8` candidates per transaction. The next live run completed `957/1000` within the same `120s` budget with `candidate_selection_ms=334.58`, `prefix_fetch_ms=477281.512`, `result_flush_ms=1750.328`, `result_flush_count=120`, `result_flush_rows=2871`, `result_flush_max_ms=55.481`, and `result_flush_failures=0`.
- The batching change materially removed the write-path hotspot. The prior live sample spent about `38.3s` in explicit write buckets (`coverage/artifact/set/target/completion`), while the tuned flush path spent about `1.75s` total in chunk commits. End-to-end throughput is now primarily NNTP/prefix-fetch limited, not database-write limited.
- A later direct manual `go run cmd/gonzb/main.go indexer inspect par2 --once` check at `2026-05-26 16:05-16:18 America/New_York` uncovered a new regression path. The first post-selector sample (`stage_run=61497`) processed only `56/1000` before manual termination and recorded `prefix_fetch_ms=15040.761` versus `result_flush_ms=312087.033`, `result_flush_max_ms=177482.063`, and `result_flush_failures=7`.
- The first retry after startup/runtime fixes (`stage_run=61499`) failed quickly on a SQL type-cast bug while still showing that the surrounding flush shell was not the dominant cost by itself: `processed_count=16`, `prefix_fetch_ms=8528.722`, `result_flush_ms=57.785`, and `result_flush_max_ms=13.892`.
- The corrected rerun (`stage_run=61500`) was again terminated after an extended stall and still showed the write path dominating: `processed_count=24`, `candidate_selection_ms=575.109`, `prefix_fetch_ms=9822.700`, `result_flush_ms=170001.716`, `result_flush_max_ms=99352.586`, and `result_flush_failures=8`. This confirms the current live bottleneck is still inside the batch persistence path, not NNTP prefix fetch.
- Root-cause analysis from live `EXPLAIN (ANALYZE, BUFFERS)` shows the remaining hotspot is the PAR2 target-coverage update query in `applyBinaryPAR2TargetCoverageInTx`. On a representative `20`-target sample from binary `3459121`, Postgres chose `idx_binaries_release_family_key` and scanned about `62.5k` binaries for `(provider_id=1,newsgroup_id=401)` before hashing target names, instead of using `idx_binaries_normalized_file_identity`. Even with zero rows needing updates, that no-op sample still took about `102-116 ms` and hit about `34.6k` shared buffers. Multiplied across many targets and flushes, that explains the multi-minute flush totals.
- Direct one-shot startup also had a separate runtime bug: both downloader and indexer command bootstrap paths were preferring shared NNTP servers over scoped runtime server lists. In this environment the shared row had an empty password, so `indexer inspect par2 --once` initially failed during provider validation until the bootstrap was corrected to prefer `DownloaderServers` and `IndexerServers`.
- Final rewrite on `2026-05-26` changed PAR2 target coverage from a set-level `UPDATE ... FROM target_values` scan to indexed normalized-name lookups followed by primary-key updates. A representative `4`-name lookup now plans as an `Append` of direct `idx_binaries_normalized_file_identity` index scans and completes in about `0.161 ms`, instead of scanning the entire provider/newsgroup slice.
- Post-rewrite validation `go run cmd/gonzb/main.go indexer inspect par2 --once` completed cleanly as manual `stage_run=61501` in about `1:07.41` wall time with `candidate_count=964`, `processed_count=964`, `submitted_count=964`, `candidate_selection_ms=572.110`, `processing_ms=64214.059`, `prefix_fetch_ms=228431.339`, `result_flush_ms=9277.418`, `result_flush_max_ms=226.340`, `result_flush_count=121`, `result_flush_rows=20167`, `result_flush_failures=0`, and `result_flush_max_size=8`.
- That final sample is the current trustworthy post-change metric. Compared with the failed flush-bound manual sample (`stage_run=61500`), flush time dropped from about `170.0s` to about `9.3s`, failures fell from `8` to `0`, and the one-shot command returned to completing under the `120s` run budget. At this point `inspect_par2` is again primarily NNTP/prefix-fetch limited rather than database-write limited.

## yEnc Exact Backlog And Recovery Plan

Current state:

- yEnc recovery itself can process live batches, but exact dashboard counting is not safe with the current derived query shape
- selector-backed bounded measurement for 5000 candidates took about 1.2 seconds
- full exact yEnc count exceeded a 30 second statement timeout
- the blocker is schema shape: claimability is derived from weak/obfuscated binary state, readiness-summary state, first-part lookup, and article payload retry/name state
- live `batch_size=999`, `concurrency=8` has been completing full batches in roughly 75-85 seconds of processing time, with candidate selection varying from about 2-25 seconds before finer timing metrics landed

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

- [x] Design a migration-backed yEnc recovery work-item table or rollup table.
- [x] Define which existing events create, update, retire, or stale a yEnc work item.
- [x] Replace exact dashboard counting with indexed work-item totals.
- [x] Move recovery candidate selection to the work-item table once the backfill/maintenance path is reliable.
- [x] Add yEnc recovery metrics for selection duration, fetch duration, parse duration, match duration, write duration, not-found backoff write duration, stale candidates, retry candidates, and active worker count.
- [x] Backfill work items with a bounded migration or maintenance command, not ad hoc live schema or data edits.

Implemented yEnc work-item lifecycle:

- `RefreshBinaryStatsBatch` now incrementally creates or refreshes yEnc work items for touched binaries after assemble writes finish.
- `RecordYEncRecoveryNotFound` now updates the work item `ready_at` and `missing_count` alongside the payload retry state.
- `ApplyYEncHeaderRecovery` now retires recovered source/target work items as `done`.
- `indexer maintenance` now performs bounded work-item backfill batches for older eligible binaries so the queue can be filled without manual SQL edits.

Live yEnc measurements on 2026-05-26:

- Baseline before the work-item table: dashboard backlog was bounded at `1000`, and the last available stage-run metrics for the old derived selector showed `candidate_selection_ms=3277.882` on a failed `concurrency=8` run. A direct baseline `go run cmd/gonzb/main.go indexer recover-yenc --once` at live `batch_size=999` and `concurrency=4` completed `999/999` in about `3:03.37` wall time, but that legacy command path did not persist stage metrics.
- After adding migration `026_yenc_recovery_work_items.up.sql` and running maintenance backfill, the durable queue held `10000` ready items before the first post-change recovery pass. After two tracked post-change runs it still held `8002` ready items and `8038` total items, proving the queue is materially larger than the old bounded dashboard estimate.
- The tracked post-change `go run cmd/gonzb/main.go indexer recover-yenc --once` run on `2026-05-26 13:24-13:28 America/New_York` completed `999/999` with `candidate_selection_ms=57.473`, `processing_ms=251107.442`, `fetch_ms=988531.288`, `write_ms=13964.070`, `merged=989`, `fetch_failures=0`, and `parse_failures=0`.
- The main meaningful yEnc improvement is selector/count correctness and speed: recovery candidate selection moved off the expensive derived join and the dashboard now counts indexed queue rows instead of a bounded `LIMIT` snapshot.

## Assemble Backlog Concerns

Assemble keeps returning as an operational bottleneck. Current live backlog on 2026-05-21 was about 44.5 million unassembled article headers, with about 60k currently claimed. This is not an NNTP bottleneck: assemble is mostly database selection/write/refresh work.

Live stage-run findings from 2026-05-21:

- `assemble_lane_b` processed 60k headers in about 329 seconds, around 182 headers/sec.
- That lane B run refreshed about 42,977 binaries from 60k headers, so the batch had very low part locality.
- Lane B cumulative worker timing was dominated by `binary_upsert_duration_ms` at about 935 seconds and `binary_refresh_duration_ms` at about 516 seconds, followed by `binary_part_upsert_duration_ms` at about 101 seconds.
- Recent lane B per-binary timing was consistently expensive enough to justify real batching: `UpsertBinary` averaged about 16-22 ms per unique binary, and `RefreshBinaryStatsBatch` averaged about 11-12 ms per refreshed binary. On 38k-57k unique binaries per 60k-header pass, that becomes minutes of cumulative worker time.
- `assemble_lane_a` selection is a clear problem: observed runs spent about 43 seconds selecting 0 headers, about 65 seconds selecting 465 headers, and about 195 seconds selecting only 46 headers.
- A later lane A run selected 1,989 headers but still spent about 65 seconds in candidate selection.
- A live `EXPLAIN (ANALYZE, BUFFERS)` of the lane A priority selector showed the root cause: Postgres chose a sequential scan over about 18.7M `binaries` rows and sorted incomplete named binaries before joining to the 100k pending-header window. That plan returned 20,311 ids in about 22.1 seconds, with about 13.3 seconds of read I/O and a 57 MB external merge sort.
- Rewriting lane A priority matching as a `LATERAL` lookup from each structured pending header into the existing normalized binary identity index returned the same 20,311 ids in about 351 ms on the same live backlog. This was a query-shape problem, not a missing live index problem.
- A post-change `go run cmd/gonzb/main.go indexer assemble lane-a --once` pass completed eight tiny priority batches at about 507-867 headers/sec. Logged candidate selection rounded to 0.00 ms for those batches, replacing the prior 32-195 second selector stalls.
- A post-change `go run cmd/gonzb/main.go indexer assemble lane-b --once` pass still took about 126 seconds wall time for eight 7,500-header worker batches. Refresh timing improved from the prior 11-12 ms per refreshed binary to about 3.2-5.8 ms per refreshed binary, but `binary_upsert_duration_ms` remained about 64-72 seconds per worker batch and is now clearly the dominant Lane B cost.
- That same Lane B command shows the remaining refresh bucket is not pure binary stat recomputation anymore. It includes one-at-a-time release-family summary refreshes after the set-based binary stat update, so summary refresh batching is the next refresh-side target.
- On 2026-05-26, the first true `UpsertBinaries` batch implementation exposed two database limits under real Lane B load: holding too many advisory locks in one chunk transaction caused `OUT OF SHARED MEMORY`, and larger `INSERT ... ON CONFLICT` batches deadlocked when concurrent workers locked conflicting rows in different orders.
- The follow-up fix was to keep the logical assemble batch large but process binary upserts in smaller internal chunks, commit each chunk independently so advisory locks are released promptly, and force a deterministic `provider_id/newsgroup_id/binary_key` ordering inside the batch upsert.
- With worker count reduced to 4 and internal upsert chunk size reduced to 250, `go run cmd/gonzb/main.go indexer assemble lane-b --once` completed cleanly with 15k worker batches. Observed worker metrics were about 122-244 headers/sec, about 38.8s-71.7s of `binary_upsert_ms`, about 15.0s-48.7s of `binary_refresh_ms`, and only about 4.4s-6.1s of `binary_part_upsert_ms`.
- A clean rerun on 2026-05-26 after clearing logs reproduced the same general result with 4 workers and 15k worker batches. Observed worker metrics were:
  - `8190` unique binary upserts, `254.06` headers/sec, `37399.02 ms` binary upsert, `14045.93 ms` binary refresh
  - `8443` unique binary upserts, `197.98` headers/sec, `39092.44 ms` binary upsert, `29243.82 ms` binary refresh
  - `12496` unique binary upserts, `180.99` headers/sec, `54960.88 ms` binary upsert, `20845.27 ms` binary refresh
  - `12846` unique binary upserts, `148.11` headers/sec, `57683.84 ms` binary upsert, `36487.58 ms` binary refresh
- That rerun confirms the batching change materially helped. Pre-batching 7.5k worker slices commonly spent about `64s-72s` in `binary_upsert_ms`; the post-change rerun handled 15k worker slices with `37s-58s` of `binary_upsert_ms` in the observed worker logs while remaining stable.
- The 2026-05-26 `lane-a --once` rerun also stayed healthy at larger logical worker slices: about 2,135-2,136 headers per worker batch, about 799-960 headers/sec, `candidate_selection_ms=0.00`, and only 13-50 unique binary upserts per worker batch.
- Direct `lane-a --once` and `lane-b --once` command logs remain the most reliable measurement source for these tests. The `indexer_stage_runs` table did not consistently persist the newer ad hoc command runs, which is a separate runtime-observability gap.
- A later serve-mode observation on 2026-05-26 showed materially worse live Lane B throughput while other DB-heavy stages were active. Recent supervisor-era worker logs were about `80-106 headers/sec`, about `82s-99s` of `binary_upsert_ms`, and about `42s-70s` of `binary_refresh_ms`.
- Those slow serve-mode Lane B passes overlapped with active `recover_yenc`, `inspect_par2`, `release`, and occasional `inspect_archive` work. The log shows `assemble_recovery_attempts=0` for the slow Lane B workers, so the slowdown was not NNTP work inside assemble itself.
- A fresh direct `go run cmd/gonzb/main.go indexer assemble lane-b --once` rerun on 2026-05-26, using the same 15k worker slices and the current runtime settings, completed with much better worker metrics:
  - `8004` unique binary upserts, `280.12 headers/sec`, `31206.82 ms` binary upsert, `14902.93 ms` binary refresh
  - `8597` unique binary upserts, `225.24 headers/sec`, `34036.56 ms` binary upsert, `25833.24 ms` binary refresh
  - `11868` unique binary upserts, `207.68 headers/sec`, `47593.98 ms` binary upsert, `18166.51 ms` binary refresh
  - `12085` unique binary upserts, `184.31 headers/sec`, `46997.37 ms` binary upsert, `27702.69 ms` binary refresh
- That serve-vs-once comparison strongly suggests the supervisor orchestration itself is not the Lane B bottleneck. The meaningful difference is concurrent stage pressure, especially from other write-heavy indexer stages and PAR2 activity that can deadlock or hold competing row/advisory locks.
- After reducing live `recover_yenc` and `inspect_par2` concurrency to `4` each in runtime settings, a fresh serve-mode scheduled `assemble_lane_b` run completed on 2026-05-26 with these aggregate metrics:
  - `batch_size=60000`, `worker_count=4`, `processed_headers=60000`
  - `binaries_refreshed=17684`, `unique_binary_upserts=17684`, `binary_upsert_cache_hits=42316`
  - `headers_per_second=726.99`, `refreshed_binaries_per_second=214.27`
  - `candidate_selection_duration_ms=18877.37`
  - `header_match_duration_ms=18849.02`
  - `binary_upsert_duration_ms=87692.55`
  - `binary_part_upsert_duration_ms=15790.71`
  - `binary_refresh_duration_ms=37666.43`
- Worker-level log samples from that same serve-mode run were much healthier than the earlier 8-worker overlap period and showed the batch shape changing over the pass:
  - `72` unique upserts, `1393.95 headers/sec`, `1556.55 ms` upsert, `174.05 ms` refresh
  - `1542` unique upserts, `678.64 headers/sec`, `8444.71 ms` upsert, `3989.50 ms` refresh
  - `7969-8101` unique upserts, about `235.65-235.71 headers/sec`, about `38820.98-38870.31 ms` upsert, about `16576.17-16926.72 ms` refresh
- This does not prove the 4/4 setting is globally optimal, but it does show the live supervisor path recovered from the much worse `80-106 headers/sec` / `82s-99s` upsert period observed with heavier overlap.

Current batching audit:

- `UpsertBinaryParts` is batched in chunks up to 8,000 records and marks article headers assembled with a set-based update. This part is already using the database batching pattern.
- `RefreshBinaryStatsBatch` used to be only batched at the API boundary. Internally it looped each binary id, ran `refreshBinaryStatsInTx` one binary at a time, then refreshed release-family summaries one key at a time.
- `RefreshBinaryStatsBatch` now performs binary stat recomputation as a set-based aggregate/update over chunks of up to 8,000 binary ids, then dedupes and refreshes the affected release-family summary keys. This should remove the 11-12 ms per-binary aggregate/update cost from Lane B.
- `UpsertBinary` used to be fully one-at-a-time. Assemble now batches unique binary rows in memory per worker and sends them through `UpsertBinaries`, but the hot SQL is still expensive because each chunk must perform many `INSERT ... ON CONFLICT` checks and updates against existing binary identities.
- Large logical Lane B batches now depend on small internal binary-upsert chunks. Bigger chunk transactions created too many advisory locks and row-lock conflicts; smaller chunk transactions traded some extra round trips for materially better stability.
- The internal binary-upsert chunk should be modeled as its own advanced tuning value, not as a percentage of assemble batch size. The stability limit is driven by unique-binary density per worker and advisory-lock footprint, which can vary sharply even when total selected headers stay constant.
- Lane A priority candidate selection is set-based, but the old query shape encouraged a full incomplete-binary scan before joining to pending headers. The lateral rewrite makes pending structured headers drive indexed binary lookups and keeps repeated file names cheap through Postgres memoization.
- Application RAM use around 1 GB in serve mode is not itself the assemble limiter right now. The slow paths are database I/O, per-row transactions, and query shape. We should use RAM deliberately in assemble by grouping batch work in memory and sending larger set-based operations to Postgres, not by adding generic caches before the hot SQL is fixed.

Likely SQL bottlenecks to prove or fix:

- Lane A priority selector: `listPriorityAssemblyHeaderIDs` against `article_headers`, `article_header_ingest_payloads`, and `binaries`, especially when the configured batch is large and the capped recent window is 100k headers.
- Lane B binary writes: the dominant cost is now the set-based `UpsertBinaries` SQL itself plus release-family refresh work, not the old one-transaction-per-binary path. Stability still depends on keeping advisory-lock scope and row-lock ordering disciplined.
- Binary stats refresh: fixed for binary stat rows with a set-based aggregate/update; still needs live post-change verification and summary-key batching review.
- Release-family summary refresh: summary keys are deduped but still refreshed one at a time. Each key runs an aggregate query, a dominant-binary query, and an upsert into `release_family_readiness_summaries`.

Assemble action items:

- [x] Capture `EXPLAIN (ANALYZE, BUFFERS)` for lane A priority selection during a representative backlog state and document whether the expensive node is recent pending scan, payload join, binary normalized-name lookup, ranking/sort, or hydration.
- [x] Rewrite lane A priority selection so pending headers drive indexed binary lookups instead of letting Postgres scan and sort the large `binaries` table.
- [ ] Add explicit assemble metrics for claim selector substeps: priority selection, recent selection, hydration, and claim update.
- [x] Batch binary upserts by unique binary key, returning ids for all records in one repository call where possible. The store now batches unique binary rows per worker and processes them in smaller internal transactions.
- [x] Expose internal binary-upsert chunk size as an advanced runtime setting with a conservative default. Implemented as `binary_upsert_db_chunk_size` on assemble-stage runtime settings, surfaced in the admin UI behind the advanced-settings toggle, with default `250`.
- [ ] Add chunk-level retry/telemetry around `UpsertBinaries` so deadlock retries and chunk counts are visible in metrics instead of only in command logs.
- [x] Convert `RefreshBinaryStatsBatch` to a true set-based aggregate/update over all refreshed binary ids instead of looping one binary at a time.
- [ ] Batch release-family summary refreshes by key set where practical.
- [ ] Consider reducing or disabling lane A polling when repeated empty/tiny lane A selections are observed, so lane A does not spend tens of seconds proving no priority work while lane B has a massive backlog.
- [ ] Re-check lane B after binary upsert and refresh batching; if header matching becomes dominant only then revisit matcher-level optimization.
- [ ] Decide whether serve mode needs temporary stage staggering or lower live concurrency for `recover_yenc` / `inspect_par2` while the assemble backlog is still dominant. Current evidence points to cross-stage contention, not supervisor overhead.
- [x] Re-check live Lane A selection timing after deployment. Expected selection time for the tested backlog shape is sub-second instead of tens of seconds.
- [x] Re-check live Lane B refresh timing after deployment. Refresh dropped, but `binary_upsert_duration_ms` remains dominant.
- [ ] Investigate why `indexer_stage_runs` did not consistently persist direct `indexer assemble lane-a --once` and `lane-b --once` test runs. This is obscuring exact before/after comparisons in the dashboard.

## Current NNTP Transport Shape

Downloader path:

- downloader builds the process shared `nntp.Manager` when the downloader module is enabled
- the manager creates providers and wraps each provider with a semaphore sized to `MaxConnection`
- downloader worker count is derived from manager `TotalCapacity()`
- when every provider semaphore is full, `Fetch` returns `ErrProviderBusy`
- downloader requeues busy jobs after a short delay

Indexer path:

- indexer runtime reuses the process shared `nntp.Manager` when downloader has already initialized it
- indexer-only deployments still create an `nntp.Manager` with `wait_queue` capacity policy for the selected scrape server config
- scrape latest/backfill, assemble fetches, yEnc recovery, and inspection stages share the manager client
- the manager provides a hard semaphore around provider calls, including fetch body, fetch prefix, group stats, and XOVER
- indexer calls wait for capacity until their request context expires instead of creating unbounded extra provider connections

Implication:

- in all-in-one deployments, downloader and indexer now share one process-wide NNTP manager and provider semaphore pool
- downloader still uses caller-managed `ErrProviderBusy` behavior
- indexer uses scoped manager clients and waits behind the manager when no slot is available
- shared provider settings live in runtime `servers`; legacy downloader/indexer server fields remain compatibility fallbacks

## Proposed NNTP Manager Direction

Use one semaphore-backed NNTP manager abstraction for every module that sends NNTP commands. Downloader, indexer, and future NNTP consumers should ask the transport layer for work such as fetch body, fetch prefix, group stats, or XOVER. They should not each own provider capacity, low-level retry behavior, or connection backpressure.

Manager capacity behavior should be configurable by policy:

- `return_busy`: current downloader-compatible behavior where a saturated manager returns `ErrProviderBusy` and the caller owns requeue timing
- `wait_queue`: manager-owned queue/backpressure behavior where requests wait for a capacity slot until the request context expires

Longer-term preference:

- move downloader toward manager-owned queue/backpressure too, so low-level NNTP capacity handling stays out of downloader and indexer domains
- keep domain stages responsible for their own business-level retries and failure decisions, not provider-slot scheduling
- expose queue wait as metrics so a module visibly slows down when NNTP is saturated instead of silently creating more connections

Shared-pool fairness model implemented in this sprint:

- downloader and indexer NNTP usage are combined into one process-wide pool for all-in-one runtime builds
- module reservations are dynamic instead of fixed hard splits
- idle borrowing lets indexer use up to 100 percent of the pool when downloader demand is quiet
- recent downloader demand caps new indexer borrows at the configured indexer share until the demand window clears
- runtime `nntp_pool` settings expose idle borrow, indexer max percent, downloader reserve percent, and downloader demand window
- manager stats now report reservation settings, downloader/indexer active counts, active limits, and whether downloader demand is currently active

This model fits the DDD boundary better: NNTP transport owns provider mechanics, while downloader and indexer ask for NNTP operations and react to returned domain-relevant results.

## NNTP Action Items

- [x] Refactor indexer NNTP fetches onto the semaphore-backed `nntp.Manager` path instead of a standalone provider.
- [x] Add manager capacity policy options: caller-managed `ErrProviderBusy` and manager-owned wait queue.
- [ ] Move downloader toward manager-owned wait-queue behavior after metrics prove the new policy is stable.
- [x] Add shared-pool module reservations with idle borrowing, so indexer can use the full pool when alone and yield a configured share when downloader work is active.
- [x] Combine downloader/indexer provider settings into shared runtime `servers` settings and expose `nntp_pool` reservation knobs in the WebUI.
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
- [x] Share the NNTP manager between downloader and indexer in all-in-one runtime builds while preserving downloader `ErrProviderBusy` behavior.
- [x] Add runtime-configurable NNTP pool idle borrow and usage percentages.
- [x] Add PAR2/yEnc step timing metrics and lift the PAR2 worker cap high enough for a configured concurrency of 20 to take effect.

Needs completion:

- [ ] New PAR2/yEnc step timing metrics are observed in live runs and used to decide whether the next bottleneck is NNTP, parse, fallback materialization, selection, or database writes.
- [ ] PAR2 result persistence is batched or consolidated enough that database round trips are not the dominant cost.
- [ ] yEnc work-item/rollup design provides fast exact dashboard counts and faster recovery candidate selection.
- [ ] Assemble lane A selection and lane B DB write/refresh bottlenecks are measured with SQL plans and resolved with true batch repository operations where needed.
- [ ] Downloader-specific manager-owned wait policy and richer active worker stats are handled in a separate downloader-focused session.
