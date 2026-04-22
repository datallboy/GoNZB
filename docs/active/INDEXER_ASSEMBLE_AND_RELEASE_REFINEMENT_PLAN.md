# Indexer Assemble And Release Refinement Plan

Snapshot date: 2026-04-21

This is the current active execution plan for the post-stabilization refinement loop.

Update on 2026-04-22:

- this document remains active as the baseline refinement record
- the current day-to-day execution plan has moved to `docs/active/INDEXER_BACKLOG_BURNDOWN_PERFORMANCE_PLAN.md`
- live validation sign-off was completed later on `2026-04-22`; keep this document as the baseline record until the active-doc transition is performed

The storage, schema, and runtime-stability pass is mostly complete. The current bottleneck has shifted to throughput and work ordering:

- `article_headers` backlog is still large
- assemble is safe, but it processes a very large undifferentiated pending set
- release is still spending time on dirty families that are not actionable yet because their binaries are fragment-only

## Current Measured State

- `1,605,290` unassembled headers
- `93,824` dirty release families
- `2,824` complete binaries
- `57,047` incomplete binaries
- the old junk `1/1 + expected_file_count=0` binary pattern is no longer present in the live DB

Interpretation:

- assemble quality is much better than before
- release quality guardrails are working
- the next iteration should optimize throughput and queue quality, not weaken release safety

## Goals

- clear the assemble backlog faster without regressing grouping quality
- prioritize headers that improve already-started binaries before spending equal time on brand-new fragment families
- make `release --once` spend most of its time on actionable work
- reduce dirty-family queue noise by cooling down non-actionable fragment-only families
- leave the system ready for Phase 3 API/UI work once backlog behavior is stable

## Non-Goals

- reopening schema-baseline or payload-split decisions unless a new blocker appears
- weakening release persistence rules just to make more rows appear in `releases`
- broad API/UI work before assemble/release throughput is stable

## Key Decisions

### Release formation remains conservative

Releases should be formed from complete binaries, not from fragment-only binary sets.

Working rule:

- a family with `binary_count > 0` and `complete_binary_count = 0` is not release-actionable
- those families should not remain hot in `release_stage_dirty_families`
- they should be cooled down and requeued only when binary progress changes

Why this is the chosen direction:

- `completion_pct` is derived from binary-level completeness
- fragment-only families are currently known non-work for release formation
- leaving them dirty forever makes the queue noisy and causes repeated `formed=0` passes
- binary updates already exist as the correct requeue trigger

### Assemble prioritization should favor progress on existing binaries

The current newest-first pending-header scan is safe, but it does not distinguish between:

- headers that could complete or materially improve an already-started binary
- headers that belong to brand-new fragment families

The refinement phase should explicitly prioritize the first case.

## Implementation Changes

### 1. Assemble candidate prioritization

Replace the current single-lane pending-header query with a two-lane selection strategy.

Status:

- completed on 2026-04-21
- implemented as a deterministic two-lane merged batch in `ListUnassembledArticleHeaders(limit)`
- validated with repository ordering coverage plus assemble/release-related Go test passes

Lane A: progress-improving headers

- source rows:
  - `article_headers.assembled_at IS NULL`
  - joined to `article_header_ingest_payloads`
- candidate rule:
  - structured file identity exists:
    - `subject_file_name <> ''` or
    - `yenc_total_parts > 1`
  - and there is an existing binary in the same provider/newsgroup that matches the normalized file identity
- order:
  - newest-first by `article_headers.id DESC`
- batch budget:
  - default `70%` of assemble batch

Lane B: fresh pending headers

- source rows:
  - remaining `assembled_at IS NULL` headers not selected by Lane A
- order:
  - newest-first by `article_headers.id DESC`
- batch budget:
  - default `30%` of assemble batch

Repository changes:

- replace `ListUnassembledArticleHeaders(limit)` with a prioritized variant that returns a single merged batch with lane metadata or at least deterministic lane ordering
- use structured ingest metadata already persisted on `article_header_ingest_payloads`:
  - `subject_file_name`
  - `subject_file_index`
  - `subject_file_total`
  - `yenc_part_number`
  - `yenc_total_parts`
  - `yenc_file_size`
- if needed after measurement, add a supporting index for pending structured-name lookups

Expected behavior:

- assemble should spend more of each batch improving known binaries
- complete binaries per hour should increase
- release should receive better requeue signals from real binary progress

### 2. Assemble-side yEnc recovery cost control

Keep yEnc fallback, but narrow when it runs.

Status:

- completed on 2026-04-21
- yEnc fetch recovery now skips stable multipart headers that already have structured file identity and an existing-binary match
- assemble stage logging now emits `assemble_recovery_attempts`, `assemble_recovery_successes`, `assemble_recovery_noops`, and `assemble_recovery_fetch_failures`
- validated with assemble service coverage for both skip and opaque multipart recovery paths plus assemble/release-related Go test passes

Do not fetch article bodies for yEnc recovery when:

- structured ingest metadata already gives a stable file identity, and
- the current match already produces multipart grouping, and
- the header matches an existing binary by structured identity

Only attempt yEnc fetch recovery when:

- the current match is low-confidence or opaque, and
- structured metadata is missing or insufficient, and
- the header is likely multipart or otherwise likely to materially improve grouping if recovered

Instrumentation to add:

- `assemble_recovery_attempts`
- `assemble_recovery_successes`
- `assemble_recovery_noops`
- `assemble_recovery_fetch_failures`

Expected behavior:

- lower average assemble cost per header
- less NNTP fetch work inside assemble
- no regression in multipart obfuscated grouping

### 3. Release candidate prioritization

Keep the dirty-family queue, but make selection explicitly favor formable families.

Status:

- completed on 2026-04-21
- release candidate ordering now prefers formable families by `complete_binary_count DESC`, then expected-file-count evidence, then queue age
- zero-binary stale-cleanup families remain eligible while fragment-only families stay out of the normal release candidate batch
- validated with repository coverage for complete-vs-fragment priority, expected-file-count preference, zero-binary stale-cleanup eligibility, and release-related Go test passes

Candidate window behavior:

- scan a larger queue window than the final batch size
- compute per-family stats inside the candidate query:
  - `binary_count`
  - `complete_binary_count`
  - `expected_file_count` presence
  - `updated_at`

Selection rules:

- always include families with `binary_count = 0` so stale-release cleanup still works
- prefer families with `complete_binary_count > 0`
- rank by:
  1. `complete_binary_count DESC`
  2. expected file-count evidence present before missing
  3. `updated_at ASC`

Expected behavior:

- `release --once` should spend most of its time on formable work
- the front of the queue should stop being dominated by fragment-only families

### 4. Fragment-family cooldown and requeue behavior

This is the key release behavior change for this phase.

Status:

- completed on 2026-04-21
- release now cools down dirty families that still have binaries but zero complete binaries by deleting any stale releases and acking the dirty row without attempting normal persistence
- fragment-only families remain visible at lower queue priority so normal release passes can cool them down after actionable work
- release stage logging now reports `cooled_down_fragment_only_families`, and binary progress refresh continues to requeue families through existing dirty-family insertion paths
- validated with release service coverage for cooldown ack behavior, repository coverage for fragment-only tail selection and post-ack requeue, and release-related Go test passes

Cooldown rule:

- if a dirty family resolves to:
  - `binary_count > 0`
  - `complete_binary_count = 0`
- then release should treat it as non-actionable for now
- do not attempt normal release persistence for that family
- acknowledge/remove its dirty-family queue row during that release pass

Why this is safe:

- release is not the system of record for binary completeness
- binary progress is already tracked on `binaries`
- future `UpsertBinary(...)` and `RefreshBinaryStats(...)` calls already requeue family work when progress changes

Required implementation behavior:

- cooldown applies only to fragment-only families
- do not cooldown families with `binary_count = 0`; those still need stale-release cleanup behavior
- do not cooldown partially actionable families that already have at least one complete binary
- cooldown should be reflected in release-stage logs and counters

Requeue trigger:

- any binary update that changes a family’s effective release readiness should reinsert the dirty-family row
- existing binary upsert/stats refresh code should remain the source of truth for requeueing

Expected behavior:

- dirty-family backlog more accurately reflects actionable release work
- repeated `formed=0` passes caused by known fragment-only families should drop sharply

### 5. Runtime observability and operator diagnosis

Add clearer stage-level diagnostics.

Status:

- completed on 2026-04-21
- assemble stage logging now reports `pending_headers`, `lane_a_selected`, `lane_b_selected`, `processed_headers`, `binaries_refreshed`, and yEnc recovery counters
- release stage logging now reports `candidate_families`, `formed`, `cooled_down_fragment_only_families`, `stale_cleanup_only_families`, `skipped_fragments`, `skipped_confidence`, and `skipped_completion`
- `docs/INDEXER_TEST_QUERIES.md` now includes reusable operator queries for pending assembly summary, why a header is still unassembled, why a family is still dirty but unformed, and whether a family is fragment-only or release-actionable
- validated with assemble/release/store/runtime Go test passes after the observability changes

Assemble logging/counters:

- pending headers count
- lane-A vs lane-B selected counts
- processed headers
- refreshed binaries
- yEnc recovery attempts/success/failure counts

