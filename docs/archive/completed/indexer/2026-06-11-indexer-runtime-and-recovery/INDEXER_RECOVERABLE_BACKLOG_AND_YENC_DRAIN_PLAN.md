# Indexer Recoverable Backlog And yEnc Drain Plan

Snapshot date: 2026-06-05

This doc captures the current upstream work plan after the release ready-queue refactor.

## Current State

- `release` is no longer the bottleneck and no longer churns on fragment suppression.
- `release_ready_candidates` is currently empty in live runs.
- The upstream parked backlog is dominated by weak unresolved inventory:
  - dirty `release_family weak_single_binary`: `9,324,195`
  - dirty `release_family fragment_only`: `2,335,129`
  - dirty `release_family actionable`: `3,087`
- `recover_pending` classification is effectively absent in `release_family_readiness_summaries`:
  - total `release_family recover_pending`: `0`
  - total `base_stem recover_pending`: `0`
- `recover_yenc` is productive but under-prioritized relative to the unresolved universe:
  - work items `ready=330,121`, `done=18,570`
  - recent throughput at `concurrency=8`: about `1000` attempted per `80-90s`, with `~930-955` recovered and `~825-849` merged
- unresolved obfuscated main-payload binaries remain much larger than the queued recovery surface:
  - unrecovered obfuscated main-payload binaries: `13,340,444`
  - recovered obfuscated main-payload binaries: `16,982`
- binary completeness is a parallel blocker:
  - main-payload multipart binaries: `2,538,501`
  - complete multipart main-payload binaries: `22,419`
  - single-part main-payload binaries: `10,896,577`

## Working Interpretation

- The main bottleneck is upstream, not `release`.
- Most parked single-binary and fragment rows are unresolved weak inventory that likely needs yEnc BODY recovery to improve identity.
- The current family-summary model does not represent blank-family unresolved binaries well enough to expose them as `recover_pending`.
- Multipart binary completion will not improve while both assemble lanes remain disabled.

## Execution Goals

1. Add a recoverable-backlog visibility surface.
2. Add explicit unresolved / recoverable classification without forcing blank-family rows through `release_family_readiness_summaries`.
3. Prioritize `recover_yenc` toward high-value recoverables that can unlock release formation.
4. Re-enable only the incomplete-binary-first assemble path in a controlled way.
5. Re-measure before changing assemble grouping heuristics again.

## Planned Work

### 1. Recoverable Backlog Visibility

Add a refresh- or maintenance-owned read model for unresolved parked work, separate from `release_ready_candidates`.

Track at least:

- `weak_single_binary`
- `fragment_only`
- yEnc-eligible unresolved binaries
- multi-binary fragment families
- expected-file-count-backed families
- multipart incomplete binaries

Do not use `release` logs as the primary backlog-analysis surface anymore.

### 2. Recover-Pending Classification

The current `recover_pending=0` result means the family-summary model is not representing unresolved weak inventory correctly.

Required direction:

- add a dedicated derived surface for recoverable unresolved candidates
- key it at binary level or another unresolved-cluster level
- do not force blank-family unresolved binaries into `release_family_readiness_summaries`
- keep one heavy materializer for this derived state, preferably `release_summary_refresh` or a dedicated maintenance-style materializer

### 3. yEnc Priority Tiers

High-priority recoverables:

- `expected_file_count > 1`
- `file_index > 0`
- `total_parts > 1`
- fragment families with `binary_count > 1`
- unresolved binaries with blank promotable family identity

Low-priority recoverables:

- true single-binary opaque singles with no multi-file evidence

Desired outcome:

- seed and claim high-priority work first
- spend BODY fetch capacity where it can unlock better grouping and release formation

### 4. Controlled Throughput Increase

- keep the query-stability guard on `recover_yenc`
- keep current live-safe default around `concurrency=8` until prioritization is proven
- after priority tiers are live, `12` is the next reasonable runtime step
- avoid jumping back to `16+` before the queue is value-ranked

### 5. Controlled Assembly Re-enable

When runtime is ready for drain mode:

