# Indexer Assemble Release Queue And Lane Split Evaluation

Snapshot date: 2026-05-07

This doc is the active execution guide for the current stabilization sprint focused on:

- release queue coordination state
- assemble lane behavior under backlog pressure
- deciding whether to split assemble lane A and lane B into separate runtime-controlled stages

## Sprint Goal

Decide, with live measurements, whether the long-term model should be:

1. one assemble stage with smarter internal lane behavior
2. separate assemble subcommands that operators can run manually
3. a true lane A / lane B split with separate runtime settings and supervisor stages

## Working Hypothesis

- the previous hot-path improvements are still real
- the current slow assemble runs are dominated by lane B backlog drain plus inline repair behavior
- release selection quality improved, but release still depends on assemble delivering more complete families faster
- persistent queue state on `release_family_readiness_summaries` is a better long-term model than `release_stage_dirty_families`

## Evaluation Plan

1. capture a live baseline from the earlier pre-queue-merge assemble/release behavior
2. capture a live measurement from the current head
3. compare:
   - assemble throughput
   - lane A / lane B mix
   - yEnc recovery behavior
   - release candidate quality
   - formed releases per pass
4. decide whether lane splitting should be:
   - deferred
   - exposed as manual subcommands only
   - promoted to true runtime-controlled stages

## Decision Standard

Prefer a true lane split only if live measurements show:

- lane A remains consistently fast
- lane B continues to dominate slow batches
- independent scheduling or throttling would materially improve the release backlog

Prefer manual subcommands only if:

- the separation is useful for operator debugging
- but not important enough to justify more runtime/control-plane complexity yet

## Live Benchmark Notes

### Before

Live same-day baseline from the pre-queue-merge / pre-lane-B-recovery-tightening behavior on the current backlog:

- `2026-05-07 12:35:34`
  - `assemble`
  - `lane_a_selected=3585`
  - `lane_b_selected=3915`
  - `processed_headers=7500`
  - `headers_per_second=69.16`
  - `header_match_ms=55655.61`
  - `binary_upsert_ms=35639.67`
  - `binary_refresh_ms=10586.70`
  - `assemble_recovery_attempts=128`
  - `assemble_recovery_fetch_failures=128`
  - `assemble_recovery_skipped_by_cap=3132`

- `2026-05-07 12:35:47`
  - `assemble`
  - `lane_a_selected=0`
  - `lane_b_selected=7500`
  - `processed_headers=7500`
  - `headers_per_second=62.18`
  - `header_match_ms=56806.75`
  - `binary_upsert_ms=54449.02`
  - `binary_refresh_ms=7332.53`
  - `assemble_recovery_attempts=128`
  - `assemble_recovery_fetch_failures=128`
  - `assemble_recovery_skipped_by_cap=5925`

- representative release same window:
  - `2026-05-07 12:35:57`
  - `release`
  - `candidate_families=3404`
  - `formed=0`
  - `cooled_down_fragment_only_families=1125`
  - `skipped_fragments=2544`

Interpretation:

- slow assemble runs were dominated by lane B backlog drain
- inline yEnc recovery was still firing in those opaque lane B slices
- release saw a large backlog but still formed very little

### After

Live current-head benchmark after:

- moving active release queue state onto `release_family_readiness_summaries`
- removing eager new-binary queue churn
- keeping lane B off inline yEnc recovery

Measured on the same live backlog:

- `2026-05-07 13:09:33`
  - `assemble`
  - `lane_a_selected=5984`
  - `lane_b_selected=1516`
  - `processed_headers=7500`
  - `headers_per_second=203.77`
  - `header_match_ms=1775.37`
  - `binary_upsert_ms=13500.66`
  - `binary_refresh_ms=20138.39`
  - `assemble_recovery_attempts=0`

- `2026-05-07 13:09:55`
  - `assemble`
  - `lane_a_selected=0`
  - `lane_b_selected=7500`
  - `processed_headers=7500`
  - `headers_per_second=127.43`
  - `header_match_ms=1956.95`
  - `binary_upsert_ms=44347.53`
  - `binary_refresh_ms=11189.11`
  - `assemble_recovery_attempts=0`

- `2026-05-07 13:10:03`
  - `assemble`
  - `lane_a_selected=0`
  - `lane_b_selected=7500`
  - `processed_headers=7500`
  - `headers_per_second=112.20`
  - `header_match_ms=1919.60`
  - `binary_upsert_ms=45641.30`
  - `binary_refresh_ms=16557.63`
  - `assemble_recovery_attempts=0`

- `2026-05-07 13:10:13`
  - `assemble`
  - `lane_a_selected=0`
  - `lane_b_selected=7500`
  - `processed_headers=7500`
  - `headers_per_second=98.02`
  - `header_match_ms=2002.72`
  - `binary_upsert_ms=48850.07`
  - `binary_refresh_ms=22602.67`
  - `assemble_recovery_attempts=0`

