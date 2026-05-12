# Indexer Release Fragment Selection Plan

Snapshot date: 2026-05-07

This doc is the active execution guide for investigating why release formation remains weak even after the assemble lane split and for deciding which fragment checks should move earlier into release candidate selection.

## Sprint Goal

Determine:

1. why `release` still forms very few releases on the current backlog
2. whether fragment rejection can move from post-selection cluster persistence into earlier candidate selection
3. what summary/schema/runtime changes are justified by the live evidence

## Current Finding

Release is not primarily blocked by:

- `min_expected_file_coverage_pct`
- `min_completion_pct`
- `min_confidence`

Release is primarily blocked by fragment-heavy families that are still reaching post-selection clustering.

## Live Evidence

### Release run with new fragment-reason metrics

Live `release --once` on the current backlog:

- `2026-05-07 14:17:27`
  - `candidate_families=10000`
  - `formed=1`
  - `cooled_down_fragment_only_families=662`
  - `cooled_down_low_coverage_families=34`
  - `stale_cleanup_only_families=5`
  - `skipped_fragments=9298`
  - `skipped_fragments_no_main_payload=0`
  - `skipped_fragments_single_main=301`
  - `skipped_fragments_multi_file=0`
  - `skipped_fragments_contextual_weak=8997`
  - `skipped_confidence=0`
  - `skipped_completion=0`

Interpretation:

- the `90%` expected-file-coverage threshold is not the main limiter
- confidence and completion gates are not the blocker
- the dominant failure path is:
  - `contextual_obfuscated`
  - weak standalone candidate
  - no expected file count

### Live summary-table distribution

Pending summary-backed release families:

- `actionable`: `24,322`
- `fragment_only`: `11,347`

Actionable families by `complete_main_payload_binary_count`:

- `1`: `23,343`
- `>=2`: `979`

Actionable families with `complete_main_payload_binary_count = 1`:

- `binary_count = 1`: `23,322`
- `binary_count > 1`: `21`

Actionable families by `family_kind` across their binaries:

- `contextual_obfuscated`: dominant
- `readable_title`: much smaller
- `archive_stem`: small

Single-binary actionable families are overwhelmingly:

- `contextual_obfuscated`
- `expected_file_count = 0`
- opaque `.bin`-style names in live samples

Representative live sample:

- `cf1b742a8362436c8a2a71369360b628.bin`
- `f8a28cc7caa64efeaea3bc05acdeec4a.bin`
- `bd4763a766c04621893c7d9d3fba5a79.bin`

### Follow-up single-binary inspection

Direct live inspection of actionable single-binary `release_family` rows showed:

- `333,830` actionable single-binary release-family rows
- `333,819` with `expected_file_count = 0`
- `319,100` with `family_kind = contextual_obfuscated`
- `319,095` whose dominant file name looked like `.bin`, `.rNN`, `.partNN`, or `.volNN+NN`
- `0` that looked like named standalone media files such as `.mkv`, `.mp4`, or `.mp3`

Representative live sample:

- `mK2g7T4K10bNxci0h75V7gZ8ONA.r07`
- `04601b416a624006a3ef2df4717c6ede.bin`
- `81GbGh8PwUDdQP0R8Nvu2y1amNEbFlKECMvPGG.bin`

## Conclusion

The current selector is still too permissive, but the fix is not simply “apply the existing fragment check earlier.”

Reason:

- the current post-selection fragment gate uses cluster-level evidence
- some of that evidence is not present in `release_family_readiness_summaries` today
- especially:
  - dominant main binary file name readability
  - standalone media/archive hints
  - title-source/title-confidence fallback evidence

So:

- a full move of `shouldPersistCluster(...)` into selection is not possible with the current summary shape
- a partial move is both possible and high-value

## Best Route

### Phase 1. Split actionable families into stronger summary-time buckets

Status: implemented.

Done in:

- `release_family_readiness_summaries` refresh logic
- release candidate selection fallback logic for older summary rows that have not been refreshed yet

Add richer summary-time classification for families such as:

1. `actionable_multi`
   - `complete_main_payload_binary_count >= 2`
   - strongest release candidates

2. `actionable_standalone_candidate`
   - `binary_count = 1`
   - `complete_main_payload_binary_count = 1`
   - summary hints say the single binary could plausibly be a standalone release