- enable `assemble_lane_a` only
- keep `assemble_lane_b` off
- keep scrape off
- use small/moderate batch and concurrency settings
- focus on incomplete-binary completion rather than broad new grouping churn

### 6. Post-Drain Re-Audit

After yEnc prioritization and lane A are active, re-measure:

- `yenc_recovery_work_items ready/done`
- unresolved binaries recovered
- multipart binaries completed
- `release_ready_candidates`
- formed releases

Only if ready candidates remain near zero after that should the next patch move back into assemble grouping heuristics.

## Current Implementation Direction

The current code direction supporting this plan is:

- keep weak provisional identity out of normal release promotion
- keep `release` consuming only ready candidates
- improve `recover_yenc` seeding so multi-file and multipart evidence is pulled ahead of opaque singles
- fix tooling such as `articleprobe` so BODY inspection uses the runtime indexer NNTP credentials

## 2026-06-06 Release Summary Refresh Follow-Up

Two refresh-path issues were confirmed live after the ready-only `release` queue work:

1. `indexer_maintenance` was still issuing large readiness-summary delete passes while `release_summary_refresh` backlog remained non-zero.
   - This was competing directly with refresh on `release_family_readiness_summaries`.
   - Maintenance now defers readiness-summary and ready-candidate cleanup when `release_family_summary_refresh_queue` is non-empty.

2. `release_summary_refresh` was issuing follow-up recovered-file-set queries on the same `*sql.Conn` while a `SELECT DISTINCT ... file_set_key` cursor was still open.
   - That left Postgres sessions `idle in transaction` and stalled refresh progress.
   - The cursor is now drained and closed before follow-up refresh queries run.

Additional follow-up:

- recovered-file-set aggregate reads were batched by provider/file-set set instead of forcing the same aggregate shape through repeated single-key calls where multiple keys are present

Live re-test after these fixes showed:

- the old `idle in transaction` refresh stall disappeared
- `indexer_maintenance` no longer re-entered the old readiness-summary purge on the patched run
- the next remaining hot path is recovered-file-set aggregate work inside refresh, not `release`

At the end of this pass:

- `release` remained healthy and continued returning `candidate_families=0`
- `release_family_summary_refresh_queue` was still `230923`
- `release_ready_candidates` was still `0`

So the current conclusion is:

- `release` is still not the bottleneck
- the refresh path is healthier than before, but still not yet draining the backlog quickly enough to feed the ready queue
- the next audit target should stay inside `release_summary_refresh`, especially the remaining recovered-file-set refresh cost and any stale stage-runtime state around the refresh stage

## 2026-06-05 Live Tuning Results

The runtime settings API was used against live `serve` on `config.yaml` with `X-API-Key` auth.

### Baseline Live State Before Drain Tuning

- `assemble_lane_a` disabled
- `recover_yenc` enabled at `batch_size=1000`, `concurrency=8`
- inspect stages enabled
- `release_ready_candidates=0`

The initial live supervisor shape was incorrect for a recovery drain and allowed unnecessary inspect pressure.

### Drain Profile A

Applied through the admin settings API:

- `scrape_*` disabled
- `assemble_lane_a` enabled at `batch_size=5000`, `concurrency=1`, `interval_minutes=0.3`
- `assemble_lane_b` disabled
- `recover_yenc` enabled at `batch_size=1000`, `concurrency=8`, `interval_minutes=0.3`
- inspect, archive/NZB tail, and enrichment stages disabled

Observed:

- `assemble_lane_a` immediately did useful work:
  - `processed_headers=3341`
  - `binaries_refreshed=1376`
- `recover_yenc` also did useful work before failure:
  - reached `attempted=600/1000`
  - `recovered=558`
  - `merged=490`
- queue movement after this pass:
  - `yenc_done: 18570 -> 18615`
  - `yenc_ready: 330121 -> 334294`
- `release_ready_candidates` remained `0`

Failure:

- Postgres still crashed under this profile
- repeated error:
  - `select yenc recovery work item backfill binaries: unexpected EOF`
  - followed by `the database system is in recovery mode`