- `2026-05-07 13:10:53`
  - `release`
  - `candidate_families=5000`
  - `formed=0`
  - `cooled_down_low_coverage_families=26`
  - `skipped_fragments=4974`

- live queue sample immediately before the release run:
  - pending summary-backed release families: `1283`

Interpretation:

- the hot-path regression from inline lane B yEnc recovery is gone
- lane A-heavy assemble batches are now much faster
- lane B-only batches are still materially slower than lane A, but their cost has shifted away from matching and into binary upsert / stats refresh work
- release still does not form enough from the live backlog even after assemble gets healthier batches

## Current Conclusion

We should do a true lane A / lane B split with separate runtime settings.

Reason:

- the live measurements show lane A and lane B have materially different performance profiles
- lane A is now healthy enough to run aggressively and keep feeding release
- lane B remains valuable backlog-burn-down work, but it still competes on write-heavy paths and should not share exactly the same runtime cadence and batch behavior as lane A
- manual subcommands are still useful for operator debugging, but they are not enough as the primary long-term control model

Recommended model:

1. `assemble_lane_a`
   - high frequency
   - larger priority share
   - can keep the current hot-path matching behavior
   - should be tuned to keep release backlogged

2. `assemble_lane_b`
   - lower frequency
   - independently tunable batch size and concurrency
   - no inline yEnc recovery on the hot path
   - positioned as backlog drain and deferred repair feeder

3. keep a compatibility `assemble` command
   - for operators who want the combined behavior manually
   - but move scheduled runtime control to the split stages

## Implementation Update

The lane split is now implemented in the current branch:

- new supervisor stages:
  - `assemble_lane_a`
  - `assemble_lane_b`
- new runtime settings:
  - `indexing.assemble_lane_a`
  - `indexing.assemble_lane_b`
- compatibility/manual commands:
  - `gonzb indexer assemble --once`
  - `gonzb indexer assemble lane-a --once`
  - `gonzb indexer assemble lane-b --once`

Behavior:

- lane A now claims only structured-priority headers that feed existing incomplete binaries
- lane B claims recent non-priority headers and excludes the structured-incomplete-binary path
- lane B still keeps inline yEnc recovery off the hot path
- combined `assemble` remains available for compatibility and manual mixed runs

## Post-Implementation Live Baseline

Live runs against the same large backlog after the true split landed:

- `2026-05-07 13:30:33`
  - `assemble lane-a --once`
  - `lane_a_selected=5000`
  - `lane_b_selected=0`
  - `processed_headers=5000`
  - `headers_per_second=379.73`
  - `header_match_ms=776.96`
  - `binary_upsert_ms=539.10`
  - `binary_refresh_ms=1190.73`
  - `unique_binary_upserts=233`
  - `binary_upsert_cache_hits=4767`
  - `assemble_recovery_attempts=0`

- `2026-05-07 13:30:44`
  - `assemble lane-b --once`
  - `lane_a_selected=0`
  - `lane_b_selected=2500`
  - `processed_headers=2500`
  - `headers_per_second=377.28`
  - `header_match_ms=410.69`
  - `binary_upsert_ms=3912.70`
  - `binary_refresh_ms=1656.82`
  - `unique_binary_upserts=1886`
  - `binary_upsert_cache_hits=614`
  - `assemble_recovery_attempts=0`

- `2026-05-07 13:31:47`
  - `assemble lane-a --once`
  - `lane_a_selected=954`
  - `lane_b_selected=0`
  - `processed_headers=954`
  - `headers_per_second=107.58`
  - `binary_upsert_ms=302.17`
  - `unique_binary_upserts=141`

- `2026-05-07 13:32:04`
  - `release --once`
  - `candidate_families=5000`
  - `formed=0`
  - `cooled_down_low_coverage_families=21`
  - `skipped_fragments=4979`

Interpretation:

- the old lane B 45-second upsert symptom was mostly a mixed-batch problem, not a pure lane B identity-matching problem
- once lane B is isolated and given a smaller batch, it remains more write-heavy than lane A, but it no longer monopolizes the assemble pass
- lane A is now a very efficient feed stage when structured priority work exists
- release is still overwhelmingly fragment-bound, so selector and queue coordination are healthier, but the backlog itself still does not contain enough formable families yet

## Baseline To Beat

Any follow-up lane tuning should now be judged against these split-stage live samples:

- lane A pass around `379.73 headers/sec` on a full `5000` priority batch
- lane A `binary_upsert_ms` well under `1s` when cache reuse is high
- lane B pass around `377.28 headers/sec` on a `2500` batch
- lane B `binary_upsert_ms` around `3.9s` with `1886` unique binary upserts
- release still showing `formed=0` on a `5000` family pass with `4979` fragment skips
