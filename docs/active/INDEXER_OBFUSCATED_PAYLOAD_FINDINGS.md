# Indexer Obfuscated Payload Findings

Snapshot date: 2026-05-19

This doc records technical findings from an externally generated payload that exposed two useful indexer/downloader lessons:

- downloader hydration should tolerate legacy XML character-set declarations
- indexer grouping should be able to promote strong recovered file identity across weak or intentionally varied header identity

This doc intentionally excludes source-identifying details. Do not add payload titles, release names, subject text, poster values, newsgroup names, message IDs, or content descriptions here.

## Baseline Before Changes

Captured during the 2026-05-19 to 2026-05-20 working session.

Importer baseline:

- external NZB hydration failed before model parsing when XML declared a legacy single-byte charset
- the common parser accepted UTF-8/UTF-16 XML only because `xml.Decoder.CharsetReader` was unset
- internal NZB export still appeared UTF-8-oriented; this was an import hardening gap, not a reason to emit legacy-encoded NZBs

Header-shape baseline:

- file entries: 66
- article segments: 9,647
- unique posters: 66
- unique subjects: 66
- unique date values: 1
- unique group sets: 9
- unique groups: 9
- unique segment byte values: 3
- unique segment numbers: 147
- unique message IDs: 9,647

Downloader post-process baseline:

- download completed into separate extensionless output files
- extensionless files included archive signatures, so the bytes were present but archive detection did not classify them
- archive extraction was extension-driven and therefore skipped the extensionless archive family
- no extracted final media artifact was moved into the completed output

Dashboard and backlog-count baseline:

- yEnc recovery and PAR2 dashboard rows both displayed exactly `1000`, which was suspicious because it matched the bounded sample cap rather than a real backlog measurement
- cached dashboard rows were stale, so the UI could keep showing the old capped value even after query logic changed
- direct PAR2 exact backlog measurement returned `18060` rows during the session
- exact yEnc recovery measurement needed query support before it was safe to use as a routine dashboard stat
- the exact yEnc dashboard count later hung during refresh because the count path had to evaluate the first-part payload relationship for every eligible weak/obfuscated binary
- after cache invalidation, the live stats endpoint reported yEnc and PAR2 backlog rows as unavailable while refresh data was missing instead of continuing to show stale `1000` values
- a direct yEnc recovery one-shot completed `100/100` attempted and recovered with no fetch or parse failures under live settings (`batch_size=100`, `concurrency=4`)
- a direct PAR2 inspection one-shot used live `batch_size=250` and did not finish inside 240 seconds; progress metrics showed most candidates completing in hundreds of milliseconds, with periodic slow candidates in the 27-36 second range

Schema-change baseline:

- query support belongs in migrations before runtime use
- no live database schema changes should be used as the normal path for this sprint
- one manual diagnostic index was created during investigation and then captured as migration `023` so the tracked schema remains authoritative

## Import Encoding Finding

The downloader hydration failure was caused by XML declaring a legacy single-byte encoding while the shared NZB parser uses Go's `encoding/xml` decoder without a `CharsetReader`.

Current parser path:

- `internal/nzb/parser.go`
- `xml.NewDecoder(r)`
- no decoder `CharsetReader`

Go's XML decoder only handles UTF-8 and UTF-16 by default. When an XML declaration names another encoding, decoding fails before the NZB model is hydrated.

Likely fix:

- add a `CharsetReader` in the common NZB parser
- support at least `iso-8859-1` and common aliases
- preferably use the standard `golang.org/x/net/html/charset` helper if dependency policy allows it
- add a tiny synthetic regression fixture that declares a legacy encoding and contains non-sensitive placeholder NZB data

This should be treated as an external-import hardening issue, not as evidence that the internal indexer should emit non-UTF-8 NZBs. The internal NZB generation paths use Go XML marshaling plus `xml.Header`, which emits UTF-8 XML.

## Observed Header Shape

The payload shape is strongly obfuscated at the NZB header layer:

- file entries: 66
- article segments: 9,647
- unique posters: 66
- unique subjects: 66
- unique date values: 1
- unique group sets: 9
- unique groups: 9
- unique segment byte values: 3
- unique segment numbers: 147
- unique message IDs: 9,647

Interpretation:

- `poster` is intentionally weak grouping evidence for this shape
- `subject` is intentionally weak grouping evidence for this shape
- `date` is useful as a proximity signal but too broad to identify a release by itself
- segment byte counts are useful as completion facts but too low-cardinality for identity
- message IDs remain exact article identity but do not reveal release membership alone
- newsgroup membership may be split across multiple groups, so a per-newsgroup-only grouping boundary can fragment an otherwise coherent file set

The important stable signal is expected to appear after reading yEnc headers from article bodies. If recovered yEnc names align, they are stronger identity evidence than the surrounding NZB header metadata.

## Audit Findings

Downloader import:

- legacy charset support belongs in the shared NZB parser so downloader and any other NZB consumers get identical behavior
- parser regression coverage should stay synthetic and non-identifying

Downloader extraction:

- extension-only archive detection is too weak for obfuscated payloads
- archive signature checks already existed inside individual extractors, so the safe improvement was to allow extensionless candidates into those checks
- extensionless archive families need deduplication before extraction; otherwise every volume can look like a primary archive candidate
- after extraction succeeds, extensionless archive artifacts must be excluded from the completed output just like `.rar`, `.7z`, `.zip`, `.par2`, and related artifacts
- downloader yEnc header filenames are parsed during segment decoding, but live task file paths are already preallocated and written concurrently by that point
- adopting yEnc names in downloader output needs a coordinated task/file-handle path transition; it should not be patched as an unsynchronized worker-side rename

Indexer backlog visibility:

- yEnc recovery backlog should count claimable rows, not rows the stage will skip because of missing subject names or retry backoff
- exact dashboard counts need supporting indexes before replacing capped estimates
- UI values equal to the measurement cap must be visibly treated as capped or stale until refreshed
- old cached capped rows can become misleading when a stat definition changes from bounded to exact, so definition-changing migrations should invalidate affected cached dashboard rows
- yEnc recovery is not currently safe as a full exact dashboard count; bounded selector-backed measurement is preferred until a dedicated rollup or materially faster count path exists
- dashboard refresh must protect itself with per-stat timeouts so one bad backlog query cannot hang the whole admin view
- stage metrics should expose whether configured batches fill and what effective concurrency was used
- yEnc recovery stage metrics should also separate candidate-selection time from fetch/process time so selector regressions are visible
- PAR2 inspection needs progress and timing metrics because a filled batch can contain a small number of slow candidates that dominate wall time
- PAR2 target coverage updates must use the normalized file identity expression that has index support
- PAR2 target coverage should update target matches in batches; per-target updates turned some candidates into avoidable database stalls
- inspection stages can race release cleanup or re-formation, so stale release ids must not make binary-scoped inspection artifacts violate release foreign keys or fail otherwise completed binary inspections

Release grouping:

- `poster`, `subject`, and single-group boundaries are weak for this obfuscated shape
- recovered yEnc identity is the first strong grouping evidence available without full payload download
- cross-group promotion should stay bounded to recovered identity, compatible file counts, and close posting proximity
- group provenance must remain attached for fetch routing even if release formation bridges groups
- current release-family summaries are keyed by `provider_id + newsgroup_id + key_kind + family_key`
- the main release candidate queue carries `newsgroup_id` forward, so recovered identity does not yet produce one cross-group dirty candidate by default
- `ListBinariesForReleaseCandidate` can already omit the group boundary when called with `newsgroup_id = 0`, but the candidate queue and ack path do not yet model that as a normal candidate shape
- cross-group promotion should therefore be added as an explicit recovered-identity candidate path instead of weakening all existing summary boundaries

## Indexer Grouping Implications

The existing `recover_yenc` direction remains the right repair path for this family of obfuscation. BODY-prefix recovery gives the indexer access to the yEnc filename without requiring full article download.

The sample suggests a narrower next improvement: keep initial scrape and article identity scoped by provider/newsgroup for operational safety, but allow later release grouping to use a recovered identity key that can bridge newsgroup splits when the evidence is strong enough.