3. `weak_contextual_single`
   - `binary_count = 1`
   - `complete_main_payload_binary_count = 1`
   - `family_kind = contextual_obfuscated`
   - no expected file count
   - opaque `.bin` or otherwise weak standalone evidence

4. keep:
   - `fragment_only`
   - `stale_cleanup_only`

This lets release avoid spending full candidate-expansion work on the weakest single-binary contextual cases.

### Live result after Phase 1

Live `release --once` after the weak-single cooldown landed:

- `2026-05-07 14:40:01`
  - `candidate_families=10000`
  - `formed=0`
  - `cooled_down_fragment_only_families=3702`
  - `cooled_down_low_coverage_families=38`
  - `cooled_down_weak_single_families=5635`
  - `stale_cleanup_only_families=1`
  - `skipped_fragments=2128`
  - `skipped_fragments_contextual_weak=2128`

Interpretation:

- the weak single-binary contextual families are now being filtered much earlier
- post-selection fragment work dropped materially from the earlier `9298` fragment skips
- release is still not forming much from this backlog, which means the next bottleneck is the remaining fragment-only and weak multi-binary grouping cases rather than the single-binary contextual noise

### Follow-up multi-binary inspection

Direct live inspection of multi-binary actionable `release_family` rows showed a second major issue:

- the remaining large actionable backlog is almost entirely `contextual_obfuscated`
- sample families averaged `6.76` binaries each
- their average `expected_file_count` was only `0.61`, but a meaningful subset already had:
  - `expected_file_count > 1`
  - nonblank `base_stem`
  - positive `file_index`
- representative large contextual families contained dozens or hundreds of distinct `base_stem` values under one coarse `release_family_key`

Interpretation:

- these are not mainly missing article parts
- many of them already have archive-style grouping signals
- the remaining waste is that release is still seeing coarse contextual `release_family` groups that should be deferred in favor of stronger `base_stem` grouping

### Phase 2. Add summary fields needed for early standalone triage

Status: partially implemented.

Current summary rows now persist:

- `dominant_family_kind`
- `dominant_file_name`
- `dominant_match_confidence`

That is enough for the current weak-single filter.

Current summary rows still do not know enough to distinguish:

- “single complete binary that should be a release”
- from “opaque single `.bin` that should cool down”

Add summary fields derived from the dominant main payload binary:

- `dominant_family_kind`
- `dominant_file_name`
- `dominant_file_ext`
- `dominant_file_index`
- `dominant_match_confidence`
- `single_binary_family`
- `standalone_candidate_hint`

The exact schema can stay compact if:

- file name is optional
- extension/family kind/confidence carry most of the decision weight

### Phase 3. Change release candidate ordering and cooldown

Status: implemented for the weak-single path and the first coarse multi-binary contextual deferral path.

New ordering target:

1. `stale_cleanup_only`
2. `actionable_multi`
3. `actionable_standalone_candidate`
4. `fragment_only`
5. `weak_contextual_single`

Likely behavior:

- `weak_contextual_single` should normally be cooled without full binary listing
- `actionable_standalone_candidate` can still be inspected, but after stronger multi-binary families
- `prefer_base_stem` coarse contextual multi-binary families should cool without full binary listing so release can focus on stronger actionable rows

### Live result after Phase 3 follow-up

During the follow-up implementation we identified a large class of coarse contextual multi-binary families where:

- `family_kind = contextual_obfuscated`
- `expected_file_count > 1`
- `file_index > 0`
- `base_stem` is already present on multiple binaries

These are strong signs that the better grouping unit is `base_stem`, not the current coarse `release_family` row.

The selector now derives a `prefer_base_stem` bucket for that shape and cools those candidates before full binary listing.

### Fresh queue validation after handoff wiring

With the service stopped, `release --once` correctly reported no candidates because the active queue was empty:

- `SELECT count(*) FROM release_family_readiness_summaries WHERE updated_at > COALESCE(processed_at, updated_at)` returned `0`

That was a true empty queue, not a selector false negative.

After a fresh live `assemble lane-b --once`, the queue repopulated with:

- `base_stem actionable`: `2`
- `base_stem fragment_only`: `119`
- `release_family actionable`: `587`
- `release_family fragment_only`: `19`
- `release_family stale_cleanup_only`: `8`
- `release_family weak_single_binary`: `6,237`

The next live `release --once` produced:

