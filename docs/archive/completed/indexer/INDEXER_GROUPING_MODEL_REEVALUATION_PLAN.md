# Indexer Grouping Model Re-Evaluation Plan

Snapshot date: 2026-05-12

This is the active plan for re-evaluating how article headers become binaries, how binaries become release-family candidates, and how release formation should use obfuscated Usenet signals.

## Current Finding

Yes, the grouping model needs another pass.

The current pipeline captures useful signals, but release-family identity still falls back too easily to weak title-derived or broad contextual keys. On a clean database, that allowed small opaque posts to look `100%` complete even though they had no usable archive/media/PAR/yEnc-recovered identity.

## Current Assembly Model

Header ingestion persists:

- raw subject
- poster and poster id
- xref/newsgroup context
- quoted subject filename when present
- subject file index and file total
- yEnc part number and yEnc total parts
- yEnc file size when present

Assemble then:

- claims article headers by lane
- hydrates the parsed metadata into match candidates
- runs the matcher
- upserts one `binaries` row per `binary_key`
- upserts one `binary_parts` row per article/header part
- refreshes binary stats and release-family readiness summaries

Current useful behavior:

- yEnc subject file index/count is preserved as `file_index` and `expected_file_count`
- `expected_file_count` is the poster/NZB file-set denominator, such as `[01/10]`; it can include PAR2, SFV, NFO, SRR, and other sidecars when the poster includes them in the subject counter
- `expected_archive_file_count` is the archive/protected-payload denominator from stronger archive/PAR2 evidence; it is separate because a split archive can be `20` archive parts inside a `21` file release that also contains an SFV
- yEnc article part/count is preserved as `part_number` and `total_parts`
- Lane A prioritizes headers that match existing incomplete binaries by filename
- Lane B drains recent unmatched backlog
- recovered yEnc names can re-key or merge binaries after prefix BODY inspection

## Current Weaknesses

### Weak readable-title promotion

Subjects like:

- `80894690-n-YuO [1/4] - "opaque-token" yEnc (1/1)`

can produce a source title like:

- `80894690 n YuO`

That is not a release title. It is a numeric post bucket plus a short obfuscated suffix.

Current mitigation:

- these titles are now blocked during release persistence and candidate selection

Long-term fix:

- classify them as weak obfuscated sequence keys during matching, not as `readable_title`

### Contextual key is too broad and too arbitrary

For opaque posts without real filenames, current contextual release-family keys are built from:

- poster
- newsgroup/xref
- posting window
- article bucket
- expected file count

This is useful as a fallback, but it can still group weak candidates that only share coarse context.

Long-term fix:

- contextual keys should become provisional queues, not release-family identities
- release formation should prefer stronger keys when file index/count and repeated obfuscated subject tokens exist

### Expected file count is being trusted too early

`expected_file_count = 1` currently means the binary is complete, but it does not mean it is a valid release.

Long-term rule:

- expected count only proves coverage
- release identity still requires usable evidence:
  - archive/media/PAR filename
  - yEnc-recovered filename
  - strong repeated obfuscated release token
  - post-release inspect/enrichment evidence

## Target Grouping Model

### Level 1. Article part identity

Purpose:

- decide which article belongs to which binary/file

Inputs:

- yEnc part number and total parts
- quoted yEnc filename or recovered yEnc filename
- subject file name
- message id/article number as tie-breakers

Desired key:

- provider
- newsgroup
- file identity
- yEnc total parts

If a filename is missing or opaque:

- still assemble the binary
- mark identity as weak/provisional
- do not promote to releasable without more evidence

### Level 2. File set identity

Purpose:

- decide which binaries/files belong to the same release candidate

Inputs:

- normalized obfuscated subject prefix before `[file/total]`
- file index and file total
- poster
- newsgroup
- tight posting window
- yEnc total parts / size shape
- recovered filename or archive base stem when available

Desired key priority:

1. recovered yEnc filename archive/media/PAR stem
2. explicit archive/media/PAR stem from XOVER subject
3. repeated obfuscated subject set token plus expected file count
4. contextual fallback only as a provisional queue

Important:

- the same obfuscated title is often the strongest release-level signal
- the file index/count should be used to prove set coverage
- article part/count should prove binary coverage, not release identity by itself

### Level 3. Release identity

Purpose:

- decide whether a grouped file set is worth exposing as a release

Inputs:

- file-set coverage
- binary part completion
- usable filename/title evidence
- recovered yEnc evidence
- archive/media/PAR relationship

Required for formation:

- complete enough files for expected count
- at least one usable identity signal
- no numeric-only/generated source title as the sole title

## Proposed Code Changes

### Phase 1. Add subject set-token extraction

Add matcher logic that extracts a stable release-set token from subjects before the file counter and quoted filename.

Examples:

- `80894690-n-YuO [1/4] - "opaque" yEnc (1/1)`
  - set token: `80894690 n yuo`
  - token class: `numeric_obfuscated_set`
- `Some.Release.Name [01/86] - "file.part01.rar" yEnc (1/60)`
  - set token: `some release name`
  - token class: `readable_title`

The token class matters:

- `readable_title` can become a release-family key
- `numeric_obfuscated_set` should be a provisional file-set key, not a final title

### Phase 2. Split matcher outputs

Make the matcher explicitly emit:

- `source_release_key`
  - trace/debug grouping from the original source shape
- `release_family_key`
  - strongest releasable family key known now
- `file_set_key`
  - stronger provisional set key from obfuscated title plus expected file count
- `file_family_key`
  - specific file/binary key
- `identity_strength`
  - `strong`, `probable`, `weak`, `provisional`
- `identity_reason`
  - e.g. `archive_stem`, `media_filename`, `numeric_obfuscated_set`, `contextual_fallback`, `yenc_recovered`

This may require schema changes. Pre-alpha status makes that acceptable.

### Phase 3. Prefer file-set grouping before release formation

Release candidate selection should prioritize:

1. strong `release_family_key`
2. complete `file_set_key` with expected file count coverage and usable identity
3. recovered-yEnc promoted families
4. provisional contextual groups only for recovery/enrichment, not release formation

### Phase 4. Make yEnc recovery a promotion path

`recover_yenc` should continue to be separate from assemble.

Use it to:

- recover real filenames from weak/provisional groups
- re-key binaries into archive/media/PAR stems when possible
- mark unrecoverable weak groups with backoff
- feed release only after identity improves

### Phase 5. Add release-readiness buckets for identity strength

Add or derive buckets such as:

- `actionable_strong`
- `actionable_file_set`
- `needs_yenc_recovery`
- `weak_obfuscated_set`
- `weak_single_binary`
- `fragment_only`
- `stale_cleanup_only`

Release should mainly process:

- `actionable_strong`
- `actionable_file_set`

Recovery/enrichment should process:

- `needs_yenc_recovery`
- selected `weak_obfuscated_set`

### Phase 6. Add configurable content filters

Some complete file sets are valid Usenet posts but not useful indexer releases. The current example is small `rclone crypt` payloads whose file magic starts with `RCLONE`.

Add settings-driven filtering after enough payload evidence exists:

- minimum and maximum binary/file size rules
- blocked magic-byte signatures such as `52 43 4C 4F 4E 45` for `RCLONE`
- optional blocked extension or recovered filename patterns
- binary-level quarantine/blacklist status so release, inspect, and download stages skip repeat work

Important:

- header-only assemble should stay fast and should not fetch bodies just to filter
- yEnc recovery or inspect can attach magic/size evidence later
- filtered binaries should remain auditable so false positives can be reversed during pre-alpha testing

## Validation Plan

Use a clean database and measure after each phase:

- formed releases count
- formed releases with usable file names
- formed releases with `classification != misc`
- releases formed only from source titles
- releases with extensionless files
- yEnc recovery recovered/merged/not-found rates
- percent of pending candidates in each readiness bucket

## Live Validation Notes

Validated on 2026-05-12 after migration `018_binary_grouping_identity_fields` was applied.

Commands:

- `go test -count=1 ./...`
- `go run ./cmd/gonzb --config config.yaml indexer maintenance repair-runtime`
- `go run ./cmd/gonzb --config config.yaml indexer assemble lane-b --once`
- `go run ./cmd/gonzb --config config.yaml indexer assemble lane-a --once`
- `go run ./cmd/gonzb --config config.yaml indexer release --once`

