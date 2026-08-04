# GoNZB Stabilization Audit (February 23, 2026)

## Scope
- Reviewed architecture and behavior from `README.md` and `ARCHITECTURE.md`.
- Audited core backend packages under `cmd/` and `internal/` for reliability, error handling, fragility, and security.
- Ran static validation:
  - `GOCACHE=/tmp/gocache go test ./...` (builds, no tests present)
  - `GOCACHE=/tmp/gocache go vet ./...` (1 issue reported)

## Executive Summary
The project has a clean modular structure and compiles successfully, but there are several high-impact stability bugs in queue persistence, NZB retrieval/caching, and NNTP connection handling. The most important pre-feature work is to harden job lifecycle correctness (recoverability and no false-complete states), make NZB caching atomic, and close nil/error edge cases in API and NNTP code.

## Findings

### 1. High: Nil-pointer crash in download endpoint for missing IDs
- Evidence: `internal/api/controllers/newznab.go:98`, `internal/api/controllers/newznab.go:104`
- Problem: `GetResultByID` can return `(nil, nil)` when an ID does not exist, but `res` is dereferenced immediately (`res.RedirectAllowed`, `res.ID`).
- Impact: A crafted/nonexistent `id` can panic the handler and potentially crash the process depending on server panic recovery behavior.
- Recommendation: Explicitly handle `res == nil` and return `404` before any dereference.

### 2. High: NZB cache poisoning via partial writes
- Evidence: `internal/indexer/manager.go:126`, `internal/indexer/manager.go:134`, `internal/store/store.go:57`, `internal/store/store.go:60`
- Problem: NZBs are written directly to final cache path via `io.TeeReader`. If caller reads partially/cancels, truncated `.nzb` is left in cache and `Exists()` treats it as valid forever.
- Impact: Persistent parse failures/retries and hard-to-recover download failures after transient network interruptions.
- Recommendation: Write to temp file and atomically rename on successful full read; invalidate/delete partial files on read/copy errors.

### 3. High: Queue durability hole can lose pending jobs
- Evidence: `cmd/gonzb/main.go:210`, `cmd/gonzb/main.go:215`, `internal/store/queue.go:101`
- Problem: CLI path inserts queue item before release metadata is persisted; queue reload queries use `JOIN releases`, so orphaned queue rows are invisible after restart/crash.
- Impact: Jobs can disappear from active queue recovery, violating persistent queue expectations.
- Recommendation: Persist release first (or transactionally persist release + queue item), and use recovery query strategy that can surface orphaned rows for repair.

### 4. High: NNTP handshake implementation is fragile and can hang/fail with valid servers
- Evidence: `internal/nntp/provider.go:172`, `internal/nntp/provider.go:175`
- Problem: Code reads greeting with `ReadCodeLine(200)`, then performs a second read for 201. For servers replying 201 on first line, the second read waits for another line that may never come.
- Impact: Connection setup stalls or fails against standards-compliant providers that return 201 (common "no posting" greeting).
- Recommendation: Parse single greeting response and accept both 200/201 without issuing a second read.

### 5. High: IPv6 server addresses are mishandled
- Evidence: `internal/nntp/provider.go:142`; `go vet` output: `address format "%s:%d" does not work with IPv6`
- Problem: Address formatting uses string concatenation pattern instead of `net.JoinHostPort`.
- Impact: IPv6 NNTP hosts fail to connect.
- Recommendation: Replace with `net.JoinHostPort(p.conf.Host, strconv.Itoa(p.conf.Port))`.

### 6. Medium: Restart path can incorrectly mark jobs complete without task hydration
- Evidence: `internal/engine/manager.go:61`, `internal/engine/manager.go:63`, `internal/engine/manager.go:184`, `internal/engine/manager.go:388`
- Problem: Only `downloading` is reset to `pending`; `processing` is not. A recovered `processing` item may have `Tasks == nil`, then `PostProcess(nil)` returns nil and finalize marks completed.
- Impact: False-positive completed status after crash/restart.
- Recommendation: Reset both `downloading` and `processing` to `pending`, or re-hydrate tasks before allowing post-process/complete transitions.

### 7. Medium: Existing file checks are too weak and can skip required downloads
- Evidence: `internal/processor/processor.go:91`
- Problem: File existence alone sets `IsComplete=true`; no size/hash/CRC validation occurs before skipping download.
- Impact: Truncated/corrupt files can be treated as done, causing downstream repair/extraction failures.
- Recommendation: Validate size at minimum; ideally validate via PAR/CRC when available before marking complete.

