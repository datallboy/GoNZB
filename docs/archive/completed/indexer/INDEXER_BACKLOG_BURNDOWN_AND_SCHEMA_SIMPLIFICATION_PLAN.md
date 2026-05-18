# Indexer Backlog Burn-Down And Schema Simplification Plan

Snapshot date: 2026-05-05

This was the execution plan for the backlog-burn-down pass after the assemble selector rewrite.

The immediate goal was to keep the faster assemble path fed while removing the next sources of per-batch cost, schema duplication, and write amplification.

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
- the latest live assemble run persisted metrics without `pending_headers` or `pending_count_duration_ms`, while operator/UI backlog visibility remains available through direct query paths such as [INDEXER_TEST_QUERIES.md](/mnt/home-datallboy/Projects/github.com/datallboy/gonzb/docs/archive/development/indexer/INDEXER_TEST_QUERIES.md:122)
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

Workstream 6 follow-up tuning note on `2026-05-06`:

- a broader live sweep was run with `3` manual `assemble --once` runs per setting and averages taken from persisted stage metrics
- tested settings and averages:
  - `25,000 / 4 workers`: about `11.11s`, about `2,252` headers/sec, `candidate_selection_duration_ms=8,615`, `header_match_duration_ms=5,570`
  - `50,000 / 4 workers`: about `18.20s`, about `2,756` headers/sec, `candidate_selection_duration_ms=12,318`, `header_match_duration_ms=12,919`
  - `50,000 / 6 workers`: about `18.42s`, about `2,716` headers/sec, `candidate_selection_duration_ms=12,884`, `header_match_duration_ms=18,126`
  - `50,000 / 8 workers`: about `18.16s`, about `2,755` headers/sec, `candidate_selection_duration_ms=12,782`, `header_match_duration_ms=23,924`
  - `60,000 / 4 workers`: about `21.59s`, about `2,779` headers/sec, `candidate_selection_duration_ms=14,004`, `header_match_duration_ms=18,425`
  - `65,000 / 4 workers`: about `22.52s`, about `2,887` headers/sec, `candidate_selection_duration_ms=14,532`, `header_match_duration_ms=18,288`
- `75,000 / 6 workers` failed immediately with `extended protocol limited to 65535 parameters`, confirming that the current claim/hydration path still has a remaining parameter ceiling above the `65,000` range
- in this local environment, `65,000 / 4 workers` produced the best average throughput of the tested settings, while pushing concurrency above `4` at `50,000` did not improve throughput and substantially increased aggregate match/upsert worker time
- the local runtime setting was updated to keep `assemble.batch_size=65000` and `assemble.concurrency=4` after the sweep

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

- [x] batch replace inserts for TMDB, predb, and TVDB release match rows
- [x] batch predb entry upserts where possible while preserving normalized-title conflict semantics
- [x] preserve chosen-match flags, payload JSON, and fallback normalization behavior
- [x] validate enrichment stages against the live dev database after the change

Acceptance criteria:

- enrichment replace paths no longer issue one insert per row in the common case
- release enrichment state remains correct for duplicate and overlapping match sets

Workstream 8 sign-off:

- complete on `2026-05-06`
- `ReplaceReleaseTMDBMatches`, `ReplaceReleasePredbMatches`, `UpsertPredbEntries`, and `ReplaceReleaseTVDBMatches` now batch their common-case insert/upsert work instead of issuing one write per row
- the predb path keeps the prior normalized-title conflict semantics by deduping repeated normalized titles within one batch using last-write-wins behavior before issuing the multi-row upsert
- chosen flags, payload JSON, and normalized-title fallback behavior were preserved and covered by a new pgindex integration test that exercises TMDB, TVDB, predb entry, and release-predb-match persistence together
- focused Go tests passed for pgindex plus the predb and tmdb enrich packages, the new Postgres-backed integration test passed, and live `go run ./cmd/gonzb --config config.yaml indexer enrich predb --once` plus `... enrich tmdb --once` sanity runs completed successfully after temporarily enabling those stages in the local runtime settings

