# Indexer Query And Runtime Cleanup Plan

Snapshot date: 2026-04-20

This doc is the subsystem guide for hot query cleanup and recurring maintenance.

## Assembly Query Redesign

### Current issue

- the current query scans a large amount of `article_headers`
- it anti-joins `binary_parts`
- it joins `article_poster_map` and `posters`
- it takes about `26.46s` for `5,000` rows on the dev DB

### New behavior

- pending work is represented by `article_headers.assembled_at IS NULL`
- `ListUnassembledArticleHeaders` reads from:
  - `article_headers`
  - `article_header_ingest_payloads`
- it orders by newest-first `id DESC`
- it does not anti-join `binary_parts`
- it does not join `article_poster_map`
- it does not join `posters`

### Validation target

- `EXPLAIN (ANALYZE, BUFFERS)` shows a pending-index-driven scan
- `5,000` pending rows should return in under `1s`

## Release Dirty-Family Queue

### Current issue

- normal `ListReleaseCandidates` does a full-table binary regrouping pass
- this costs about `5.13s` on the dev DB

### New behavior

- `release_stage_dirty_families` tracks changed release work
- `UpsertBinary` enqueues:
  - `release_family` key
  - `base_stem` key when `expected_file_count > 1` and `base_stem <> ''`
- normal scheduled release work reads only queued families
- reform mode stays separate and can still scan existing releases when explicitly requested

### Safety rule

- queued family rows are acknowledged only after:
  - release persistence succeeds
  - release files are replaced
  - stale releases for that family are deleted

### Validation target

- normal release candidate work is proportional to changed binaries
- `EXPLAIN (ANALYZE, BUFFERS)` for scheduled release work should avoid the previous full-table window aggregate

## Remaining Release/Inspect Stabilization Work

The hot-query cleanup is in place, but query/runtime stabilization is not fully complete until release inspection can see more of the obfuscated catalog.

Current measured symptom on the dev DB:

- `548` releases at `completion_pct >= 100`
- only `49` archive-like `release_files`
- `0` direct media-like `release_files`
- `37,809` `release_files` ending with `.bin`

That means the current candidate query logic is fast, but too many complete releases still fail candidate selection for archive/media inspection.

## Obfuscated Multipart Assembly Repair

### Confirmed failure mode

Current dev DB analysis confirmed a more important issue than filename recovery alone:

- `release_id=3CdNsxCiPeAHoEMGeTcqdXPT71L` appeared as `99` standalone `.bin` files
- direct article inspection showed those rows were actually `99` different yEnc parts of one file
- shared yEnc metadata:
  - `name=kuqn1sj0tdehymt5l4ba7u`
  - `total=807`
  - per-row `part=` varied across the release

So the system was not just failing inspect discovery. It was assembling multipart obfuscated content into fake standalone binaries.

### Required behavior

- treat yEnc header metadata as authoritative fallback identity when subject/XOVER markers are opaque
- use square-bracket counters as release file-count truth when present:
  - `[13/15]` means file `13` of `15` in the release
- use yEnc counters as file article-count truth:
  - `yEnc (113/220)` or yEnc header `part=113 total=220` means article `113` of `220` for one file
- never allow those article counters to be mistaken for release file counts

### Implementation direction

- low-confidence opaque assembly candidates should fetch the article body header and re-run matching with injected structured metadata:
  - `name`
  - `part`
  - `total`
  - `size`
- the rematch should produce:
  - stable `binary_key` by yEnc file name
  - stable `part_number` and `total_parts`
  - contextual `release_family_key` when titles remain obfuscated
- release formation should then see one incomplete multipart binary instead of many fake single-part binaries
- release formation must also stay conservative once binaries exist:
  - `[1/1]` is explicit standalone evidence
  - readable direct-media standalone binaries may persist without a file counter when confidence is strong
  - opaque `.bin` or archive-like single binaries without explicit single-file evidence must not become releases

## Inspect Candidate Discovery For Obfuscated Posts

### Current issue

`inspect_archive` and `inspect_media` are currently gated mostly by file-name shape:

- archive extensions:
  - `.7z`
  - `.7z.001`
  - `.zip`
  - `.zip.001`
  - `.rar`
  - `.part01.rar`
  - `.part1.rar`
