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

Updated finding on `2026-05-06`:

- a pathological `20,000`-header assemble run took about `33 minutes` with `12,924` yEnc recovery attempts and `12,924` fetch failures
- recent fast `20,000`-header runs stayed around `10` to `15` seconds with only a handful of recovery attempts
- this confirmed that inline yEnc recovery is not a normal-path requirement and needs explicit hot-path guardrails

Updated recommendation:

- keep inline yEnc recovery only for last-resort opaque headers where scrape/XOVER did not already expose a subject-derived file name
- cap yEnc recovery attempts per assemble batch so one pathological slice of backlog cannot monopolize the stage
- retain the option to build a deferred repair pass later for deeper recovery on the remaining opaque failures

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

- [x] confirm all current uses of `pending_headers`
- [x] make pending-header counting optional or move it out of `RunOnceWithMetrics`
- [x] keep assemble metrics valid when the count is skipped
- [x] add a separate read path for UI/operator backlog visibility if needed

Acceptance criteria:

- assemble can run without blocking on a full pending count
- pipeline correctness is unchanged
- UI or operator metrics still have a supported way to query backlog size

Workstream 2 sign-off:

- complete on `2026-05-05`
- current source usage of `pending_headers` was confirmed to be stage metrics/logging plus operator SQL, not a pipeline correctness dependency
- assemble no longer performs the blocking `COUNT(*)` during `RunOnceWithMetrics`
- the latest live assemble run persisted metrics without `pending_headers` or `pending_count_duration_ms`, while operator/UI backlog visibility remains available through direct query paths such as [INDEXER_TEST_QUERIES.md](/mnt/home-datallboy/Projects/github.com/datallboy/gonzb/docs/INDEXER_TEST_QUERIES.md:122)
- on `2026-05-06`, admin dashboard support was added for an explicit manual backlog refresh via `/api/v1/admin/indexer/overview/backlog`, so operators can pull `unassembled_headers` on demand without reintroducing the count into assemble stage runs
- on `2026-05-06`, that backlog path was refined into a persisted dashboard snapshot backed by `indexer_dashboard_stats`; dashboard loads now read the cached value instantly, and explicit refresh writes a new snapshot only when requested

## Workstream 3. Release Article Lineage Consolidation

Goal:

- remove redundant `release_file_articles` storage if `binary_parts` is the true source of truth

Current code path:

- release builds article refs from `binary_parts`
- release writes those refs again into `release_file_articles`
- NZB and inspect read back from `release_file_articles`

Tasks:

- [x] verify all current `release_file_articles` read paths
- [x] switch `ListCatalogReleaseFileArticles` to derive from `release_files -> binary_parts -> article_headers`
- [x] switch public/admin file article counts away from `release_file_articles`
- [x] stop writing `release_file_articles` in `ReplaceReleaseFiles`
- [x] add invariant coverage for:
  - release file to binary uniqueness
  - article ordering by `part_number`
  - NZB equivalence before and after the change
- [x] add a cleanup migration later to drop `release_file_articles` and its indexes after validation

Acceptance criteria:

- no runtime path depends on `release_file_articles`
- release writes no longer copy article refs out of `binary_parts`
- NZB and inspect behavior stays correct

Workstream 3 sign-off:

- runtime consolidation complete on `2026-05-05`
- current runtime read paths were switched from `release_file_articles` to `binary_parts`
- release formation no longer reads batch article refs just to copy them into `release_file_articles`
- live validation showed newly written `release_files` with `0` `release_file_articles` rows while matching `binary_parts` counts remained present and ordered
- the destructive schema cleanup was completed on `2026-05-05` with migration `011_drop_release_file_articles.up.sql` after confirming runtime aggregator, resolver, inspect, and downloader paths all derive article refs from `binary_parts`

## Workstream 4. Inspection Claim And Write Batching

Goal:

- remove row-at-a-time claim/update work from inspection hot paths

Tasks:

- [x] batch binary inspection claims instead of inserting/updating one candidate at a time
- [x] review archive/media/par2/nfo/password persistence helpers for row-at-a-time writes that can become set-based
- [x] keep stage claim correctness under concurrency

Acceptance criteria:

- inspection claim writes are batched
- no stage behavior regresses under concurrency

Workstream 4 sign-off:

- complete on `2026-05-05`
- binary inspection claiming now uses one set-based `INSERT ... ON CONFLICT` for the reserved candidate batch instead of one write per candidate
- focused store and inspect tests passed, and a live `indexer inspect archive --once` run completed normally with the expected reservation metrics
- the follow-up review found additional per-binary artifact replace helpers that could be batched later, but they are downstream persistence helpers rather than the shared claim hot path that every inspection worker contends on

## Workstream 5. Scrape Insert Batching

Goal:

- reduce row-by-row scrape insert overhead so backlog ingest headroom stays above assemble throughput

Tasks:

- [x] review `InsertArticleHeaders` for set-based insert opportunities on:
  - `article_headers`
  - `article_header_ingest_payloads`
  - poster normalization
- [x] preserve idempotency and uniqueness semantics
- [x] measure scrape throughput before and after

Acceptance criteria:

- scrape remains correct under duplicate/overlapping overview fetches
- insert overhead is reduced without sacrificing clarity or recoverability

Workstream 5 sign-off:

- complete on `2026-05-05`
- `InsertArticleHeaders` now preprocesses valid overview rows once, then writes posters, `article_headers`, and `article_header_ingest_payloads` in chunked set-based batches inside the same transaction
- duplicate or overlapping overview rows still resolve onto the same `article_headers` row, and payload semantics remain last-write-wins for a repeated header within the same scrape batch
- focused Go tests passed, and direct PostgreSQL integration validation passed via `TestInsertArticleHeadersBatchDedupesDuplicateRowsLastPayloadWins`
- a live `indexer scrape --once` stage timing comparison was not available because the existing `scrape_latest` stage state is currently disabled in the local runtime, but the new path was validated against the live Postgres store and removes the old per-header insert/update loop that previously executed one poster write, one header write, and one payload write per row

## Workstream 6. Assemble Batch Size Tuning

Goal:

- increase backlog burn-down throughput by amortizing assemble fixed per-run costs across larger claimed batches

Tasks:

- [x] measure current live `assemble --once` behavior at the baseline configured batch size
- [x] test larger assemble batch sizes starting with `25,000`
- [x] test a larger follow-up size such as `50,000` if memory, lock duration, and transaction times remain healthy
- [x] compare:
  - `total_duration_ms`
  - `headers_per_second`
  - `candidate_selection_duration_ms`
  - `header_match_duration_ms`
  - `binary_part_upsert_duration_ms`
  - `binary_refresh_duration_ms`
- [x] keep the best-performing size only if throughput improves without unacceptable memory or contention side effects

Acceptance criteria:

- assemble throughput improves measurably on live runs or the current batch size is explicitly confirmed as near-optimal
- the chosen batch size does not introduce stability regressions or materially worse lock/claim behavior

Workstream 6 sign-off:

- complete on `2026-05-06`
- live manual assemble stage runs were measured at `20,000`, `25,000`, and `50,000` headers with the current `4` workers and the new yEnc recovery guardrails active
- observed results:
  - `20,000`: about `10.11s`, about `1,978` headers/sec, `candidate_selection_duration_ms=7,715`, `header_match_duration_ms=5,609`
  - `25,000`: about `11.64s`, about `2,148` headers/sec, `candidate_selection_duration_ms=8,551`, `header_match_duration_ms=6,910`
  - `50,000`: about `17.55s`, about `2,849` headers/sec, `candidate_selection_duration_ms=11,881`, `header_match_duration_ms=12,607`
- none of the measured runs showed yEnc recovery churn, lease instability, or claim/lock regressions during the test window
- the local persisted runtime setting was updated to keep `assemble.batch_size=50000` because it delivered the best measured throughput while remaining well below the current `5 minute` claim lease in this environment

## Workstream 7. Inspection Artifact Replace Batching

Goal:

- remove remaining row-at-a-time delete-and-reinsert loops from per-binary inspection persistence helpers

Scope:

- `ReplaceBinaryInspectionArtifacts`
- `ReplaceBinaryArchiveEntries`
- `ReplaceBinaryMediaStreams`
- `ReplaceBinaryTextEvidence`
- `ReplaceBinaryPAR2Sets`

Tasks:

- [x] batch inserts after the existing per-binary delete step for each helper
- [x] preserve current JSON sanitization and normalization behavior
- [x] keep replace semantics identical for empty and non-empty row sets
- [x] validate archive, media, par2, nfo, and password inspection flows after the change

Acceptance criteria:

- no helper performs one insert statement per persisted row in normal operation
- inspection outputs remain byte-for-byte or field-for-field equivalent for the same probe results