## Workstream 9. Release File Insert Batching

Goal:

- finish reducing release-stage write amplification by batching `release_files` replacement inserts

Tasks:

- [x] batch `ReplaceReleaseFiles` inserts after the current delete and cross-release cleanup steps
- [x] preserve `release_files.binary_id` uniqueness and current file ordering semantics
- [x] validate release formation and NZB generation after the change

Acceptance criteria:

- `ReplaceReleaseFiles` no longer inserts one row at a time for normal release batches
- release and NZB behavior remains unchanged for the same candidate set

Workstream 9 sign-off:

- complete on `2026-05-06`
- `ReplaceReleaseFiles` now keeps the existing delete-and-cross-release-cleanup logic but inserts replacement `release_files` rows through one batched multi-row insert instead of one `INSERT ... RETURNING` per file
- `release_files.binary_id` uniqueness and ordering behavior were preserved because the existing cross-release binary eviction step is unchanged and the inserted rows still carry the original `file_index` values used by downstream ordering queries
- focused Go tests passed for pgindex plus the release stage package, and existing pgindex coverage for release-file-driven public/catalog behavior remained green after the batching change
- a live `go run ./cmd/gonzb --config config.yaml indexer release --once` sanity run completed successfully after the patch

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

## Workstream 11. Inspect Media Throughput And Backlog Profiling

Goal:

- determine whether the current long-tail backlog bottleneck is media inspection reservation/query work, artifact materialization, external probe tooling, or conservative runtime settings

Why this is next:

- on `2026-05-06`, live stage metrics showed `unassembled_headers=0` and recent `assemble` runs averaging well under a second when little residual work remained
- recent `release` runs stayed sub-second to low-second on the same environment
- the last five completed `inspect_media` runs averaged about `1,705s`, making media inspection the clearest remaining backlog drain

Tasks:

- [x] capture baseline live `inspect_media` timing and per-run metrics for at least three runs at the current `batch_size` and `concurrency`
- [x] profile where wall time is spent across:
  - candidate reservation
  - artifact materialization
  - media probe execution
  - persistence writes
- [x] verify whether there are repeated per-binary reads or re-materialization steps that can be cached or skipped
- [x] test higher `inspect_media` concurrency and, if safe, larger batch sizes with multiple runs per setting
- [x] determine whether the practical bottleneck is database-bound, tool/IO-bound, or CPU-bound and document the conclusion
- [x] implement the highest-signal improvement if it is low-risk and clearly supported by the measurements

Acceptance criteria:

- the dominant `inspect_media` bottleneck is identified with live measurements
- at least one tuning or code change is either implemented or explicitly ruled out with evidence
- the plan records the chosen next operating point for `inspect_media`

Workstream 11 sign-off:

- complete on `2026-05-06`
- baseline live measurements identified the dominant bottleneck as archive-backed media probing, not candidate reservation or persistence writes
- completed `inspect_media` history showed `ffprobe_archive` dominating volume at `3,195` rows, with about `112MB` average materialized bytes per binary and about `35.5s` average per-binary elapsed time
- the hot cost came from materializing large archive prefixes and extracted member prefixes before `ffprobe`, while the batched DB claim path itself remained lightweight
- a targeted fast path was implemented for archive-backed media when the archive entry name already exposes strong media signals:
  - keep the existing archive summary and filename heuristics
  - skip `7z` extraction and `ffprobe` when the archive entry already yields resolution plus codec for video, or codec for audio
  - persist completion as `probe_mode=heuristic_archive_entry`