Release logging/counters:

- candidate families inspected
- formed releases
- skipped for confidence
- skipped for completion threshold
- cooled-down fragment-only families
- stale-cleanup-only families

Add reusable queries to docs for:

- why a header is still unassembled
- why a family is still dirty but unformed
- whether a family is fragment-only or actually release-actionable

## Validation Targets

### Assemble

- higher completed-binary-per-hour rate than the current baseline
- better refreshed-binaries-to-processed-headers ratio
- reduced rate of fragment-only families entering the front of the release queue

### Release

- `release --once` should more often produce formed releases when formable families exist
- dirty-family queue size should better track actionable work, not fragment churn
- repeated `formed=0` passes should drop significantly once cooldown is active

### Safety

- no return of the old fake standalone binary regression
- no release created from a family with zero complete binaries
- no regression in obfuscated multipart grouping quality

## Runtime Validation Findings

Live validation on 2026-04-21 found that correctness changes 1 through 5 were not yet sufficient to satisfy the exit criteria in production-like conditions.

Observed behavior during controlled `--once` validation:

- `pending_headers` remained at `1,605,290` during the observation window
- `dirty_families` remained at `93,824`
- `complete_binaries` remained at `2,824`
- `incomplete_binaries` remained at `57,047`
- `releases` remained at `132`
- current-code `assemble --once` and `release --once` runs did not complete promptly enough to demonstrate healthy churn

Query investigation results:

- the main bottleneck is repository query cost before the service loops do meaningful work
- assemble is currently dominated by `ListUnassembledArticleHeaders(limit)`
- release is currently dominated by `ListReleaseCandidates(limit)`
- this is primarily a query-shape and supporting-index problem, not yet evidence of NNTP fetch cost or release clustering cost dominating the normal pass

### Follow-on Query Fixes

This section was the active follow-on work needed after the first live validation pass. The query and runtime fixes below were completed on 2026-04-21 and should be treated as the baseline for future validation work.

#### 6. Assemble selector query support and shape cleanup

Status:

- completed on 2026-04-21
- added schema support in migration `005_indexer_refinement_query_support.up.sql`
- lane A now starts from a bounded `recent_pending` window on `article_headers` and does point lookups into payload and binary identity instead of broad merge/hash joins
- live validation with `assemble.batch_size=250` completed repeatedly in about `3.3s` to `3.4s` per pass instead of hanging in candidate selection
- two consecutive live passes moved `pending_headers` from `1,605,290` to `1,604,790`
- those same passes selected `lane_a_selected=175` and `lane_b_selected=75` each time and refreshed `3` then `4` binaries respectively

Required changes:

- add a supporting index for structured ingest name lookup on `article_header_ingest_payloads`
- add a normalized file-identity lookup index on `binaries`
- reshape lane A so it starts from `article_headers` pending rows and does point lookups into payload and binary identity instead of broad merge/hash joins

Expected behavior:

- assemble candidate selection should return promptly for small and medium batches
- lane A should become cheap enough to validate with repeated live `--once` runs
- operator `pending_headers` and lane selection counters should start moving during validation windows

#### 7. Release selector query split for index usage

Status:

- completed on 2026-04-21
- split `release_family` and `base_stem` lookup paths into separate lateral aggregate branches so Postgres could use the family-specific indexes
- added `idx_binaries_base_stem_family_lookup` and aligned the query predicate to the partial-index condition
- store connections now open through parsed `pgx` config with PostgreSQL `jit=off` for this workload because JIT startup cost was dominating one-shot release passes after the query rewrite
- live validation showed that the old `queue_window = batch_size * 10` sample was still too narrow: the oldest `2,000` dirty families were all fragment-only, while the oldest `20,000` contained actionable complete families
- the selector now samples a broader capped window before ranking so actionable families can be surfaced ahead of fragment-only cooldown work
- post-fix live release passes completed successfully instead of hanging:
  - first two passes cooled down `200` fragment-only families each and reduced `dirty_families` from `93,824` to `93,424`
  - a later pass with the broader queue window reported `candidate_families=200 formed=8 cooled_down_fragment_only_families=191 skipped_confidence=2`
  - during that pass `releases` increased from `132` to `137` and `dirty_families` fell further to `93,224`

Required changes:

- split release candidate stats into separate `release_family` and `base_stem` branches
- let the `release_family` branch use `idx_binaries_release_family_key`
- add a simpler `base_stem` lookup index for the `expected_file_count > 1` branch
- avoid the single `OR` join path in the release candidate query

Expected behavior:

- `release --once` should finish promptly for small and medium batches
- dirty-family queue sampling should become cheap enough for repeated validation runs
- the queue should start demonstrating actionable churn instead of long-running selector time

### Current Post-Fix Interpretation

- the original live hang was primarily a repository SQL and PostgreSQL execution-planning problem, not evidence that NNTP yEnc recovery or release clustering were the main bottlenecks
- assemble is now visibly churning and prioritizing existing-binary improvement work
- release is now visibly processing queue work again, including both fragment-family cooldown and actionable release formation
- the front of the dirty queue is still heavily fragment-only, so more live soak is still needed before the overall refinement loop can be signed off against the full exit criteria

## Live Sign-Off Update On 2026-04-22

Follow-on validation recorded in `docs/active/INDEXER_BACKLOG_BURNDOWN_PERFORMANCE_PLAN.md` now satisfies the live refinement sign-off requirement for this plan.

Sign-off evidence:

- repeated live `assemble --once` passes completed in about `21s` to `25s` at `batch_size=2500`
- those assemble passes consistently kept lane A active:
  - `lane_a_selected=410`
  - `lane_a_selected=399`
  - `lane_a_selected=399`
- repeated live `release --once` passes completed in about `84s` to `94s`
- those release passes consistently formed releases from actionable families:
  - `formed=137`
  - `formed=136`
  - `formed=137`
- fragment-only cooldown no longer dominated the release loop:
  - `cooled_down_fragment_only_families=43`
  - `cooled_down_fragment_only_families=42`
  - `cooled_down_fragment_only_families=42`
- bounded validation-window backlog movement was real:
  - pending headers moved from `640932` to `633432`
  - complete binaries moved from `7386` to `7418`
  - dirty families drained back to `0`
- no live evidence suggested a return of the prior junk standalone-binary regression

Current conclusion:

- the refinement loop goals for assemble prioritization, release queue quality, and runtime viability are now met
- remaining near-complete release follow-up should be treated as a narrower inspection and catalog-quality task, not as a blocker on this refinement loop
- this document can now be treated as signed off for its Phase 3 gate criteria once the active-doc transition is performed

#### 8. Assemble completion-first prioritization for partial payload binaries

Status:

- completed by the 2026-04-22 backlog burn-down validation pass
- live data shows the current partial-binary backlog is mostly named main-payload work, not anonymous `.bin` junk
- snapshot during validation:
  - about `57k` partial binaries overall
  - about `56.9k` marked `is_main_payload = true`
  - essentially `0` active `.bin`-named partials in the current backlog sample
  - releases currently include a small but real pool in the `70%` to `99%` completion range

Problem:

- the current lane-A assemble prioritization already prefers headers that match an existing binary, but it does not explicitly prefer binaries that are close to completion
- this leaves near-finished payload binaries competing with very low-progress matches, which slows down release readiness and keeps partially-complete releases hot longer than necessary

Refinement direction:

- within lane A, prefer matched binaries with:
  - `observed_parts < total_parts`
  - main payloads before auxiliary files
  - higher completion ratio before lower completion ratio
- continue allowing fresh work through lane B so new releases do not starve

Expected behavior:

- binaries already at `70%` to `95%` completion should finish sooner when matching headers are present
- release-actionable families should surface faster
- partially-complete releases should either reach `100%` sooner or cool down cleanly instead of lingering mid-completion

## Test Plan

- repository test for prioritized assemble selection:
  - headers matching existing binaries are selected before fresh unrelated headers
- assemble service test:
  - yEnc recovery is skipped when structured metadata is already sufficient
- assemble service test:
  - yEnc recovery still runs for opaque multipart headers lacking structured identity
- repository test for release candidate ordering:
  - families with complete binaries outrank fragment-only families
- repository test for fragment-family cooldown:
  - fragment-only family is acknowledged/removed from `release_stage_dirty_families`
- repository test for requeue behavior:
  - later `UpsertBinary` or `RefreshBinaryStats` progress requeues the cooled-down family
- live validation:
  - compare completed binaries/hour before and after
  - compare `formed` rate across repeated `release --once` runs before and after
  - verify dirty-family backlog trends down in actionable terms, not just raw count

## Exit Criteria Before Phase 3

This refinement loop is complete when:

- assemble backlog is trending down at a healthy sustained rate
- assemble is demonstrably prioritizing work that improves existing binaries
- release queue passes routinely form releases when sufficient complete data exists
- fragment-only queue churn no longer dominates normal release runs
- no junk-release regression appears during live validation

At that point:

- move this refinement plan to `docs/archive/completed/indexer/`
- keep `INDEXER_API_AND_WEB_UI_EXPANSION_PLAN.md` as the next active feature-phase document
