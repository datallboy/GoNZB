# Indexer Backlog Burn-Down And Schema Simplification Plan

Snapshot date: 2026-05-05

This is the active execution plan for the next backlog-burn-down pass after the assemble selector rewrite.

The immediate goal is to keep the faster assemble path fed while removing the next sources of per-batch cost, schema duplication, and write amplification.

## Why This Sprint Exists

Recent live validation on the local `gonzb-postgres` dev database showed:

- assemble candidate selection dropped from about `336s` to about `7.3s` for a `10,000` header batch after the selector rewrite
- total assemble runtime dropped from about `339s` to about `10.9s`
- release runs are now relatively healthy at about `3s` to `10s` on non-empty batches

That means the dominant next work is no longer the old selector query shape. The next work is:

- remove hot-path payload work that assemble no longer needs
- remove non-essential blocking metrics queries from the stage hot path
- collapse redundant release article lineage storage if invariants hold
- batch remaining row-at-a-time write paths in inspect and scrape

## Current Decisions

### Decision 1. Keep `article_headers` and `article_header_ingest_payloads` separate for now

Reason:

- `article_headers` is the hot queue, claim, lineage, and serving table
- `article_header_ingest_payloads` holds larger transient scrape payload fields
- the successful assemble selector rewrite depended on selecting from the narrow `article_headers` table first and hydrating payload data later
- blindly merging the tables would widen the hot table and likely regress pending-header scans

What to do instead:

- keep the split
- tighten which payload fields are required in hot paths
- shorten payload retention
- move more operations to claim/select ids first, then hydrate on demand

### Decision 2. Keep yEnc recovery, but make it more deliberate

Current behavior:

- assemble calls `matcher.Match(...)`
- if the subject is opaque or the match still looks weak, assemble fetches the article body header from NNTP
- it reads the yEnc header and retries matching with the recovered file name and part metadata

Why it exists:

- it rescues obfuscated uploads where subject-based matching alone would leave `.bin`-style names or low-confidence one-part matches

Current recommendation:

- keep it because it is functionally useful
- do not let it dominate normal throughput
- narrow the trigger conditions if measurements show it is too eager
- if needed later, consider a deferred repair/recovery pass for the worst opaque cases instead of doing everything inline in assemble

### Decision 3. Remove non-essential metrics work from the assemble hot path

Current behavior:

- assemble performs `COUNT(*)` on pending unassembled headers every run
- that count is used for stage metrics and operator visibility, not for correctness of the pipeline itself

Current recommendation:

- stop blocking assemble on that count
- move backlog counting to:
  - a separate background refresh path
  - or an on-demand UI/API query
  - or an optional runtime flag when exact stage metrics are needed

## Workstream 1. Assemble Hot Path Payload Reduction

Goal:

- remove `raw_overview_json` dependence from the normal assemble/match hot path

Scope:

- assemble candidate hydration
- matcher input construction
- yEnc recovery rematch input

Tasks:

- [x] verify which matcher behaviors still rely on `candidate.RawOverview`
- [x] stop decoding full `raw_overview_json` during assemble candidate hydration when structured columns already provide the needed values
- [x] remove any remaining hot-path reads of `raw_overview_json` from assemble selector hydration if they are not needed for matching
- [x] keep enough structured fields for:
  - file name
  - part number
  - total parts
  - file count
  - size hints
- [x] re-measure `header_match_duration_ms` after the change

Acceptance criteria:

- assemble does not JSON-decode raw overview payloads for the normal hot path
- matching behavior remains correct for structured multipart subjects
- recent live runs show lower `header_match_duration_ms` or lower total runtime without selector regression

Workstream 1 sign-off:

- complete on `2026-05-05`
- matcher raw-overview dependence was verified to be limited to fields that can be synthesized from structured columns plus `bytes`
- assemble candidate hydration no longer reads or decodes `raw_overview_json` in the normal hot path
- focused Go tests passed and live `assemble --once` validation remained healthy with recent `header_match_duration_ms` around `3.2s` to `3.6s` and total runtime around `7.7s` to `8.4s` on `10,000` header runs

## Workstream 2. Assemble Pending Count Removal

Goal:

- remove the blocking `COUNT(*)` query from the assemble stage hot path

Tasks:

- [ ] confirm all current uses of `pending_headers`
- [ ] make pending-header counting optional or move it out of `RunOnceWithMetrics`
- [ ] keep assemble metrics valid when the count is skipped
- [ ] add a separate read path for UI/operator backlog visibility if needed

Acceptance criteria:

- assemble can run without blocking on a full pending count
- pipeline correctness is unchanged
- UI or operator metrics still have a supported way to query backlog size

## Workstream 3. Release Article Lineage Consolidation

Goal:

- remove redundant `release_file_articles` storage if `binary_parts` is the true source of truth

Current code path:

- release builds article refs from `binary_parts`
- release writes those refs again into `release_file_articles`
- NZB and inspect read back from `release_file_articles`

Tasks:

- [ ] verify all current `release_file_articles` read paths
- [ ] switch `ListCatalogReleaseFileArticles` to derive from `release_files -> binary_parts -> article_headers`
- [ ] switch public/admin file article counts away from `release_file_articles`
- [ ] stop writing `release_file_articles` in `ReplaceReleaseFiles`
- [ ] add invariant coverage for:
  - release file to binary uniqueness
  - article ordering by `part_number`
  - NZB equivalence before and after the change
- [ ] add a cleanup migration later to drop `release_file_articles` and its indexes after validation

Acceptance criteria:

- no runtime path depends on `release_file_articles`
- release writes no longer copy article refs out of `binary_parts`
- NZB and inspect behavior stays correct

## Workstream 4. Inspection Claim And Write Batching

Goal:

- remove row-at-a-time claim/update work from inspection hot paths

Tasks:

- [ ] batch binary inspection claims instead of inserting/updating one candidate at a time
- [ ] review archive/media/par2/nfo/password persistence helpers for row-at-a-time writes that can become set-based
- [ ] keep stage claim correctness under concurrency

Acceptance criteria:

- inspection claim writes are batched
- no stage behavior regresses under concurrency

## Workstream 5. Scrape Insert Batching

Goal:

- reduce row-by-row scrape insert overhead so backlog ingest headroom stays above assemble throughput

Tasks:

- [ ] review `InsertArticleHeaders` for set-based insert opportunities on:
  - `article_headers`
  - `article_header_ingest_payloads`
  - poster normalization
- [ ] preserve idempotency and uniqueness semantics
- [ ] measure scrape throughput before and after

Acceptance criteria:

- scrape remains correct under duplicate/overlapping overview fetches
- insert overhead is reduced without sacrificing clarity or recoverability

## Execution Order

1. Workstream 1: assemble hot-path payload reduction
2. Workstream 2: pending count removal
3. Workstream 3: release article lineage consolidation
4. Workstream 4: inspection claim and write batching
5. Workstream 5: scrape insert batching

## Validation

For each workstream:

- run focused Go tests first
- run live `assemble --once` or `release --once` where relevant
- record before/after stage metrics from `indexer_stage_runs`
- prefer measured runtime changes over speculative schema cleanup