Results:

- schema version reached `18`
- Lane B assembled four 2,500-header batches successfully
- Lane B throughput ranged from about `302` to `514` headers/sec
- Lane B refreshed `858`, `902`, `947`, and `957` binaries per batch
- Lane A assembled four priority batches around `2,116` to `2,492` headers/sec
- Lane A had high cache-hit behavior, refreshing only `17` to `73` binaries per batch
- release processed `10,000` candidate families and formed `0`
- release cooled down `4,312` fragment-only, `5,292` weak-single, and `5` low-coverage families
- release skipped `788` contextual weak fragment candidates

Recent matcher identity distribution after the live assemble smoke run:

- `opaque_set / provisional / opaque_subject_set`: `3,187` recent binaries
- `contextual_obfuscated / weak / contextual_fallback`: `195` recent binaries
- `readable_title / probable / readable_title`: `122` recent binaries
- `readable_title / strong / readable_archive_filename`: `52` recent binaries
- `archive_stem / strong / archive_stem`: `13` recent binaries

Interpretation:

- assemble is working, but the indexed groups are much larger than the earlier `wood` baseline
- `misc` and `boneless` have far more pending headers than assembled headers, so release formation is starved by backlog depth
- most currently assembled binaries in these groups are weak/provisional opaque identities, so release correctly avoids forming public releases from them
- yEnc recovery smoke test reached NNTP fetch work and was manually stopped after it exceeded a quick-test window without progress output; this stage needs bounded progress logging and per-candidate timeout metrics before it is useful for repeated live tuning

## Database Growth Notes

The large database size is caused mostly by header retention, not releases.

Current approximate table sizes:

- `article_header_ingest_payloads`: `22 GB`
- `article_headers`: `17 GB`
- `binaries`: about `3.3 GB`
- `binary_grouping_evidence`: about `3.2 GB`
- `binary_parts`: about `2.7 GB`

Current approximate row counts:

- `article_headers`: about `35.1M`
- `article_header_ingest_payloads`: about `34.8M`
- `binary_parts`: about `10.0M`
- `binaries`: about `2.4M`
- `release_family_readiness_summaries`: about `1.9M`

Newsgroup backlog:

- `alt.binaries.misc`: `22.1M` headers, `6.9M` assembled, `15.3M` pending
- `alt.binaries.boneless`: `10.0M` headers, `3.1M` assembled, `6.9M` pending
- `alt.binaries.wood`: `5.0M` headers, `0.96M` assembled, `4.1M` pending

`article_headers` and `article_header_ingest_payloads` are not exact duplicates:

- `article_headers` holds durable identity, article numbers, message ids, dates, sizes, and assembly claim state
- `article_header_ingest_payloads` holds subject/poster/xref, parsed file/yEnc fields, yEnc recovery backoff fields, and raw overview JSON

However, `raw_overview_json` currently stores the full raw XOVER line and repeats data already persisted in typed columns. A sample estimated:

- average `raw_overview_json`: about `296` bytes/row
- estimated raw overview storage: about `10 GB`
- average subject: about `56` bytes/row
- average xref: about `70` bytes/row

Recommended storage next step:

- stop retaining full raw XOVER lines by default once typed fields are persisted
- keep a runtime/dev setting for raw overview retention when debugging scrapers
- add a maintenance migration/job to null or compact old `raw_overview_json`
- consider dropping or narrowing `idx_article_header_ingest_payloads_structured_name` if Lane A no longer needs such a broad expression index

Implemented storage change:

- new NNTP XOVER parses no longer add `raw_overview_json.line`
- `InsertArticleHeaders` defensively strips any incoming raw overview `line` key before storage
- `references` is retained when present because it is not otherwise persisted in typed columns
- existing rows are not rewritten by migration to avoid a huge table rewrite on the hot `article_header_ingest_payloads` table

Validation:

- `go test -count=1 ./internal/nntp ./internal/store/pgindex ./internal/indexing/scrape`
- `go test -count=1 ./...`