- `2026-05-07 15:08:24`
  - `candidate_families=6972`
  - `formed=0`
  - `cooled_down_fragment_only_families=138`
  - `cooled_down_low_coverage_families=4`
  - `cooled_down_weak_single_families=6237`
  - `cooled_down_prefer_base_stem_families=0`
  - `stale_cleanup_only_families=8`
  - `skipped_fragments=852`
  - `skipped_fragments_contextual_weak=852`

Interpretation:

- the empty-queue result was valid
- the weak-single filter remains the biggest win on fresh backlog slices
- the `prefer_base_stem` handoff is now wired, but this measured batch did not contain pending candidates that matched the current promotion threshold
- the next remaining work is tightening coarse multi-binary contextual grouping further so more of those families either promote into `base_stem` earlier or get filtered before post-cluster fragment checks

### Upstream grouping follow-up

The matcher previously refused to use archive-stem grouping for larger archive families once `expected_file_count > 16`.

That cap was too restrictive for the live backlog, where many obfuscated archive-style sets showed:

- `.partNN.rar`
- `.volNN+MM.par2`
- `expected_file_count` values such as `142`, `208`, `285`, and `402`

Implemented change:

- keep large mixed-stem `.7z.001` families on contextual fallback
- allow larger `.partNN.rar` / `.volNN+MM.par2` / `.par2` style families to promote into archive-stem grouping upstream

This preserves the safety of the large mixed-stem `.7z.001` cases while improving the families that already expose a shared archive-family stem.

Fresh live validation after the matcher change:

1. Active queue before run:
   - `0`
2. Live `assemble lane-b --once`
3. Pending queue after run:
   - `base_stem actionable`: `7`
   - `base_stem fragment_only`: `56`
   - `release_family actionable`: `566`
   - `release_family fragment_only`: `16`
   - `release_family stale_cleanup_only`: `1`
   - `release_family weak_single_binary`: `6,399`
4. Live `release --once`:
   - `candidate_families=7045`
   - `formed=0`
   - `cooled_down_weak_single_families=6399`
   - `skipped_fragments=3097`

Interpretation:

- upstream grouping is improving incrementally
- more strong `base_stem` candidates are appearing on fresh backlog slices
- release still is not forming releases from this slice, so the next bottleneck remains the remaining coarse contextual multi-binary families and fragment-heavy clusters

### Multi-run benchmark

To avoid over-reading a single `2 -> 7` snapshot, three fresh live cycles were run on 2026-05-07 using:

1. `assemble lane-b --once`
2. queue snapshot
3. `release --once`

Results:

- Cycle 1
  - queue:
    - `base_stem actionable`: `3`
    - `release_family actionable`: `577`
    - `release_family weak_single_binary`: `3,674`
  - release:
    - `candidate_families=4278`
    - `formed=0`
    - `cooled_down_weak_single_families=3674`
    - `skipped_fragments=2081`

- Cycle 2
  - queue:
    - `base_stem actionable`: `3`
    - `release_family actionable`: `392`
    - `release_family weak_single_binary`: `3,125`
  - release:
    - `candidate_families=3540`
    - `formed=0`
    - `cooled_down_weak_single_families=3125`
    - `skipped_fragments=406`

- Cycle 3
  - queue:
    - `base_stem actionable`: `5`
    - `release_family actionable`: `366`
    - `release_family weak_single_binary`: `3,578`
  - release:
    - `candidate_families=3971`
    - `formed=0`
    - `cooled_down_weak_single_families=3578`
    - `skipped_fragments=1232`

Interpretation:

- this is not yet a strong enough data set to claim a decisive improvement from the matcher change alone
- the trend is directionally encouraging because strong `base_stem` rows are appearing repeatedly on fresh backlog slices
- but release formation is still `0` across these three cycles
- the dominant remaining queue shape is still weak single-binary contextual families plus contextual multi-binary fragment work
- the next phase should keep pushing more coarse contextual multi-binary families into stronger upstream grouping instead of relying on release-time filtering

### Deep live root-cause check

Additional live Postgres inspection on 2026-05-07 separated the remaining failures into two distinct classes.

1. Pure opaque junk families

- sampled rows had:
  - opaque one-token subjects
  - no `subject_file_index`
  - no `subject_file_total`
  - no yEnc file counters beyond trivial single-part hints
- payload inspection showed the structure was not being dropped by persistence
- the useful grouping data simply was not present in the scraped headers