### 8. Medium: Extraction workflow can collide and overwrite files
- Evidence: `internal/processor/extractor.go:28`, `internal/processor/extractor.go:58`
- Problem: Work directory naming is deterministic (`_extracted<archive-base>`) and extraction output is flattened to `destDir` by basename only.
- Impact: Concurrent/same-name archive collisions, accidental overwrite, and directory structure loss.
- Recommendation: Use unique temp dirs (`MkdirTemp`) and preserve relative paths (with traversal-safe normalization).

### 9. Medium: API surface is unauthenticated and logs full query URIs
- Evidence: `internal/api/router.go:27`, `internal/api/controllers/newznab.go:19`, `internal/api/router.go:19`
- Problem: `/api` and `/nzb/:id` have no auth check; request logging includes full URI (including query params).
- Impact: If exposed beyond trusted network, endpoints can be abused and secrets in query params can be logged.
- Recommendation: Add API-key middleware and redact sensitive query params in logs.

### 10. Medium: Outbound indexer HTTP requests are brittle
- Evidence: `internal/indexer/newsnab/client.go:33`, `internal/indexer/newsnab/client.go:36`, `internal/indexer/newsnab/client.go:37`, `internal/indexer/newsnab/client.go:66`, `internal/indexer/newsnab/client.go:70`
- Problem: Query is not URL-escaped; `http.NewRequestWithContext` errors are ignored; `http.DefaultClient` has no timeout.
- Impact: Broken searches for special characters, potential panic/null request handling issues, and hung goroutines under slow upstreams.
- Recommendation: Use `url.Values`, handle request creation errors, and use a configured `http.Client` with sensible timeout.

### 11. Medium: Cancel behavior is inconsistent for pending jobs
- Evidence: `internal/engine/manager.go:252`
- Problem: `Cancel` only calls `CancelFunc`; pending jobs may not yet have a cancel func, but method still returns `true` and does not change status.
- Impact: Users see successful cancel response while job remains runnable.
- Recommendation: For pending items, immediately set status to failed/cancelled and persist it.

### 12. Low: Fatal logging inside parser library path
- Evidence: `internal/nzb/parser.go:19`
- Problem: `ParseFile` calls `log.Fatal` on open failure, terminating process from a library function.
- Impact: Hard process exits bypassing normal cleanup and error flow.
- Recommendation: Return errors instead of exiting.

### 13. Low: Database persistence errors are swallowed in queue lifecycle
- Evidence: `internal/engine/manager.go:285`, `internal/engine/manager.go:306`
- Problem: Queue status writes ignore returned errors.
- Impact: In-memory and persisted state can diverge silently.
- Recommendation: Log and surface persistence failures, and consider retry policy for critical status transitions.

### 14. Low: CLI progress path leaks debug output and skips explicit app shutdown
- Evidence: `cmd/gonzb/main.go:272`, `cmd/gonzb/main.go:251`
- Problem: Raw debug bytes are always printed during CLI downloads; early returns don’t call `appCtx.Close()`.
- Impact: Noisy UX and less deterministic shutdown/flush behavior.
- Recommendation: Gate debug output behind log level and defer `appCtx.Close()` in CLI execution path.

### 15. Low: Event streaming controller likely fails JSON serialization when active item is present
- Evidence: `internal/api/controllers/events.go:23`, `internal/api/controllers/events.go:77`
- Problem: `domain.QueueItem` includes non-serializable fields (for example `context.CancelFunc`), while marshal errors are ignored.
- Impact: SSE payloads may silently become empty/invalid if this controller is wired in.
- Recommendation: Emit a dedicated DTO for event payloads and check marshal errors.

## Gaps
- No unit/integration tests are currently present (`go test ./...` reports no test files).
- No race-focused test coverage exists for queue/download state transitions.

## Priority Stabilization Plan
1. Fix queue correctness first: findings #3 and #6.
2. Harden NZB retrieval/cache atomics: finding #2.
3. Fix NNTP connectivity robustness: findings #4 and #5.
4. Remove crash/edge-case API failures: findings #1 and #10.
5. Improve safety and observability: findings #7, #9, #11, #13.
6. Add baseline tests around queue restart recovery and interrupted NZB fetches.