## PAR2 Regrouping Finding

Investigated `270b97918a05822e6f5f6c0d731919ef.par2` after a manual PAR2 read showed target files under `95da44375475e6adf5dc90acff76194d.partNN.rar`.

Current DB state:

- binary `847137` exists in `alt.binaries.misc`
- file `270b97918a05822e6f5f6c0d731919ef` is assembled as a one-part contextual binary
- release `3DclHCNZFPNP0uB5rCWCvmvwlq1` already formed from that single tiny PAR2-like binary before the stronger gates were added
- no binaries currently exist for `95da44375475e6adf5dc90acff76194d`
- nearby article numbers are heavily interleaved with many unrelated posters, singleton opaque files, and unrelated multi-part archive families
- another nearby PAR2 stem, `e12a2861d43332c8a9829c964016c5b6a2a52f14`, is assembled as fragment-only with `10/29` files and `0%` complete main payload coverage
- nearby archive family `f01696c18b7240f1904a50450ef935e7` is assembled as an archive stem with `17/35` binaries and `42.86%` expected file coverage

Interpretation:

- the articles around this area are being retrieved and assembled
- the target RAR names discovered inside PAR2 are not visible in XOVER subjects for the checked window
- current `inspect_par2` records PAR2 presence/signature but does not parse PAR2 packets for recoverable target filenames
- without parsing PAR2 target-file packets, release formation cannot use the PAR2 as proof that a tiny opaque PAR2 belongs to a different archive stem

Recommended next implementation:

- extend `inspect_par2` to parse PAR2 main/file-description packets and persist target filenames
- add a `binary_par2_targets` table or extend PAR2 metadata to store recoverable filenames, file sizes, and hashes
- use PAR2 targets to promote/re-key weak single PAR2 binaries into the target archive family when matching binaries exist or later arrive
- mark old single-file PAR2 releases stale when their PAR2 target graph proves they are only repair metadata for a larger release

Implemented Phase 6 / PAR2 inspection work:

- `inspect_par2` now samples enough prefix bytes to parse PAR2 file-description packets and persists target filenames/sizes in `binary_par2_targets`
- PAR2 inspection summaries include `target_count` and target metadata so tiny PAR2-only releases can be audited against the recoverable file graph
- `inspect_par2` now filters parsed targets into protected targets; `.sfv`, `.nfo`, and `.srr` count when PAR2 lists them, while `.par2` files are excluded because they describe/repair the protected set
- when all archive PAR2 targets share one archive stem, the PAR2 binary is re-keyed as auxiliary evidence for that target family
- matching binaries with recovered yEnc filenames now receive `expected_archive_file_count` from the PAR2 archive target count and inferred `file_index` values from target names such as `.part02.rar`, `.r00`, `.7z.003`, and `.zip.005`
- `inspect_par2` can now inspect assembled PAR2 binaries before release formation; PAR2 binaries no longer need to become tiny solo releases before they can provide archive denominator evidence
- release-family summaries are refreshed immediately after PAR2 coverage is applied, so release selection can see the stronger denominator without waiting for another assemble pass
- expected-file coverage uses complete binaries against the expected count, while actionable readiness still requires at least one complete main-payload binary
- runtime inspect settings now include `min_binary_bytes`, `max_binary_bytes`, and `blocked_magic_hex`
- default blocked magic includes `52434C4F4E45` for `RCLONE`
- inspect discovery marks matching completed opaque binaries as `content_filtered` with reason/rule metadata
- later inspect stages skip binaries already marked content-filtered by inspect discovery
- the Runtime Settings UI explains the size/magic filters and exposes them alongside inspection tool settings

Validation:

- `go test -count=1 ./...`
- `go test -count=1 ./internal/indexing/inspect ./internal/indexing/inspect/discovery ./internal/indexing/inspect/par2 ./internal/store/pgindex ./internal/app ./internal/infra/config ./internal/settingsadmin ./internal/runtime/wiring`
- `npm --prefix ui run build`

Remaining follow-up:

- use stored PAR2 target graphs opportunistically when matching target binaries arrive after the PAR2 was already inspected
- add an operator-facing audit/undo path before turning content filters into a broader release/download blacklist
- run live inspect PAR2 passes against the fresh database once enough formed PAR2 releases exist