Conclusion:

- this class is not fixable by a release-key refactor alone
- it needs either deferred recovery/enrichment or aggressive cooldown as non-actionable

2. Structured but still overgrouped contextual families

Representative live family:

- `bob home com news easynews com alt binaries boneless alt binaries kenpsx 20260507 2 release 144004950000 files 60`
- summary row:
  - `binary_count=1397`
  - `complete_main_payload_binary_count=2`
  - `expected_file_count=60`
- live binary shape:
  - `distinct_base_stem_count=1397`
  - `indexed_file_count=1397`
  - `all_contextual=true`

Representative subject samples:

- `[04/60] "KLEDYqhq4tScziCMFNikMxj8awueRYCz" yEnc (1/4)`
- `[08/60] "zlP9IzWDjOvfpjXGym69zpUyLeRkYTKT" yEnc (04/49)`
- `[09/60] "ygSNM2gTfRH9t1CTxyDDubu4AnhnCBbj" yEnc (96/96)`

Interpretation:

- structured counters are already being scraped and persisted
- these are not article-to-binary assembly failures
- the current contextual `release_family_key` is too coarse for same-poster same-window same-`files-N` posting waves
- `prefer_base_stem` was also too broad for this shape because every binary had a unique stem, so there was nothing real to consolidate

### Phase 3 follow-up. Cool obviously overgrouped contextual waves earlier

Status: implemented.

The selector now derives an `overgrouped_contextual` cooldown bucket when a `release_family` row is:

- `contextual_obfuscated`
- indexed multi-file
- large relative to `expected_file_count`
- and every discovered `base_stem` is unique

This catches the “unique-stem matrix” shape before release spends full binary-listing and cluster work on it.

It also tightens `prefer_base_stem` so it only fires when stems actually repeat, which means there is a real consolidation opportunity.

Live validation of the representative family above with the new heuristic:

- `binary_count=1397`
- `expected_file_count=60`
- `distinct_base_stem_count=1397`
- derived bucket: `overgrouped_contextual`

### Phase 3 follow-up. Live NNTP metadata probe

Status: investigated.

`cmd/articleprobe` was run against the representative overgrouped family using live NNTP `STAT`, `XOVER`, `HEAD`, and `BODY`.

Findings:

- `XOVER` and `HEAD` do not expose extra grouping identity beyond the subject, poster, groups, dates, and article number
- the BODY yEnc header does expose the real file name immediately
- the real yEnc names are exactly the stable grouping data missing from XOVER/header scraping

Representative article:

- XOVER subject:
  - `[01/60] "QWMnpmgkZ12NwFmZB8sWPTs2rFrTBp2S" yEnc (1/1)`
- BODY yEnc header:
  - `=ybegin part=1 total=1 line=128 size=165616 name=Below.Deck.Down.Under.S04E13.The.Way.the.Cookie.Crumbles.1080p.AMZN.WEB-DL.DDP2.0.H.264-Kitsune.par2`

Second representative article:

- XOVER subject:
  - `[06/60] "RnZJBxsqpTzjUjMuSo3olzQTQUGZeEY8" yEnc (13/13)`
- BODY yEnc header:
  - `=ybegin part=13 total=13 line=128 size=9051800 name=Below.Deck.Down.Under.S04E13.The.Way.the.Cookie.Crumbles.1080p.AMZN.WEB-DL.DDP2.0.H.264-Kitsune.vol015+016.par2`

Interpretation:

- for this class, the missing signal is not available through XOVER or HEAD
- yEnc BODY-header recovery is the right enrichment mechanism
- the recovery should run as a separate low-concurrency stage so lane B stays fast
- candidates should be marked or selected from known weak shapes:
  - `overgrouped_contextual`
  - `weak_single_binary`
  - contextual binaries with empty/opaque subject filenames and no prior successful yEnc recovery
- successful recovery should move article parts onto binaries keyed by the recovered yEnc filename and refresh both old and new release-family summaries

Implemented next implementation:

1. Added a `recover_yenc` indexer stage with runtime settings and CLI support via `gonzb indexer recover-yenc --once`.
2. Candidate selection targets weak/overgrouped contextual binaries where XOVER/subject data did not expose a useful filename.
3. The NNTP provider now has a prefix-only `BODY` fetch path. It reads a bounded prefix for `=ybegin` and discards the connection instead of draining the full article body.
4. The stage rematches with recovered `name`, `part`, `total`, and `size` values from the yEnc header.
5. Successful recovery re-keys or merges binaries by the recovered yEnc filename, updates binary parts/release files, and refreshes old and new release-family summaries.
6. `430` / not-found and parse failures reuse the existing `yenc_recovery_retry_after` backoff so bad candidates do not churn every pass.

Operational notes:

- `XOVER`, `HEAD`, and portable `XHDR` commands cannot return the yEnc body header; `BODY` or `ARTICLE` is still required.
- The stage intentionally runs after assemble. It should not be used to make lane B slower.
- Start with a small batch size such as `25` and concurrency `1`; increase only after live metrics show low fetch failure and merge rates are stable.

### Live validation after `recover_yenc` implementation

Status: initial smoke passed, release impact still needs more backlog cycles.

Live `recover-yenc --once` runs on 2026-05-11:

- Run 1:
  - `candidates=25`
  - `attempted=25`
  - `recovered=25`
  - `merged=4`
  - `not_found=0`
  - `fetch_failures=0`
  - `parse_failures=0`
- Run 2:
  - `candidates=25`
  - `attempted=25`
  - `recovered=24`
  - `merged=1`
  - `not_found=1`
  - `fetch_failures=0`
  - `parse_failures=0`
- Run 3:
  - `candidates=25`
  - `attempted=25`
  - `recovered=25`
  - `merged=0`
  - `not_found=0`
  - `fetch_failures=0`
  - `parse_failures=0`

Current live database count after the smoke:

- `binaries.recovered_source = 'yenc_header'`: `102`

Follow-up `release --once` after the first and third recovery passes still formed `0` releases:

- after run 1:
  - `candidate_families=122`
  - `formed=0`
  - `stale_cleanup_only_families=113`
  - `skipped_fragments_contextual_weak=6`
- after run 3:
  - `candidate_families=103`
  - `formed=0`
  - `stale_cleanup_only_families=97`
  - `skipped_fragments_contextual_weak=11`

Interpretation:

- BODY-header recovery is actionable and clean on sampled candidates
- the selector originally needed tightening; candidate query now returns in roughly `211 ms` on live Postgres
- release formation did not improve from only `75` small-batch recovery attempts, so the next measurement should run repeated recovery plus assemble/release cycles until enough related files in a family have recovered yEnc names
- the current release queue is mostly stale cleanup, so release impact needs to be measured after recovered summaries accumulate, not from a single immediate release pass

### 2026-05-11 live assemble/recover/release validation

Repeated live cycles were run after the lane split and `recover_yenc` stage wiring:

- Lane A remained fast and mostly cache-hit driven.
  - representative pass: `lane_a_selected=899`, `processed_headers=899`, `binaries_refreshed=16-64`, `headers_per_second=931-2550`
- Lane B remained the heavier backlog-drain path.
  - representative pass: `lane_b_selected=2500` per worker, `binaries_refreshed=1273-1848`, `headers_per_second=285-431`
- `recover_yenc --once` stayed clean.
  - latest pass: `candidates=25 attempted=25 recovered=25 merged=2 not_found=0 fetch_failures=0 parse_failures=0`
- `release --once` after fresh assemble/recovery input:
  - `candidate_families=5473`
  - `formed=0`
  - `cooled_down_fragment_only_families=656`
  - `cooled_down_weak_single_families=4489`
  - `cooled_down_overgrouped_families=1`
  - `cooled_down_prefer_base_stem_families=1`
  - `skipped_fragments_contextual_weak=264`

Database edge-case inspection:

- `releases` with files: `10763`
- one-file releases: `396`
- one-article releases: `32`
- huge one-article releases: `0`
- `binaries`: `3476719`
- one-part binaries: `3133910`
- huge one-part binaries: `0`
- max binary size: `14158922512` bytes, represented by high-part-count binaries rather than single article rows

Interpretation:

- the very large files are not currently showing as one-article size-accounting failures
- small one-article release rows mostly appear to be old or weak identity releases, not valid strong formations
- current formation is low because the fresh queue is dominated by weak single binaries and contextual weak groups
- the remaining release work is now more accurately classified; the next improvement should focus on increasing upstream usable identity evidence, not relaxing release thresholds

### Numeric opaque subject guard