- focused Go tests passed for media inspection plus related inspect/store packages
- live validation with the existing `inspect_media` runtime setting of `batch_size=100` and `concurrency=2` improved one completed run from about `1,569.48s` before the patch to about `75.22s` after the patch
- the post-patch run completed `95` binaries via `heuristic_archive_entry` and only `5` via heavy `ffprobe_archive`, confirming that repeated archive materialization was the main backlog drain
- a follow-up live sweep against the remaining backlog confirmed that post-fix `inspect_media` was no longer the dominant overall backlog bottleneck, but the sampled higher-concurrency comparison was not apples-to-apples because the backlog slice drained during the test window
- based on that outcome, no further `inspect_media` runtime tuning is required to close this sprint; any later concurrency sweep should be treated as a new tuning task against a controlled benchmark slice rather than a blocker for this plan

Workstream 11 follow-up on `2026-05-06`:

- the remaining heavy archive-media path was refined to stream `7z x -so ... entry` directly into `ffprobe -i pipe:0` instead of writing an extracted media prefix file back to the workspace first
- this keeps real media metadata probing for obfuscated archive-backed posts while removing one large category of SSD writes from the hot path
- inspect workspaces also gained a configurable backend:
  - `workspace_backend=disk`
  - `workspace_backend=memory`
  - `memory_work_dir=/dev/shm/gonzb-inspect` by default
- a live `indexer inspect media --once` sanity run completed successfully after the stream-path change; it still materializes the sparse archive container for the remaining hard cases, but no longer writes the extracted member prefix file before probing

## Workstream 12. Release Candidate Listing And `list_binaries` Query Optimization

Goal:

- profile and, if warranted, optimize the remaining dominant release-stage query work around candidate-family binary listing

Why this is next:

- recent live `release` runs are healthy overall, but a representative run still spent most of its time in `list_binaries_duration_ms`
- current release metrics also show many `fragment_only_families`, so we need to separate query cost from eligibility/data-shape effects before making broader schema changes

Tasks:

- [x] capture baseline live `release --once` metrics across multiple non-empty runs
- [x] identify the current SQL path behind `list_binaries_duration_ms`
- [x] run `EXPLAIN (ANALYZE, BUFFERS)` against the live dev database for the dominant release listing query
- [x] check for:
  - unnecessary row width
  - looped query patterns
  - poor join order
  - missing or low-value indexes
  - opportunities to claim/select IDs first and hydrate later
- [x] determine whether `fragment_only_families` is mainly a release throughput issue or an input-quality / eligibility issue
- [x] implement the best supported optimization if the query is still materially expensive

Acceptance criteria:

- the dominant release-stage query path is measured and documented
- any meaningful SQL optimization opportunity is either implemented or explicitly ruled out with evidence
- the plan clearly states whether release is still a backlog bottleneck after profiling

Workstream 12 sign-off:

- complete on `2026-05-06`
- baseline live release metrics showed that release was already healthy overall, but non-empty formation runs still spent the majority of their time in `list_binaries_duration_ms`
- representative pre-patch runs included:
  - run `107176`: about `1.04s` elapsed with `list_binaries_duration_ms=899.98`
  - run `107207`: about `0.96s` elapsed with `list_binaries_duration_ms=465.05`
  - run `107243`: about `0.59s` elapsed with `list_binaries_duration_ms=474.67`
- `EXPLAIN (ANALYZE, BUFFERS)` on a live actionable `base_stem` release family showed the main issue was not a missing join or N+1 loop; it was an index-mismatch predicate:
  - the old `base_stem` branch used `NULLIF(BTRIM(base_stem), '') IS NOT NULL`
  - Postgres chose `idx_binaries_release_family_key`, then filtered out about `73k` rows per worker
  - the live explain for the old shape took about `318.63ms`
- a predicate rewrite to match the partial `base_stem` index directly (`BTRIM(base_stem) <> ''`) dropped the live sample lookup to about `0.30ms` using `idx_binaries_base_stem_family_lookup`
- `ListBinariesForReleaseCandidate` was updated to:
  - use index-friendly `BTRIM(base_stem) <> ''` predicates for `base_stem` matching
  - claim candidate binary ids first, then hydrate full binary rows afterward
  - keep the existing family semantics while reducing row width inside the candidate CTE