## Weak Single Binary Recovery Finding

`weak_single_binary` should not be read as junk. It means the XOVER/header-only view did not expose a trustworthy release/file identity.

A recent weak-single sample showed:

- nearly all sampled weak singles were one observed article part
- most were small XOVER-visible article chunks around `740 KB`
- posters were often randomized, making same-poster grouping weak for this population
- BODY-prefix probing recovered real yEnc names from otherwise opaque subjects

Manual BODY-prefix checks proved that at least some weak singles are real multipart archive segments:

- binary `6586203` recovered `Wbostp9Yf138Oybk1yc93o.part02.rar`, yEnc part `2/62`, file size `44040192`
- binary `6584898` recovered `Wbostp9Yf138Oybk1yc93o.part03.rar`, yEnc part `28/62`, file size `44040192`

Interpretation:

- XOVER can hide the true file name even when the article is actionable
- yEnc `part/total` is article-segment coverage for one file, not release file-count coverage
- archive volume names recovered from yEnc headers are stronger grouping evidence than subject/title/extension
- release formation should not form these as single-file releases, but recovery should promote them into archive-stem families when enough evidence arrives

Implemented recovery throughput change:

- `recover_yenc` now honors runtime `concurrency`
- the stage logs progress every 100 attempted candidates and at batch completion
- final metrics include concurrency so live tuning can distinguish NNTP latency from merge/re-key cost

Next data-driven discovery work:

- infer archive-volume file index from recovered yEnc names such as `.part02.rar`
- add binary-level probe/audit support for magic bytes, ASCII ratio, printable strings, and pointer-like text evidence
- detect pointer candidates by data patterns such as message IDs, NZB/XML fragments, URLs, JSON-like metadata, or unusually text-heavy payloads
- keep these probes out of assemble/release hot paths unless they produce durable grouping or filtering evidence

Side-note plan:

- add binary-level evidence rows for sampled payload signatures instead of relying on filename/title/extension
- record magic bytes, ASCII ratio, printable string samples, pointer-pattern hits, and sampled-byte offsets
- treat pointer-like payloads as discovery evidence only until a follow-up stage proves the referenced articles/files exist
- infer archive volume indexes from recovered yEnc filenames such as `.part02.rar`, `.r04`, `.7z.003`, and `.zip.005`
- use inferred archive volume indexes as release/file-set coverage evidence, not as article part coverage

## Recovery Throughput Finding

`recover_yenc` is slow because BODY-prefix recovery intentionally does not drain the full article body.

Current behavior:

- `recover_yenc` calls `FetchBodyPrefix`
- `FetchBodyPrefix` issues `BODY <message-id>` and reads only the configured prefix bytes
- because the NNTP dot stream still has unread article bytes, the provider discards that connection instead of returning it to the pool
- each candidate can therefore pay a fresh dial/TLS/auth cost

Implications:

- increasing `recover_yenc.concurrency` helps only until provider/server connection setup becomes the bottleneck
- scrape uses the same provider instance in the current indexer runtime, so XOVER scrape and BODY-prefix recovery can compete when supervised together
- there is no global NNTP work queue with stage fairness today; the provider has an idle connection pool, not a priority scheduler
- there is no standard XOVER field that contains the yEnc `=ybegin name=...` body header, so recovered filenames usually require BODY/ARTICLE data

Header-only improvement:

- some Message-IDs include article counters such as `<Part84of700...>`
- these counters are available from XOVER and can improve part coverage without BODY fetches
- matcher should use Message-ID `PartNofM` as article part evidence when subject yEnc counters are missing or weaker
- same date, close article numbers, similar bytes/lines, and similar poster domain/prefix are useful prioritization signals, but not strong enough to form release/file identity alone

Open throughput options:

- reserve/dedicate indexer NNTP capacity for recovery instead of letting scrape occupy all useful transport time
- add a stage-aware NNTP scheduler with separate scrape/recovery queues and per-stage concurrency caps
- prioritize recovery candidates with header evidence that suggests real multipart content
- keep yEnc BODY-prefix recovery outside assemble unless a tiny bounded hot-path sample proves worth the latency
- investigate provider-specific commands, but assume portable NNTP has no partial BODY range request