Potential model:

- preserve `provider_id + newsgroup_id + article_number/message_id` as ingest and binary membership identity
- preserve `newsgroup_id` on binaries for provenance and fetch routing
- derive a cross-group candidate key only from strong identity evidence, such as recovered yEnc filename/base stem plus compatible part counts and time proximity
- form or promote release candidates by `provider_id + recovered_file_set_key` across multiple `newsgroup_id` values when all files have strong recovered identity
- store all participating groups on the resulting release through `release_newsgroups`

Guardrails:

- do not use poster similarity for this pattern
- do not bridge groups from subject-only contextual fallback
- require recovered yEnc identity or equivalent strong archive/PAR2 identity
- require close posted-at proximity
- require compatible expected file count or archive target evidence
- keep cross-group promotion bounded so broad backfills do not explode into noisy global joins

## Concrete Backlog Candidates

1. Downloader parser hardening

   Add legacy charset support to the common NZB parser and cover it with a synthetic test.

   Status: done on 2026-05-19. The shared parser now installs a charset reader, and `internal/nzb/parser_test.go` covers a synthetic legacy-encoded NZB.

2. Cross-group recovered-identity promotion

   Add a release-summary path that can promote strong recovered identity across newsgroups while preserving per-group article membership.

3. Recovery candidate discovery audit

   Check whether `recover_yenc` selection is too tightly bound to one newsgroup candidate at a time. If so, add metrics showing how many weak binaries share a recovered base key across groups after recovery.

   Status: started on 2026-05-19. The dashboard yEnc backlog count now mirrors the claimable recovery selector, including subject-name and retry-backoff filters, so it no longer overstates rows the stage cannot inspect now. Query-support indexes were added so the dashboard can show the exact claimable backlog instead of a capped sample. `recover_yenc` stage metrics also report effective concurrency and whether a run filled its configured batch, which makes bottleneck checks clearer.

4. Release candidate query review

   Review current release candidate queries for places where `newsgroup_id` is still part of the grouping partition even after strong `base_stem` or recovered identity is available. Keep the ingest boundary, but consider relaxing the release-formation boundary only for high-confidence identity.

   Status: started on 2026-05-19. Dashboard bounded backlog stats expose when remaining estimated counts hit the measurement cap. yEnc recovery and PAR2 inspection now use exact indexed counts, making capacity tuning clearer before changing release grouping boundaries.

5. NZB export normalization

   Keep internally generated NZBs UTF-8, deterministic, and provenance-complete. For multi-group releases, include the full release group set while retaining file/article membership accuracy.

6. Downloader extensionless archive extraction

   Detect archive payloads by signature when filenames are extensionless, extract one representative per extensionless archive family, and drop original archive artifacts after successful extraction.

   Status: done on 2026-05-20. `internal/processor` now detects extensionless RAR, 7z, and ZIP signatures, dedupes extensionless archive-family extraction candidates per directory and signature kind, and excludes extensionless archive artifacts from completed output after extraction. Synthetic processor tests cover signature detection, family dedupe, artifact dropping, and extensionless 7z post-processing.

7. Dashboard exact-count cache invalidation

   Invalidate cached yEnc/PAR2 dashboard rows when their definitions change from capped samples to exact indexed counts, so stale `1000` values are not presented as exact values.

   Status: done on 2026-05-20. Migration `024` deletes the affected cached rows and bumps the expected pgindex schema version to `24`. After this migration applies, the UI should show those stats as uncached until the refresh endpoint recomputes them with the exact-count queries.

8. yEnc dashboard refresh hang fix

   Keep yEnc backlog refresh bounded to the actual recovery selector rather than running a full exact count across every eligible weak/obfuscated binary.

   Status: done on 2026-05-20. The yEnc dashboard stat is again a capped backlog snapshot (`1000+` when saturated), backed by `ListYEncRecoveryCandidates`, and migration `025` invalidates the affected cached row. Dashboard stat refresh also has a per-stat timeout so a future slow stat persists an error instead of hanging the whole refresh.