Workstream 7 sign-off:

- complete on `2026-05-06`
- `ReplaceBinaryInspectionArtifacts`, `ReplaceBinaryArchiveEntries`, `ReplaceBinaryMediaStreams`, `ReplaceBinaryTextEvidence`, and `ReplaceBinaryPAR2Sets` now keep their existing delete-then-replace semantics but write new rows through chunked multi-row inserts instead of one statement per row
- the existing JSON sanitization and token normalization paths were preserved because the batched write path still marshals through the same `sanitizeJSONMap` and `sanitizeStringSlice` helpers before insert
- a new pgindex integration test covers non-empty persistence plus empty replace clearing across all five helpers on a real Postgres-backed binary
- focused tests passed for pgindex plus archive/media/par2/nfo/password inspect packages, and a live `go run ./cmd/gonzb --config config.yaml indexer inspect --once` sanity run completed successfully

## Workstream 8. Enrichment Match And Entry Batching

Goal:

- reduce row-at-a-time write amplification in release enrichment persistence

Scope:

- `ReplaceReleaseTMDBMatches`
- `ReplaceReleasePredbMatches`
- `UpsertPredbEntries`
- `ReplaceReleaseTVDBMatches`

Tasks:

- [ ] batch replace inserts for TMDB, predb, and TVDB release match rows
- [ ] batch predb entry upserts where possible while preserving normalized-title conflict semantics
- [ ] preserve chosen-match flags, payload JSON, and fallback normalization behavior
- [ ] validate enrichment stages against the live dev database after the change

Acceptance criteria:

- enrichment replace paths no longer issue one insert per row in the common case
- release enrichment state remains correct for duplicate and overlapping match sets

## Workstream 9. Release File Insert Batching

Goal:

- finish reducing release-stage write amplification by batching `release_files` replacement inserts

Tasks:

- [ ] batch `ReplaceReleaseFiles` inserts after the current delete and cross-release cleanup steps
- [ ] preserve `release_files.binary_id` uniqueness and current file ordering semantics
- [ ] validate release formation and NZB generation after the change

Acceptance criteria:

- `ReplaceReleaseFiles` no longer inserts one row at a time for normal release batches
- release and NZB behavior remains unchanged for the same candidate set

## Workstream 10. Assemble yEnc Recovery Guardrails

Goal:

- prevent pathological opaque-header slices from turning inline yEnc recovery into the dominant assemble cost

Tasks:

- [x] confirm whether scrape/XOVER already provides the structured metadata needed for normal assemble matching
- [x] narrow inline yEnc recovery so it does not run when the subject already exposed a structured file name
- [x] add a per-batch cap on yEnc recovery attempts
- [x] persist `430` / not-found yEnc recovery misses with retry backoff so the same missing article is not re-fetched every assemble pass
- [x] re-test live assemble at `20,000` batch size after the guardrails

Acceptance criteria:

- inline yEnc recovery remains available for true opaque last-resort cases
- pathological batches cannot spend thousands of body fetch attempts in one assemble run
- recent live `20,000`-header assemble runs remain in the fast range after the change

Workstream 10 sign-off:

- complete on `2026-05-06`
- confirmed that scrape/XOVER plus persisted structured fields already cover the normal hot path, while inline yEnc recovery is only needed for true opaque last-resort cases
- assemble now skips inline recovery when a subject-derived file name is already present, caps yEnc recovery attempts per batch, and persists `430` / not-found misses onto the article ingest payload with escalating retry backoff
- focused assemble and store tests cover subject-name skips, per-batch cap behavior, persisted backoff skips, and not-found backoff recording
- recent post-restart live `20,000`-header assemble runs returned to the fast range with `0` recovery attempts in the normal case, while the earlier pathological `33 minute` run was explained by `12,924` failed recovery fetches

## Execution Order

1. Workstream 1: assemble hot-path payload reduction
2. Workstream 2: pending count removal
3. Workstream 3: release article lineage consolidation
4. Workstream 4: inspection claim and write batching
5. Workstream 5: scrape insert batching
6. Workstream 6: assemble batch size tuning
7. Workstream 7: inspection artifact replace batching
8. Workstream 8: enrichment match and entry batching
9. Workstream 9: release file insert batching

## Validation

For each workstream:

- run focused Go tests first
- run live `assemble --once` or `release --once` where relevant
- record before/after stage metrics from `indexer_stage_runs`
- prefer measured runtime changes over speculative schema cleanup