Footer/range notes:

- `=ypart begin/end` is in the cheap BODY prefix and can provide per-article byte range evidence
- `=yend size/pcrc32/crc32` is at the end of the article body, so capturing it requires reading/draining the full BODY
- footer CRC is useful for verification/dedupe/probe work, but should not be required for normal grouping because it is too expensive at recovery scale
- downloader decode already reads yEnc footers for checksum verification during actual downloads; recovery currently only needs the prefix identity evidence

Stage contract:

- assemble stays fast and header-only
- assemble may create weak/provisional binaries when XOVER is insufficient, but it should not form release-quality identity from weak context alone
- `recover_yenc` is the promotion stage for weak/provisional binaries that need BODY-prefix identity evidence
- release should ignore/cool down weak/provisional families and split-archive families without expected file-count evidence
- inspect stages can add richer evidence later; if that evidence improves grouping, affected families are requeued instead of blocking assemble

Archive volume inference:

- recovered yEnc names such as `.part02.rar`, `.r04`, `.7z.003`, and `.zip.005` should populate `file_index`
- inferred volume indexes are file-set coverage evidence only; they are not an expected file-count denominator by themselves
- release should not form split-archive sets with only inferred indexes and no expected file count, because those are likely partial snapshots

Unknown denominator completion model:

- recovered yEnc identity gives the file name, file size, article part number, article total, and article byte range for one split volume
- that is enough to know whether a single split volume is article-complete
- it is not enough to know whether the whole archive/release is file-complete
- observed highest archive volume index is only a lower bound, not the expected file count
- release formation should treat archive sets with unknown expected count as `needs_more_evidence`, not complete

Ways to discover the missing denominator:

- subject/XOVER file counters when visible, such as `[01/86]`
- PAR2 target file-description packets, when PAR2 is available and parsed; this is the strongest denominator because it lists protected target filenames directly
- RAR/ZIP archive metadata from a representative first volume, when enough leading bytes are available
- 7z start header from `.7z.001`: the first 32 bytes include the next-header offset/size, which can imply the logical archive range
- for split 7z, if the first volume yEnc file size is known, `ceil(next_header_end / first_volume_size)` can infer the highest required split volume

Implications:

- `recover_yenc` should promote grouping and per-volume article completion, but should not invent `expected_file_count`
- `inspect_par2` can provide `expected_archive_file_count` when target file-description packets expose a protected split archive/media file set
- a future lightweight `inspect_archive_head` or archive discovery pass can probe only representative first volumes and persist inferred expected file count when reliable
- sparse archive inspection can work once the required first/last ranges are present, but it should not guess the last part without header/PAR2 proof
- RAR is more forgiving because first-volume metadata often provides useful archive entries earlier; 7z often needs the encoded next header near the logical end of the concatenated archive
- passworded archives should still record archive identity, password-required state, and available entry hints; unknown password is not a grouping failure

Binary evidence stage placement:

- binary-level magic/ASCII/string/pointer probing belongs in inspect/discovery-style stages, not assemble
- discovery should persist durable evidence rows or metadata that later stages can use without re-fetching
- pointer-like text files are currently speculative; detect and audit them, but do not make them release-forming until references are proven
- passworded archives should stay in inspect/archive and inspect/password; unknown-password results should be persisted as release metadata and kept searchable/auditable without blocking grouping

Good outcome:

- small opaque `misc` releases stop forming
- complete multi-file obfuscated sets are retained as provisional candidates
- releases form only after they have a real filename/title signal or a strong file-set identity

## Acceptance Criteria

- `80894690-n-YuO` style posts are never `readable_title`
- opaque one-file posts do not form releases solely from `expected_file_count=1`
- multi-file obfuscated sets use subject set token plus file count as a grouping signal
- yEnc part count remains binary coverage evidence
- yEnc file count remains release/NZB-file-set coverage evidence
- PAR2 target count remains archive/protected-payload coverage evidence
- release family summaries expose identity strength and readiness clearly enough for UI/runtime tuning