9. yEnc recovery timing metrics

   Add timing fields that distinguish slow candidate selection from slow article prefix recovery.

   Status: done on 2026-05-20. `recover_yenc` metrics now include `candidate_selection_ms` and `processing_ms` alongside `batch_full`, `effective_concurrency`, `attempted`, and `recovered`.

10. PAR2 inspection throughput hardening

   Keep PAR2 inspection observable and prevent target coverage updates from becoming the bottleneck for obfuscated archive sets.

   Status: done on 2026-05-20. `inspect_par2` now reports candidate-selection, processing, and per-candidate progress timing. PAR2 target coverage updates use the normalized file identity expression that matches the existing index and batch target updates per candidate. Live one-shot testing still showed that `batch_size=250` is too large for the current data shape when periodic slow candidates appear, so the PAR2 one-shot loop now stops cleanly after a bounded run budget and leaves remaining candidates for the next scheduler tick.

11. Stale release-link inspection hardening

   Keep binary-scoped inspection outputs durable when a release row is deleted or replaced while an inspection stage is running.

   Status: done on 2026-05-20. Inspection artifact/archive/media replacement now keeps valid release ids but stores stale release ids as `NULL`, preserving foreign keys without dropping the binary-scoped evidence. Archive, media, PAR2, NFO, and password stages now treat missing release rollups as stale-link skips after completing the binary inspection, instead of failing the entire stage run.

## Action Item Sign-Off

Done:

- [x] Create an active, non-identifying findings doc for the obfuscated payload working session
- [x] Add shared NZB parser legacy charset support
- [x] Add a synthetic parser regression test for legacy charset declarations
- [x] Add migration-backed query indexes for yEnc/PAR2 backlog visibility
- [x] Replace suspicious capped yEnc/PAR2 dashboard counts with exact indexed counts where query support exists
- [x] Add yEnc recovery metrics for effective concurrency and full-batch detection
- [x] Fix downloader post-process handling for extensionless archive artifacts
- [x] Record baseline and audit findings in this active doc
- [x] Add migration-backed invalidation for stale yEnc/PAR2 cached dashboard stats
- [x] Audit downloader yEnc filename adoption risk
- [x] Audit release candidate grouping boundaries for recovered cross-group identity
- [x] Bound yEnc dashboard backlog refresh after exact count hang
- [x] Add per-stat dashboard refresh timeout guardrail
- [x] Add yEnc recovery candidate-selection and processing timing metrics
- [x] Refresh live dashboard stats enough to verify stale `1000` rows no longer display as exact values
- [x] Audit yEnc recovery selection after live stats refresh with a direct one-shot run
- [x] Add PAR2 inspection candidate-selection and per-candidate progress timing metrics
- [x] Align PAR2 target coverage updates with normalized file identity index support
- [x] Batch PAR2 target coverage updates per candidate
- [x] Add a bounded PAR2 run budget so large configured batches cannot monopolize the scheduler loop
- [x] Harden inspection artifact and release-rollup writes against stale release links

Needs completion:

- [ ] Design a fast full-cardinality yEnc recovery backlog rollup if operators need exact yEnc backlog size instead of a capped claimable snapshot
- [ ] Design and implement bounded cross-group recovered-identity promotion
- [ ] Add a synthetic multi-group recovered-yEnc grouping fixture
- [ ] Design downloader yEnc filename adoption as a coordinated task/path transition if post-extraction signature handling is not sufficient for future samples
- [ ] Confirm internal NZB export remains UTF-8, deterministic, and complete for multi-group releases

Deferred unless new evidence requires it:

- [ ] Split indexer modules into separate products or processes
- [ ] Use poster or subject similarity alone to bridge obfuscated releases across groups
- [ ] Add committed fixtures derived from the observed external payload

## Validation Notes

Use non-sensitive synthetic fixtures for automated tests:

- one legacy-encoded NZB parser fixture
- one multi-group obfuscated header fixture with randomized posters and subjects
- one recovered-yEnc grouping fixture showing same recovered base identity across multiple newsgroups

Avoid adding the observed external payload as a committed fixture.
