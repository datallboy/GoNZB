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

## Validation Plan

Use a clean database and measure after each phase:

- formed releases count
- formed releases with usable file names
- formed releases with `classification != misc`
- releases formed only from source titles
- releases with extensionless files
- yEnc recovery recovered/merged/not-found rates
- percent of pending candidates in each readiness bucket

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
- [ ] implement explicit subject set-token extraction
- [ ] split provisional file-set identity from releasable release-family identity
- [ ] add readiness buckets for identity strength
- [ ] validate on a clean database with live assemble/recover/release cycles