## Sign-off

Status on 2026-05-12:

- [x] confirmed current code persists file index/count and yEnc part/count
- [x] confirmed current release formation still needed stronger identity gates
- [x] blocked numeric generated titles and opaque contextual standalone releases
- [x] implement explicit subject set-token extraction
- [x] split provisional file-set identity from releasable release-family identity
- [x] add readiness buckets for identity strength
- [x] document configurable size/magic filtering as the next filter layer
- [x] implement runtime size/magic content filters for inspect discovery
- [x] implement PAR2 target filename persistence for `inspect_par2`
- [x] prove weak-single binaries can be real multipart archive segments via yEnc BODY-prefix recovery
- [x] make `recover_yenc` concurrency-driven and observable enough for live tuning
- [x] document binary-level probe side-note plan and recovery transport bottleneck
- [x] use Message-ID `PartNofM` counters as header-only article part evidence
- [x] infer archive volume indexes from recovered yEnc/archive filenames
- [x] keep release conservative for split archives without expected file-count evidence
- [x] apply parsed PAR2 target graphs as expected archive-file-count evidence for matching archive families
- [x] split poster/NZB file-set count from archive/protected-payload expected count
- [ ] validate on a clean database with live assemble/recover/release cycles

## Implementation Sign-off

### Level 1. Article Part Identity

Status: signed off for this slice.

- matcher still uses yEnc part/total as binary coverage evidence
- structured XOVER/yEnc fields still override ambiguous subject counters
- no BODY fetch was added to assemble

Validation:

- `go test -count=1 ./internal/indexing/match ./internal/indexing/assemble`

### Level 2. File Set Identity

Status: signed off for this slice.

- matcher now extracts a `subject_set_token` before the file counter and quoted filename
- `80894690-n-YuO [1/4] - "opaque" yEnc (1/1)` becomes `subject_set_token=80894690 n yuo`
- numeric/generated prefixes are classified as `numeric_obfuscated_set`, not `readable_title`
- matcher now emits and persists `file_set_key`, `identity_strength`, `identity_reason`, `subject_set_token`, and `subject_set_kind`

Validation:

- `go test -count=1 ./internal/indexing/match ./internal/indexing/assemble ./internal/indexing/yencrecover ./internal/store/pgindex`

### Level 3. Release Identity

Status: signed off for this slice.

- release summaries now bucket numeric/opaque subject-set families as `weak_obfuscated_set`
- release formation cools those families down instead of forming releases from the prefix alone
- yEnc recovery can select `weak_obfuscated_set` candidates for promotion work

Validation:

- `go test -count=1 ./internal/indexing/release ./internal/store/pgindex`

## Clean-Database And Overnight Validation

Status on 2026-05-14: signed off.

Clean-database and overnight supervisor validation showed the grouping changes are landing materially better evidence instead of only producing weak opaque backlog.

Key live results:

- `releases`: `952`
- `releases.expected_archive_file_count > 0`: `702`
- `binaries.expected_archive_file_count > 0`: `13,481`
- non-`.bin` named binaries: `230,679`
- actionable families: `4,608`
- actionable `archive_stem` families: `2,626`
- actionable `contextual_obfuscated` families: `1,919`
- families with PAR2-backed archive-count evidence:
  - `archive_stem`: `2,015`
  - `contextual_obfuscated`: `1,233`

Important interpretation:

- yEnc recovery and PAR2 target persistence made a positive difference
- stronger file names, archive stems, and archive/protected-file denominators are reaching both `binaries` and `releases`
- release formation is no longer the primary blocker for this sprint
- the main operational problem has shifted to storage growth and retention

Known remaining noise:

- very large `weak_single_binary` and `fragment_only` populations still exist
- large contextual/test families such as `anonymous anon nowhere invalid ... alt binaries test ...` still pollute backlog summaries
- this is now primarily a retention/cleanup problem, not proof that the grouping work failed

## Final Sign-Off

Signed off on 2026-05-14.

Disposition:

- archive this plan as completed work
- keep the landed yEnc/PAR2/release improvements
- move the active execution focus to database growth trimming and retention reduction