- focused tests passed for pgindex, release, and resolver packages
- live validation after the patch showed release run `107273` completing in about `0.05s` with:
  - `formed=2`
  - `binaries_listed=123`
  - `list_binaries_duration_ms=4.941`
- conclusion:
  - the remaining `list_binaries` query had a real but narrow optimization opportunity, and it is now largely resolved
  - the high `fragment_only_families` counts are mainly an input-quality / eligibility characteristic of the queued families, not the current release-stage throughput bottleneck

## Workstream 13. Standalone Cached Dashboard Stats Expansion

Goal:

- expand operator-visible backlog stats without coupling expensive counts to stage hot paths or normal dashboard loads

Principles:

- expensive stats must remain standalone
- stage runs must not block on dashboard/operator count queries
- dashboard reads should prefer persisted cached snapshots
- manual refresh should fan out through one explicit operator action rather than being tied to unrelated stage execution

Candidate stats:

- [x] pending media inspection count
- [x] pending release candidate families count
- [ ] unreleased eligible binaries or a better-defined release backlog count
- [ ] optional enrich backlog counts if they prove useful operationally

Tasks:

- [x] define which backlog stats are worth surfacing and what each one means operationally
- [x] add a shared cached stats model and API shape that mirrors the persisted `unassembled_headers` snapshot pattern
- [x] add a dashboard refresh action that refreshes all configured cached stats in one standalone request
- [x] keep the refresh path independent from assemble, release, inspect, and enrich stage hot paths
- [x] persist snapshot timestamps and partial-failure status so the dashboard can show stale vs fresh data clearly
- [x] document which stats are exact counts and which are approximations if any expensive count needs a cheaper proxy later

Acceptance criteria:

- dashboard backlog stats load from cached persisted values by default
- operators can refresh all supported stats explicitly without tying up stage runs
- no stage command regains blocking count queries as a side effect of the dashboard expansion

Workstream 13 sign-off:

- complete on `2026-05-06`
- the dashboard stats surface was expanded from a single `unassembled_headers` snapshot into a shared cached model backed by `indexer_dashboard_stats`
- the current shipped stats are all exact counts:
  - `unassembled_headers`: article headers still waiting for assemble processing
  - `pending_media_inspection_binaries`: binaries that `inspect_media` would claim if it ran now, using the same candidate predicates as the stage itself
  - `pending_release_candidate_families`: dirty release families still waiting for release processing
- the refresh path now runs as one explicit standalone action through `/api/v1/admin/indexer/overview/stats/actions/refresh`, while normal dashboard loads read only persisted cached snapshots
- cached rows now persist:
  - last successful snapshot time
  - last refresh attempt time
  - last error text
- partial refresh failures no longer break the whole dashboard view; the UI can show a stale last-good value alongside the failed refresh attempt metadata for that stat
- focused Go tests passed for controller, store, and runtime wiring packages
- the new PostgreSQL-backed integration test `TestRefreshIndexerDashboardStatsPersistsCachedCounts` passed against the local Postgres environment
- the admin dashboard UI now exposes one `Refresh All Stats` action and renders per-stat freshness/error notes without re-coupling those counts to assemble, release, inspect, or enrich stage execution

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
10. Workstream 10: assemble yEnc recovery guardrails
11. Workstream 11: inspect media throughput and backlog profiling
12. Workstream 12: release candidate listing and `list_binaries` query optimization
13. Workstream 13: standalone cached dashboard stats expansion

## Validation

For each workstream:

- run focused Go tests first
- run live `assemble --once` or `release --once` where relevant
- record before/after stage metrics from `indexer_stage_runs`
- prefer measured runtime changes over speculative schema cleanup
