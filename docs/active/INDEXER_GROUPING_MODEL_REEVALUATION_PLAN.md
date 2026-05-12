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

- yEnc file index/count is preserved as `file_index` and `expected_file_count`
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

Good outcome:

- small opaque `misc` releases stop forming
- complete multi-file obfuscated sets are retained as provisional candidates
- releases form only after they have a real filename/title signal or a strong file-set identity

## Acceptance Criteria

- `80894690-n-YuO` style posts are never `readable_title`
- opaque one-file posts do not form releases solely from `expected_file_count=1`
- multi-file obfuscated sets use subject set token plus file count as a grouping signal
- yEnc part count remains binary coverage evidence
- yEnc file count remains release/file-set coverage evidence
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