Conclusion:

- `8x1000` is too aggressive for the current yEnc selector path

## 2026-06-11 Integrity Incident

The later unrecoverable Postgres crash was not caused by the release-ready schema work.

Confirmed root cause from the Postgres container log:

- recovery repeatedly PANICed on `_bt_restore_page: cannot add item to page`
- WAL context pointed at relation `1663/16384/16837`
- relation `16837` resolves to `public.article_headers_newsgroup_id_message_id_key`

This is the hot unique B-tree on `article_headers(newsgroup_id, message_id)`, which is exercised by scrape/header ingest.

### Immediate containment now implemented

- added explicit integrity commands:
  - `indexer maintenance check-integrity`
  - `indexer maintenance reindex-critical`
- scrape now runs a critical-ingest-index preflight before inserting headers
- if the critical `article_headers` indexes fail integrity checks, scrape aborts instead of continuing to write

### Current interpretation

- release formation and `release_summary_refresh` were real application bottlenecks and were improved
- the database corruption is a separate ingest/index integrity problem
- the next larger redesign should reduce direct scrape pressure on the canonical `article_headers` uniqueness indexes rather than treating the release pipeline as the primary cause

### 2026-06-11 live integrity results

- metadata-only integrity checks passed once the cluster came back up
- deep `amcheck` then confirmed both secondary `article_headers` uniqueness indexes are corrupt:
  - `article_headers_newsgroup_id_article_number_key`
  - `article_headers_newsgroup_id_message_id_key`
- targeted reindex did **not** repair the cluster cleanly:
  - `ERROR: could not access status of transaction ...`

Current operational conclusion:

- scrape must remain blocked against this cluster
- this is likely deeper heap / transaction-status corruption, not just one bad B-tree page
- restore-from-backup is now the preferred recovery path unless a lower-level PostgreSQL salvage procedure is explicitly chosen

### Drain Profile B

Applied through the admin settings API:

- kept `assemble_lane_a=5000x1`
- reduced `recover_yenc` to `batch_size=500`, `concurrency=4`, `interval_minutes=0.3`

Observed:

- runtime restart interrupted the prior in-flight `8x1000` batch, so some `context canceled` fetch lines were expected
- after restart, the system still failed again on the same selector path

Failure:

- Postgres crashed again at the yEnc backfill selector
- same `unexpected EOF` / recovery-mode pattern

Conclusion:

- reducing to `4x500` was not enough

### Drain Profile C

Applied through the admin settings API:

- kept `assemble_lane_a=5000x1`
- reduced `recover_yenc` to `batch_size=100`, `concurrency=2`, `interval_minutes=1.0`

Observed:

- queue state after this pass:
  - `yenc_done=18638`
  - `yenc_ready=335633`
- `release_ready_candidates` still `0`

Failure:

- Postgres still crashed again on the same selector path

Conclusion:

- even `2x100 @ 1m` is not sufficient to stabilize the current selector

### Isolation Check

Applied through the admin settings API:

- disabled `recover_yenc`
- left `assemble_lane_a` enabled
- left `release_summary_refresh`, `release`, and `indexer_maintenance` enabled

Observed:

- `indexer_maintenance` alone still crashed with:
  - `select yenc recovery work item backfill binaries: unexpected EOF`
- queue state after this isolation pass:
  - `yenc_done=18646`
  - `yenc_ready=335592`

Conclusion:

- the root instability is not just `recover_yenc` concurrency
- the shared yEnc backfill selector path itself is unstable under `serve`
- `indexer_maintenance` also invokes it, so API-only runtime tuning cannot fully stabilize yEnc drain behavior

## Updated Operational Conclusion

No safe live `recover_yenc` tuning profile was found in `serve` using runtime settings alone.

The current limiting factor is:

- the yEnc work-item backfill selector path
- not the raw BODY fetch concurrency number

The highest-value next code work is now:

1. split or rewrite the yEnc backfill selector so it stops crashing Postgres
2. isolate maintenance from the same selector or make maintenance runtime-configurable
3. only after that, resume live tuning of `recover_yenc` concurrency and batch size