Live DB inspection found recent bad formations shaped like:

- release title/source: `80791181 n`
- files: two extensionless opaque yEnc names
- XOVER subject: `80791181-n [1/2] - "opaque-token" yEnc (1/1)`

These were being treated as `readable_title` because the numeric subject fragment looked like a source title. They are not strong release identities.

Status: implemented.

- release cluster persistence now rejects numeric subject-derived titles when the cluster has no usable file identity
- release candidate selection now classifies those rows as fragment cooldown before binary expansion
- usable identity currently means archive/media/PAR filename evidence or a high-confidence non-source recovered title
- regression coverage was added for both the release service and Postgres selector paths

### Phase 4. Keep post-cluster fragment enforcement

Even after richer summary-time filtering:

- keep `shouldPersistCluster(...)`
- keep fragment reason metrics

Reason:

- some ambiguous cases will always require cluster-level evidence
- early filtering should reduce wasted work, not eliminate final correctness checks

## Why This Is Better Than Only Tuning Thresholds

Threshold tuning does not solve the observed backlog shape.

Live evidence already showed:

- coverage threshold is barely firing
- confidence threshold is not the problem
- completion threshold is not the problem

The real waste is that release is mostly inspecting single-binary contextual-obfuscated families that are poor standalone candidates.

## Acceptance Criteria

The next implementation pass should be considered successful when:

1. release logs show most skipped fragment work removed from the hot path
2. `candidate_families` drops materially for equivalent release runs
3. `formed` rises on the same backlog, or release reaches the real remaining bottleneck faster
4. fragment reason metrics remain available for post-change validation

## Immediate Next Changes

- keep the new fragment-reason metrics
- keep the new weak-single summary fields
- keep the selector-side fallback so older summary rows benefit immediately
- investigate the remaining `fragment_only` / `skipped_fragments_contextual_weak` families after the single-binary cleanup
- run repeated `recover_yenc --once`, assemble, and release cycles to measure whether recovered yEnc names turn overgrouped contextual waves into formable release candidates
- focus next grouping work on recovered yEnc filenames rather than broader article-bucket tuning

## Sign-off

Status on 2026-05-11:

- [x] verified that `.bin` is matcher fallback behavior, not a lane B yEnc regression
- [x] verified that “no release candidates found” was a true empty active queue at that moment
- [x] implemented early weak-single cooldown
- [x] implemented coarse contextual multi-binary `prefer_base_stem` deferral
- [x] implemented `base_stem` handoff wiring so coarse family deferral can immediately requeue matching `base_stem` summaries
- [x] widened upstream archive-stem grouping for larger `.partNN.rar` / `.volNN+MM.par2` style families
- [x] verified structured obfuscated families are being persisted correctly and are mainly failing due to coarse contextual grouping
- [x] tightened `prefer_base_stem` to require repeated stems and added early `overgrouped_contextual` cooldown for unique-stem matrix families
- [x] verified with live `STAT`, `XOVER`, `HEAD`, and `BODY` that yEnc BODY headers contain real file names missing from XOVER/HEAD
- [x] updated `cmd/articleprobe` so it can use runtime SQLite NNTP settings and disable ARTICLE output during metadata probes
- [x] implemented separate `recover_yenc` stage for weak/overgrouped contextual binaries
- [x] added prefix-only NNTP BODY fetch for yEnc header inspection without draining full articles
- [x] added yEnc recovery store path that re-keys or merges recovered binaries and refreshes release-family summaries
- [x] added runtime/UI settings and CLI wiring for `gonzb indexer recover-yenc --once`
- [x] added release/service and repository test coverage for the new cooldown paths
- [x] added yEnc recovery service tests for successful prefix recovery and not-found backoff
- [x] added matcher coverage for large-family grouping behavior
- [x] added numeric opaque subject guards so extensionless `80791181-n` style groups do not form releases
- [x] verified live assemble, recover-yEnc, release cycles after the guard
- [x] inspected release/binary edge cases and confirmed huge files are represented as high-part-count binaries, not one-article binaries
- [x] validated on live Postgres family-shape queries, targeted release selector heuristics, live NNTP metadata probes, targeted Go tests, and UI production build

Remaining active focus:

- continue improving upstream usable identity evidence for contextual obfuscated groups
- decide whether the dev database should be reset after the current code path is stable, since old release rows still include historical weak formations