- media extensions:
  - `.mkv`
  - `.mp4`
  - `.avi`
  - `.ts`
  - `.flac`
  - `.mp3`
  - `.m4a`

That works well for transparent posts, but not for opaque `.bin` release files.

### Target behavior

Add a pre-inspection discovery path so complete releases can become inspect candidates even when file names are opaque.

Implemented design:

1. add `inspect_discovery` before archive/media inspection
2. store per-binary recovered kind and recovered extension with confidence on `binaries`
3. canonicalize opaque file names back onto `binaries`, `release_files`, and `binary_parts`
4. let downstream archive/media stages consume those recovered names through their normal filename filters

Recovered fields:

- `recovered_kind`
- `recovered_extension`
- `recovered_confidence`
- `recovered_source`
- `recovered_at`

### Evidence sources to consider

- subject-derived container hints
- existing matcher `base_stem` and `family_kind`
- file-name patterns from `binary_parts`
- byte-signature sampling from first decoded bytes
- archive header probe for opaque files
- ffprobe success on opaque-but-media-like payloads

Current implementation:

- discovery chooses one representative row per opaque complete release
- it then scans opaque files within that release until it finds a recoverable signature or exhausts the release sample budget
- successful archive recovery can rename coherent sibling opaque files into `.7z.001`, `.zip.001`, or `.part01.rar` style families when the size pattern looks like a split archive set

### Runtime rule

Keep inspection expensive work out of normal release formation.

Instead:

- release formation can stay metadata-only and fast
- a dedicated discovery path can classify opaque complete files
- archive/media inspect stages can consume that recovered classification

## Release File Name Recovery

### Current issue

`pickFileName(...)` and matcher fallbacks append `.bin` when a real extension is unavailable.

That is valid as a fallback, but it is currently overrepresented in complete releases.

### Target behavior

Prefer recovered extension or recovered canonical file name when confidence is high enough, before final release-file materialization.

Rules:

- do not rewrite obviously good real file names
- do not replace low-confidence names with speculative guesses
- allow a recovered extension to improve inspect eligibility even if the full canonical name is still unknown

### Practical goal

It is acceptable if we only recover:

- `.7z`
- `.rar`
- `.zip`
- `.mkv`
- `.mp4`
- `.avi`

That alone would materially improve inspect archive/media coverage.

## Maintenance Stage

### Stage

- dedicated stage name: `indexer_maintenance`
- interval: every `6 hours`

### Operations

#### Stage runtime cleanup

- run `RepairIndexerStageRuntime()`
- purge `indexer_stage_runs`:
  - `completed` and `abandoned` older than `14 days`
  - `failed` older than `30 days`

#### Scrape run cleanup

- mark `scrape_runs` as `abandoned` when:
  - `status = 'running'`
  - `finished_at IS NULL`
  - `started_at < NOW() - INTERVAL '1 hour'`
- purge `scrape_runs`:
  - `completed` and `abandoned` older than `14 days`
  - `failed` older than `30 days`

#### Header payload cleanup

- purge `article_header_ingest_payloads` for rows whose parent header:
  - has `assembled_at IS NOT NULL`
  - and `assembled_at < NOW() - INTERVAL '7 days'`

## Optional Post-Cleanup Maintenance

Use only after measurement:
- `VACUUM (ANALYZE)` on affected hot tables after large cleanup or retention runs
- `REINDEX CONCURRENTLY` only when index bloat remains materially large

Do not make routine full reindexing part of the normal maintenance cycle.

## Additional Validation Targets

- count complete releases whose release files currently end in `.bin`
- count complete releases that are archive/media inspect eligible before recovery changes
- count complete releases that become inspect eligible after recovery changes
- verify a representative set of formerly opaque complete releases now produce:
  - `binary_archive_entries`
  - `binary_media_streams`
  - improved `releases.title`
  - improved `releases.title_source`

## Exit Condition For This Doc

This query/runtime track should not be considered complete until both are true:

1. assembly/release hot paths are efficient
2. inspect candidate discovery is no longer blocked primarily by filename opacity on complete releases