## Best Temporary Runtime Posture

For live `serve`, the safest temporary posture found was:

- `assemble_lane_a` enabled conservatively
- `recover_yenc` disabled

This is not good enough for backlog drain, but it avoids repeatedly crashing Postgres until the selector path is fixed in code.

## 2026-06-05 Selector Rewrite Follow-Up

Additional code work was completed after the failed profiles above:

- moved yEnc work-item backfill prioritization out of one large SQL statement and into staged branch selection in Go
- kept the query-local Postgres parallelism guard
- changed release-family refresh queue writes from `ON CONFLICT ... DO UPDATE` to `ON CONFLICT ... DO NOTHING` to reduce queue-row lock churn

### Runtime Outcome After Rewrite

Two distinct live states were observed:

1. With `release_summary_refresh` and `release` still enabled:
   - Postgres no longer crashed
   - `recover_yenc` and `indexer_maintenance` could stay running, but they were slow and contended with summary-refresh queue work
   - stage runs stayed open too long and were prone to lease expiry or restart interruption

2. With a focused drain profile:
   - `assemble_lane_a` enabled at `5000x1`
   - `recover_yenc` enabled at `50x1`
   - `release_summary_refresh` disabled
   - `release` disabled
   - scrape / inspect / enrich / archive tail disabled

   Live result:
   - `recover_yenc` completed successfully
   - logged:
     - `candidates=50`
     - `attempted=50`
     - `recovered=47`
     - `merged=41`
     - `not_found=3`
     - `fetch_failures=0`
     - `parse_failures=0`
     - `concurrency=1`
   - stage metrics:
     - `candidate_selection_ms ~= 21453`
     - `processing_ms ~= 45246`
   - queue movement:
     - `yenc_work_items done: 18648 -> 18654`
     - `yenc_work_items ready: 335590 -> 335543`
   - no Postgres recovery-mode event occurred during this successful run

### Updated Fine-Tuned Recovery Profile

Current known-good recovery drain profile:

- `recover_yenc=batch_size: 50`
- `recover_yenc=concurrency: 1`
- `assemble_lane_a=batch_size: 5000`
- `assemble_lane_a=concurrency: 1`
- `release_summary_refresh=false`
- `release=false`
- `scrape=false`
- `inspect*=false`
- `enrich*=false`

### Current Interpretation

- The yEnc selector regression is fixed enough to avoid Postgres crashes.
- The best live profile found in this session is a focused single-worker recovery drain, not a mixed full pipeline.
- Re-enabling `release_summary_refresh` too early still creates enough downstream contention to erase the yEnc throughput win.
- The next step is to keep using the focused drain profile to accumulate more recovered identities, then reintroduce `release_summary_refresh` carefully once the yEnc backlog has been reduced further.

## 2026-06-05 High-Throughput yEnc Tuning Sweep

After the selector fix and focused drain profile were proven stable, a direct concurrency sweep was run with:

- `recover_yenc.batch_size=1000`
- `assemble_lane_a=5000x1`
- `release_summary_refresh=false`
- `release=false`
- scrape / inspect / enrich / archive tail disabled

### `1000x8`

Completed cleanly.

- `candidates=1000`
- `attempted=1000`
- `recovered=922`
- `merged=827`
- `not_found=72`
- `processing_ms ~= 104600`
- `candidate_selection_ms ~= 23972`

Queue movement after the run:

- `done: 18654 -> 18749`
- `ready: 335543 -> 334621`

### `1000x12`

Completed cleanly.

- `candidates=1000`
- `attempted=1000`
- `recovered=923`
- `merged=838`
- `not_found=71`
- `processing_ms ~= 70497`
- `candidate_selection_ms ~= 26152`

Queue movement after the run:

- `done: 18749 -> 18834`
- `ready: 334621 -> 333698`

### `1000x16`

Completed cleanly.

- `candidates=1000`
- `attempted=1000`
- `recovered=882`
- `merged=812`
- `not_found=111`
- `processing_ms ~= 52720`
- `candidate_selection_ms ~= 26178`

