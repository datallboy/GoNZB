# Indexer Assemble And Release Refinement Plan

Snapshot date: 2026-04-21

This is the current active execution plan for the post-stabilization refinement loop.

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
