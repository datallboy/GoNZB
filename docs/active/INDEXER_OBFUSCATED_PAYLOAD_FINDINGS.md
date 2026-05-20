# Indexer Obfuscated Payload Findings

Snapshot date: 2026-05-19

This doc records technical findings from an externally generated payload that exposed two useful indexer/downloader lessons:

- downloader hydration should tolerate legacy XML character-set declarations
- indexer grouping should be able to promote strong recovered file identity across weak or intentionally varied header identity

This doc intentionally excludes source-identifying details. Do not add payload titles, release names, subject text, poster values, newsgroup names, message IDs, or content descriptions here.

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

## Validation Notes

Use non-sensitive synthetic fixtures for automated tests:

- one legacy-encoded NZB parser fixture
- one multi-group obfuscated header fixture with randomized posters and subjects
- one recovered-yEnc grouping fixture showing same recovered base identity across multiple newsgroups

Avoid adding the observed external payload as a committed fixture.