Queue movement after the run:

- `done: 18834 -> 18904`
- `ready: 333698 -> 332816`

### Tuning Interpretation

- `1000x8` is stable and materially better than single-worker drain mode.
- `1000x12` is the best balance found in this session:
  - nearly the same recovery quality as `8`
  - substantially lower wall time
  - better merged-per-minute rate than `8`
- `1000x16` is the fastest raw throughput profile found in this session, but:
  - recovery quality dropped
  - `not_found` climbed noticeably
  - it appears to trade more NNTP misses / less effective selection quality for lower batch wall time

### Current Recommendation

Use:

- `recover_yenc.batch_size=1000`
- `recover_yenc.concurrency=12`

if the goal is best balanced backlog drain.

Use:

- `recover_yenc.batch_size=1000`
- `recover_yenc.concurrency=16`

only if the goal is maximum raw backlog burn and the lower recovery efficiency is acceptable.

## 2026-06-06 Noop Cooldown Follow-Up

The next bottleneck after the selector rewrite was repeated `noop` cycling. Overnight, `recover_yenc` remained stable but converged into a low-yield tail:

- batches around `690-725` candidates
- `recovered=0`
- `noops=682`
- low double-digit `not_found`
- `fetch_failures=0`

That indicated the queue was no longer limited by raw BODY throughput. It was reselecting the same non-improving binaries immediately.

### Fix Applied

- Added durable `noop` cooldown handling in `recover_yenc`
- successful yEnc recovery now clears retry state fully on the source payload
- `noop` backoff uses shorter intervals than true `not_found`
  - 15 minutes
  - 1 hour
  - 6 hours
  - 24 hours

### Live Validation

Ran `serve` on the patched build with the same drain posture and `recover_yenc=1000x16`.

Completed run:

- `candidates=1000`
- `attempted=1000`
- `recovered=0`
- `merged=0`
- `noops=648`
- `not_found=352`
- `processing_ms ~= 26807`
- `candidate_selection_ms ~= 24805`

Queue state shifted in the intended direction even though the batch remained low-yield:

- before run:
  - `ready_at <= now()`: `99790`
  - `ready_at > now()`: `21776`
  - `done`: `36074`
- after run:
  - `ready_at <= now()`: `99315`
  - `ready_at > now()`: `22251`
  - `done`: `36074`

Interpretation:

- the hot queue shrank by `475`
- future-backed-off ready rows increased by `475`
- the system stopped immediately recycling a large slice of the noop tail

This does not create new recoveries by itself, but it improves useful throughput by preserving hot slots for candidates that have not already proven non-improving.

## 2026-06-06 Candidate Selection Audit Follow-Up

### Root Cause

The large `candidate_selection_ms` was not caused by the ready-item listing query itself. Live `EXPLAIN ANALYZE` on the ready-item list completed in about `138ms`.

The expensive step was the unconditional top-up path:

- `ListYEncRecoveryCandidates` always called `ensureYEncRecoveryWorkItemsSeed`
- seed/backfill scanned large portions of `binaries`
- one representative branch query took about `12.6s` on its own

### Fix Applied

- `ListYEncRecoveryCandidates` now checks the existing ready queue first
- top-up only runs when there is a real shortfall
- top-up size is based on shortfall instead of always running a large seed pass
- stale ready work items with missing ingest payloads are retired before counting hot capacity
- ready-queue counting now counts only joinable candidates, not raw `ready` rows

### Live Validation

Before the clean run:

- `ready_at <= now()`: `103152`
- `ready_at > now()`: `18414`
- `stale`: `0`
- `done`: `36074`

Observed runs:

1. first run with a full hot queue

- `candidates=1000`
- `noops=624`
- `not_found=376`
- `candidate_selection_ms ~= 3380`

2. next run after the hot queue was mostly pushed out of immediate readiness

- `candidates=46`
- `not_found=46`
- `candidate_selection_ms ~= 24402`

3. next run after that

- `candidates=0`
- `candidate_selection_ms ~= 24954`

After the clean pass:

- `ready_at <= now()`: `0`
- `ready_at > now()`: `15990`
- `stale`: `105576`
- `done`: `36074`

### Interpretation

- the queue-first/top-up-on-shortfall change worked
- when the ready queue already has enough joinable work, selection cost drops dramatically
- when the ready queue is exhausted, selection cost rises again because the system must rescan `binaries` to find new seedable candidates
- retiring stale ready rows exposed that a very large portion of the work-item table was not actually joinable anymore

The current bottleneck has shifted:

- not the ready-item list
- not repeated noop cycling
- now it is the fallback seed/backfill scan when the joinable ready queue is empty or nearly empty

## 2026-06-06 Empty-Queue Seed Scan Backoff

### Why This Was Needed

After queue-first candidate selection was added, the next failure mode was:

- when the joinable ready queue was exhausted
- `recover_yenc` still rescanned `binaries` every minute
- those empty or near-empty top-up scans still cost about `24-25s`

An index-based fix was attempted first, but creating the large partial indexes caused a Postgres 17 backend crash during migration. That approach was reverted.

### Safer Runtime Fix

Added an in-process seed-scan backoff in the PG store:

- if the ready queue already has candidates, clear any prior seed-scan backoff
- if the ready queue is empty and a top-up scan finds nothing, back off future top-up scans
- backoff escalates:
  - 1 minute
  - 5 minutes
  - 15 minutes

This avoids paying the full `binaries` rescan cost every minute after repeated empty scans.

### Live Validation

Baseline before the run:

- `ready_at <= now()`: `24`
- `ready_at > now()`: `15966`
- `stale`: `105576`
- `done`: `36074`

Observed runs:

1. low-ready top-up scan

- `candidates=22`
- `noops=15`
- `not_found=7`
- `candidate_selection_ms ~= 23625`

2. next low-ready scan

- `candidates=1`
- `not_found=1`
- `candidate_selection_ms ~= 25162`

3. first empty scan after that

- `candidates=0`
- `candidate_selection_ms ~= 25229`

4. next scan while backoff is active

- `candidates=0`
- `candidate_selection_ms ~= 514`

Queue after the run:

- `ready_at <= now()`: `0`
- `ready_at > now()`: `12778`
- `stale`: `108788`
- `done`: `36074`

### Interpretation

- the fallback seed scan is still expensive when it actually runs
- the new backoff prevents paying that full cost every minute once the queue is exhausted
- one more refinement remains possible:
  - treat extremely low-yield top-up scans, not just fully empty scans, as near-empty and enter backoff sooner

## 2026-06-08 Release Summary Refresh Throughput Progress

### What Changed

Two more refresh-path changes landed:

- hot refresh dequeue no longer uses one union-and-rank CTE over the mixed dirty queue
- hot refresh now dequeues in branch order inside one transaction:
  - actionable
  - fragment-only
  - `base_stem`
  - missing-summary rows that still have backing binaries
- `release_family` summary aggregation is chunked into much smaller sub-batches

### Live Validation

After repairing stale stage-runtime rows and starting a fresh `serve`:

- `release_summary_refresh` completed a batch instead of stalling indefinitely
- `release_family_summary_refresh_queue` moved from `230923 -> 230921`
- `release_ready_candidates` moved from `0 -> 2`
- `release` immediately consumed those two candidates on the next pass

Observed release metrics for that pass:

- `candidate_families=2`
- `candidate_families_inspected=2`
- `cooled_down_low_coverage_families=2`
- `formed=0`

### Current Remaining Bottleneck

The bottleneck has moved again:

- no longer the hot dequeue selection path
- no longer `release` candidate selection
- now the dominant query is the `WITH requested(...)` batched `release_family` summary aggregation over `binaries`

Some refresh batches are now committing, but later batches can still spend tens of seconds to over a minute in that summary aggregation query depending on the selected family keys.

### Current Interpretation

- the dequeue path was a real bottleneck and is now materially improved
- refresh can now surface real candidates to `release`
- the next throughput gain needs to come from the `release_family` batch summary aggregation path itself, not from `release` or `recover_yenc`
