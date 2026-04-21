# Indexer Assemble And Release Refinement Plan

Snapshot date: 2026-04-21

This is the current active execution plan for the post-stabilization refinement loop.

The storage, schema, and runtime-stability pass is mostly complete. The current bottleneck has shifted to throughput and prioritization:

- `article_headers` backlog is still large
- assemble is working, but it is processing a very large pending set
- release is spending too much time on fragment-heavy dirty families before enough complete binaries are available

## Current Measured State

- `1,605,290` unassembled headers
- `240,731` headers assembled in the last hour
- `1,250` binaries updated in the last hour
- about `96k` dirty release families
- the front of the release queue was dominated by fragment-only families with `0` complete binaries
- the old bad `1/1 + expected_file_count=0` opaque binary pattern is no longer showing up in the live DB

Interpretation:

- assemble quality is much better than before
- release quality guardrails are doing their job
- the current problem is throughput and work ordering, not the old junk-release regression

## Goals

- clear the assemble backlog faster without regressing grouping quality
- prioritize headers that improve already-started binaries before spending equal time on brand-new fragment families
- reduce `release --once` passes that end with `formed=0`
- make release work proportional to formable families, not just oldest queued families
- leave the system ready for Phase 3 API/UI work once the backlog behavior is stable

## Non-Goals

- reopening the header payload split or schema-baseline decisions unless a new blocker appears
- weakening release quality thresholds just to force more rows into `releases`
- broad API/UI work before assemble/release throughput is under control

## Workstreams

### 1. Assemble Candidate Prioritization

Current behavior:

- assemble reads pending headers newest-first by `article_headers.id DESC`
- this is safe, but it does not necessarily prioritize the headers most likely to complete existing binaries

Refinement direction:

- prioritize pending headers that belong to already-started binaries or known multipart families
- prefer work that increases `observed_parts` for existing binaries before purely brand-new fragment families
- consider a two-lane strategy:
  - lane 1: headers likely to improve existing binaries
  - lane 2: fresh headers with no existing binary yet

Validation target:

- more complete binaries per hour
- better ratio of refreshed binaries to processed headers
- fewer fragment-only families accumulating at the head of the release queue

### 2. Matching And yEnc Recovery Cost Control

Current behavior:

- low-confidence opaque headers may trigger yEnc-header recovery
- this is important for correctness, but it adds fetch work during assemble

Refinement direction:

- keep yEnc recovery for cases that materially improve grouping
- avoid repeated recovery work for headers/binaries where prior recovery already established stable identity
- prefer structured ingest metadata first, then fetch-based recovery only when needed

Validation target:

- lower average recovery cost per assembled header
- no regression in multipart obfuscated grouping quality

### 3. Release Candidate Prioritization

Current behavior:

- release now filters the dirty queue toward families that are actually formable:
  - no binaries left, so stale cleanup can occur
  - or at least one complete binary exists

Refinement direction:

- continue biasing release toward families with meaningful completion progress
- if needed, rank queue work by a stronger “formability” score:
  - complete binary count
  - expected file count presence
  - family quality / confidence
  - recency of actual binary improvement

Validation target:

- `release --once` should more often produce formed releases instead of mostly fragment skips
- dirty-family backlog should drain in a way that improves visible release output, not just queue churn

### 4. Runtime Observability And Repair

Current behavior:

- explicit maintenance and runtime-repair commands now exist
- this prevents stale leases from blocking stages indefinitely

Refinement direction:

- add quick operator queries or logs that explain:
  - why a header is still unassembled
  - why a dirty family is still unformed
  - whether a release queue pass skipped because of fragments, confidence, or completion

Validation target:

- faster operator diagnosis without ad hoc DB spelunking

### 5. Exit Criteria Before Phase 3

This refinement loop is complete when:

- assemble backlog is trending down at a healthy sustained rate
- assemble is demonstrably prioritizing work that improves existing binaries
- release queue passes routinely form releases when sufficient complete data exists
- fragment-only queue churn is no longer dominating normal release runs
- no new junk-release regression appears in live validation

At that point:

- move this refinement plan to `docs/archive/completed/indexer/`
- keep `INDEXER_API_AND_WEB_UI_EXPANSION_PLAN.md` as the next active feature-phase document
