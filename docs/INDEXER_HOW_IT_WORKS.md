# Indexer How It Works

This is the technical reference for the current Usenet/NZB indexer pipeline.

It is meant for engineering and operator-level understanding of how the local indexer works. It is not the quick-start or end-user setup guide.

It explains:

1. what each stage does
2. which tables and columns each stage relies on
3. how binaries, files, and releases are formed
4. how inspect and enrich update release metadata
5. how later scraped articles or metadata updates propagate back into the catalog
6. where important implementation constraints still matter

This document reflects the current implementation after the stabilization and migration squash work.

## Pipeline Overview

The indexer is a staged pipeline:

1. `indexer scrape latest`
2. `indexer scrape backfill`
3. `indexer assemble`
4. `indexer release`
5. `indexer inspect *`
6. `indexer enrich *`
7. `indexer_maintenance`

At a high level:

1. raw NNTP overview rows become `article_headers`
2. `article_headers` are matched into binary projection rows
3. binary projections are grouped into `release_files` and `releases`
4. inspect stages read release files and push metadata back onto `releases`
5. enrich stages attach external metadata to `releases`

The important thing to keep in mind is that release formation is not the end of the pipeline. Releases are created early, then improved later by inspection and enrichment.

## Core Data Model

### `article_headers`

`article_headers` is the permanent raw article identity table.

Important columns:

- `provider_id`
- `newsgroup_id`
- `article_number`
- `message_id`
- `date_utc`
- `bytes`
- `lines`
- `scraped_at`
- `assembled_at`

What it means:

- one row is one scraped NNTP article
- it is the long-lived lineage record used later for NZB article emission
- `assembled_at IS NULL` means the header has not been consumed by assembly yet

### `article_header_ingest_payloads`

This is the transient scrape payload side table.

Important columns:

- `article_header_id`
- `subject`
- `poster`
- `xref`
- `created_at`

What it means:

- scrape stores the transient structured subject/poster/xref payload here
- assembly joins this table while a header is still pending
- matcher-side `RawOverview` is reconstructed in memory from structured ingest fields and article facts rather than stored as a JSON column
- old assembled payload rows are eligible for retention cleanup

### Binary Projections

The old `binaries` compatibility table is retired for the clean v0.8 PostgreSQL baseline. Current binary state is split across `binary_core` plus side-table projections.

A binary is the indexer’s current best guess that many raw article segments belong to one posted file.

`binary_core` anchors the file-level identity:

- `provider_id`
- `newsgroup_id`
- `binary_key`

`binary_identity_current` stores the current grouping identity:

- `release_family_key`
- `source_release_key`
- `file_family_key`
- `family_kind`
- `base_stem`
- `release_key`
- `release_name`
- `binary_name`
- `file_name`
- `file_index`
- `expected_file_count`

`binary_observation_stats` stores quality and completeness:

- `total_parts`
- `observed_parts`
- `total_bytes`
- `first_article_number`
- `last_article_number`
- `posted_at`
- `match_confidence`
- `match_status`
- `is_auxiliary`
- `is_main_payload`

`binary_recovery_current` stores recovered identity evidence:

- `recovered_kind`
- `recovered_extension`
- `recovered_source`
- `recovered_confidence`
- `recovered_at`

What it means:

- one binary is usually one candidate posted file
- it may be incomplete
- it may later improve as more parts arrive
- it is the main bridge between raw Usenet data and release formation

### `binary_parts`

`binary_parts` is the part membership table from binary to raw articles.

Important columns:

- `binary_id`
- `article_header_id`
- `message_id`
- `part_number`
- `total_parts`
- `segment_bytes`
- `file_name`

What it means:

- one row is one part of one binary
- this is the exact part map used to later emit release NZB article references

### `release_files`

`release_files` is the catalog/NZB view of files inside a release.

Important columns:

- `release_id`
- `binary_id`
- `file_name`
- `size_bytes`
- `file_index`
- `is_pars`
- `subject`
- `poster`
- `posted_at`

What it means:

- this is the release-facing file inventory
- one binary usually maps to one release file
- `release_files` is what inspect stages usually look at first

### Release Article Lineage

Release article lineage is derived from `release_files` plus `binary_parts`; there is no current `release_file_articles` table in the clean v0.8 PostgreSQL baseline.

Important columns:

- `release_files.binary_id`
- `binary_parts.binary_id`
- `article_header_id`
- `part_number`

What it means:

- this is the exact article list that an NZB emitter can walk through binary lineage
- it is built from `binary_parts` during release formation

### `releases`

`releases` is the final catalog table.

Important identity/title columns:

- `release_id`
- `guid`
- `provider_id`
- `release_family_key`
- `source_release_key`
- `release_key`
- `group_name`
- `title`
- `source_title`
- `deobfuscated_title`
- `matched_media_title`
- `title_source`
- `title_confidence`
- `search_title`

Important completeness/shape columns:

- `file_count`
- `expected_file_count`
- `par_file_count`
- `completion_pct`
- `match_confidence`
- `classification`
- `poster`
- `size_bytes`
- `posted_at`

Important inspect/enrich columns:

- `has_par2`
- `has_nfo`
- `archive_count`
- `video_count`
- `audio_count`
- `sample_present`
- `runtime_seconds`
- `primary_resolution`
- `primary_video_codec`
- `primary_audio_codec`
- `subtitle_languages_json`
- `media_tags_json`
- `encrypted`
- `passworded`
- `password_state`
- `tmdb_id`
- `tvdb_id`
- `metadata_updated_at`

What it means:

- this is the release catalog row exposed to APIs and UI
- release formation creates it early
- inspect and enrich stages continue to improve it later

## Stage By Stage

### `indexer scrape latest`

Purpose:

- scrape near the head of the group
- keep moving forward as new articles are posted

Code path:

- `internal/indexing/scrape/service.go`

Key behavior:

- resolves provider and newsgroup ids
- reads group high/low watermarks from NNTP
- reads `scrape_checkpoints.last_article_number`
- fetches XOVER rows in batches
- writes compact `article_headers`
- writes transient `article_header_ingest_payloads`
- advances the latest checkpoint

Writes:

- `article_headers`
- `article_header_ingest_payloads`
- `scrape_checkpoints`
- `scrape_runs`

Important note:

- scrape does not create binaries or releases directly
- it only creates raw article inventory for later stages

Scrape tuning:

- `batch_size` is the article-number window requested for one group claim. With the default `5000`, one claim fetches up to 5,000 XOVER rows for one group.
- `max_batches` is the maximum number of group claims in one scheduled run. It is not a per-group loop count. Default: `1`.
- `concurrency` is the maximum number of group claims processed in parallel with NNTP XOVER workers. Default: `1`.
- Effective work per scheduled run is roughly `batch_size * max_batches`, bounded by available groups and provider article ranges. `concurrency` changes how many of those group claims run at the same time, not how many total claims are selected.
- Conservative default for both `scrape_latest` and `scrape_backfill`: `batch_size=5000`, `max_batches=1`, `concurrency=1`.
- Raise `max_batches` first when you want a run to touch more groups without adding parallel pressure. Raise `concurrency` only when NNTP and PostgreSQL ingest have clear headroom.

### `indexer scrape backfill`

Purpose:

- walk backward from the known frontier
- fill in older history

Key behavior:

- starts from `backfill_article_number` if present
- otherwise starts just behind the latest frontier
- fetches older XOVER ranges
- writes the same raw header tables as latest mode
- updates `scrape_checkpoints.backfill_article_number`

Backfill cutoff behavior:

- optional `indexing.backfill_until_date_by_group`
- uses XOVER `DateUTC` values, not local DB date scans
- when a batch crosses the cutoff, checkpoint state is persisted:
  - `backfill_until_date`
  - `backfill_cutoff_reached`
  - `backfill_stopped_reason`

This is restart-safe.

### `indexer assemble`

Purpose:

- convert unassembled raw article headers into binary candidates and binary parts

Code path:

- `internal/indexing/assemble/service.go`
- `internal/store/pgindex/assembly_store.go`
- `internal/indexing/match/*`

Input query:

- `ListUnassembledArticleHeaders`
- reads `article_headers`
- joins `article_header_ingest_payloads`
- filters `article_headers.assembled_at IS NULL`

Matcher input from each header:

- `subject`
- `poster`
- `message_id`
- `date_utc`
- `bytes`
- `lines`
- `xref`
- reconstructed `RawOverview` derived from structured ingest fields and article facts

Matcher output drives:

- `release_family_key`
- `source_release_key`
- `file_family_key`
- `family_kind`
- `base_stem`
- `release_name`
- `binary_name`
- `file_name`
- `file_index`
- `expected_file_count`
- `part_number`
- `total_parts`
- `is_auxiliary`
- `is_main_payload`
- `match_confidence`
- `match_status`

Counter semantics matter here:

- `[...]` before `yEnc` is the release file counter
  - `[1/5]` means file `1` of `5` files in the release
  - this is the source of truth for `file_index` and `expected_file_count`
- `yEnc (x/y)` after `yEnc`, or yEnc header `part=x total=y`, is the article counter for one file
  - `yEnc (113/220)` means article `113` of `220` for that one file
  - this is the source of truth for `part_number` and `total_parts`

What assemble writes:

1. `UpsertBinary`
2. `UpsertBinaryPart`
3. `RefreshBinaryStats`

What `UpsertBinary` does:

- inserts or updates one `binary_core` anchor row and its current side-table projections by `(provider_id, newsgroup_id, binary_key)`
- keeps the best/highest-confidence grouping data
- updates `expected_file_count`, `total_parts`, `file_name`, `release_name`, and grouping keys
- enqueues dirty work in `release_stage_dirty_families`

What `UpsertBinaryPart` does:

- inserts or updates one part row in `binary_parts`
- marks that header’s `article_headers.assembled_at`

What `RefreshBinaryStats` does:

- recomputes:
  - `observed_parts`
  - `total_bytes`
  - `first_article_number`
  - `last_article_number`
  - `posted_at`

### Does assemble handle missing articles?

Yes, but only in the sense that binaries can be partial.

Current behavior:

- a binary is created even if not all parts have been observed yet
- `observed_parts` may be less than `total_parts`
- release formation later uses that information to compute `completion_pct`

### If later scrape finds more parts, does assemble reprocess the binary?

Yes, indirectly.

What actually happens:

1. new article headers are scraped
2. those new headers are still `assembled_at IS NULL`
3. assemble processes those new headers
4. `UpsertBinary` updates the existing binary row with the same `binary_key`
5. `UpsertBinaryPart` adds missing parts
6. `RefreshBinaryStats` recalculates completeness
7. `UpsertBinary` also re-enqueues that family in `release_stage_dirty_families`

So assemble is header-driven, not “scan old binaries looking for missing parts.” But the net effect is still that later parts update the same binary.

### How obfuscated multipart posts work now

This is the most important release-formation behavior for real Usenet traffic.

When XOVER subject data is opaque or low-confidence:

1. assemble fetches the article body header
2. it parses authoritative yEnc metadata:
   - `name`
   - `part`
   - `total`
   - `size`
3. it injects that metadata back into matcher input
4. matcher re-runs and groups the article by yEnc file identity instead of by the opaque visible subject

This prevents a multipart obfuscated post from exploding into many fake one-article binaries.

Important rules:

- if the subject says `[1/5]`, the post is part of a five-file release and must not be treated as a standalone single-binary release
- if the yEnc header says `part=156 total=807`, that article is only part `156` of one file and does not carry file-start bytes or magic-number signatures
- `binary_parts` is kept ordered by `part_number`, so article order is preserved per binary

That is why a release can look like many `.bin` files if assembly does not respect yEnc metadata. The current assembly path now uses yEnc metadata as first-class identity evidence to avoid that regression.

### `indexer release`

Purpose:

- group binary projection rows into catalog releases and release files

Code path:

- `internal/indexing/release/service.go`
- `internal/indexing/release/helpers.go`
- `internal/store/pgindex/release_store.go`

Normal candidate source:

- `release_stage_dirty_families`

This means release work is driven by changed binary families, not by rescanning all binary projection tables every time.

What release does:

1. pulls dirty families
2. loads binary projection rows for that family
3. clusters binary rows into one or more release candidates
4. computes a `ReleaseRecord`
5. writes `releases`
6. writes `release_files`
7. writes release newsgroup mapping
8. updates `nzb_cache`
9. deletes stale release rows for the same family
10. acknowledges the dirty-family queue row

### Important release formation inputs

Release formation relies heavily on these binary columns:

- `release_family_key`
- `source_release_key`
- `release_key`
- `release_name`
- `binary_name`
- `file_name`
- `file_index`
- `expected_file_count`
- `is_auxiliary`
- `is_main_payload`
- `observed_parts`
- `total_parts`
- `total_bytes`
- `posted_at`
- `match_confidence`
- `poster`

### How titles are chosen

Release formation starts from a source title and then upgrades it if better local evidence exists.

The order is roughly:

1. representative source title from binary/release naming
2. inspect-derived title candidates
   - `archive_entry`
   - `nfo`
3. media file path title candidates from recognizable media file names
4. deobfuscated fallback
5. source fallback

This is why you can see titles like:

- `Seed.Release.2026.1080p.BluRay.x265-GRP`

That is not necessarily a bug by itself. It usually means the release title resolver found a readable source-like title and no stronger inspect/enrich evidence replaced it.

### Why are so many `release_files.file_name` values ending with `.bin`?

Because the current code intentionally falls back to `.bin` when it cannot derive a meaningful extension.

There are two fallback sites:

- `internal/indexing/match/helpers.go`
  - `fallbackFileName(...)`
- `internal/indexing/release/helpers.go`
  - `pickFileName(...)`

Current logic:

- if matcher cannot derive a file name with an extension, it appends `.bin`
- if release formation cannot find `binary.file_name`, `binary.binary_name`, `binary.release_name`, or family keys with a real extension, it also appends `.bin`

That is why opaque/obfuscated posts end up with many `.bin` release files.

### Does release formation take missing articles into account?

Yes.

Release completeness comes from binary completeness.

Release formation computes:

- `file_count`
- `expected_file_count`
- `completion_pct`

from clustered binaries and their observed parts.

Current behavior:

- releases can exist before they are perfect
- the service skips obviously fragmentary clusters
- it also respects `ReleaseMinConfidence`
- later assemble passes can improve the same release family and requeue it for re-formation

So releases are not immutable. They are re-formed when binaries change.

### Standalone binary guard

Release formation is intentionally conservative with one-binary releases.

Current rules:

- `[1/1]` or other explicit single-file evidence is enough to allow a standalone release
- readable direct media files like `.mkv`, `.mp4`, `.flac`, or `.mp3` can also form a standalone release when identity is strong
- opaque `.bin` files do not form standalone releases just because they are the only current binary
- archive-shaped one-binary clusters without explicit single-file evidence are also held back

This prevents obfuscated multipart slices from being mislabeled as complete single-file releases.

### `indexer inspect discovery`

Purpose:

- scan opaque complete release files before archive/media filtering
- recover likely archive or direct-media extensions from decoded payload bytes
- canonicalize recovered names back onto v2 binary projections, `release_files`, and `binary_parts`

What it relies on:

- `release_files.file_name`
- `release_files.file_index`
- `release_files.size_bytes`
- `binary_parts`
- decoded article bodies fetched through NNTP
- `binary_recovery_current.recovered_*`

Current behavior:

- candidates are chosen one release at a time for complete releases whose files are still opaque `.bin`
- discovery then scans opaque files within that release instead of trusting a single fallback-sorted file
- when a signature is found, recovery updates:
  - `binary_recovery_current.recovered_kind`
  - `binary_recovery_current.recovered_extension`
  - `binary_identity_current.file_name` when still opaque
  - `release_files.file_name` when still opaque
  - `binary_parts.file_name` when still opaque
- archive recovery may also rename sibling opaque files into `.7z.001`, `.zip.001`, or `.part01.rar` style families when the size/layout pattern looks like a split archive set

Why this stage exists:

- release formation is intentionally fast and metadata-driven
- heavily obfuscated posts often fall back to `.bin`
- archive/media inspect stages are filename-gated
- discovery bridges that gap so later inspect stages can see more of the complete catalog

### `indexer inspect archive`

Purpose:

- inspect archive-shaped release files
- populate archive entry metadata
- provide better title candidates for releases

Candidate filter:

- release must be `completion_pct >= 100`
- release file count must satisfy `expected_file_count`
- file names must look like:
  - `.7z`
  - `.7z.001`
  - `.zip`
  - `.zip.001`
  - `.rar`
  - `.part01.rar`
  - `.part1.rar`

Important consequence:

- without `inspect_discovery`, a release that is 100% complete but still all `.bin` will not be an archive candidate
- with `inspect_discovery`, recovered archive families can be renamed into archive-like file names first, then `inspect_archive` can process them normally

### `indexer inspect media`

Purpose:

- inspect direct media files or media files discovered inside inspected archives
- populate runtime, codec, resolution, subtitle, and quality fields
- contribute stronger title candidates

Candidate filter:

- release must be `completion_pct >= 100`
- file count must satisfy `expected_file_count`
- and one of these must be true:
  - direct media extension exists on release files:
    - `.mkv`
    - `.mp4`
    - `.avi`
    - `.ts`
    - `.flac`
    - `.mp3`
    - `.m4a`
  - or `inspect_archive` already completed and the release file itself is an archive extension

Important consequence:

- direct media inspection also depends on recognizable filenames
- archive-backed media inspection depends on archive inspection having run first
- opaque `.bin` release files block both paths until `inspect_discovery` recovers a usable extension or archive family shape

### `indexer inspect nfo`, `inspect par2`, `inspect password`

These stages use narrower file filters:

- `inspect_nfo`
  - `%.nfo`
- `inspect_par2`
  - `rf.is_pars = true`
- `inspect_password`
  - encrypted archive-like releases only

They all feed metadata back into `releases` through `ApplyReleaseInspectionUpdate`.

### `indexer enrich predb`

Purpose:

- attach scene-like naming and metadata from local or fetched PreDB data

Subcommands:

- `enrich predb sync-feed`
- `enrich predb sync-backfill`
- `enrich predb`
- `enrich predb scene-name-recovery`
- `enrich predb metadata-only-fallback`

Important behavior:

- sync commands populate `predb_entries`
- recovery/fallback commands try to improve weak release identity
- they are more likely to help weakly named releases than archive/media inspection when filenames are opaque

### `indexer enrich tmdb`

Purpose:

- attach external movie/TV metadata

What it updates:

- `tmdb_id`
- `tvdb_id`
- title and identity metadata when confident enough

Important behavior:

- this stage depends on the release already having a usable title/query shape
- it improves releases after formation and inspection, not before

### `indexer_maintenance`

Purpose:

- operational cleanup, not catalog formation

It currently:

- repairs stale stage runtime state
- abandons stale `scrape_runs`
- purges old `indexer_stage_runs`
- purges old `scrape_runs`
- purges retained `article_header_ingest_payloads` older than policy
- prunes eligible stale `release_family_readiness_summaries`
- backfills compact inline grouping summaries and purges eligible stable `binary_grouping_evidence`

Important operational note:

- `indexer maintenance` is the application retention pass
- it reduces row counts and dead-row growth
- if PostgreSQL files still need to shrink on disk after retention cleanup, that is a separate operator step using `VACUUM (ANALYZE)` or `VACUUM FULL`
- for the current growth-trim sprint, use the reclaim runbook in `docs/archive/development/indexer/INDEXER_POSTGRES_RUNTIME_TUNING.md` for the exact table order and downtime expectations

## How Metadata Flows Back Into Releases

### From inspect

Inspect stages update releases through:

- `ApplyReleaseInspectionUpdate`

That can update columns such as:

- `title`
- `deobfuscated_title`
- `title_source`
- `title_confidence`
- `has_par2`
- `has_nfo`
- `encrypted`
- `video_count`
- `audio_count`
- `runtime_seconds`
- `primary_resolution`
- `primary_video_codec`
- `primary_audio_codec`
- `subtitle_languages_json`
- `media_tags_json`
- `media_quality_score`
- `metadata_updated_at`

Important note:

- inspect does not rebuild releases from scratch
- it patches release metadata after formation

### From enrich

Enrichment updates release metadata similarly, but from external metadata sources.

That includes:

- `tmdb_id`
- `tvdb_id`
- title corrections
- external-title and identity improvements

## Current Rough Edges

These are important to understand when reading the catalog today.

### 1. Opaque `.bin` filenames are blocking inspect coverage

Current dev DB measurement:

- `548` releases at `completion_pct >= 100`
- only `49` archive-like `release_files`
- `0` direct media-like `release_files`
- `37,809` `release_files` with `.bin` suffix

That means inspect is currently skipping most complete releases because they do not look like archive/media files by name.

### 2. Release titles are only as good as local evidence allows

If inspect and enrich cannot upgrade the title, the release keeps a source-like readable title.

That can produce titles that are:

- readable
- plausible
- but still not authoritative

Example:

- `Seed.Release.2026.1080p.BluRay.x265-GRP`

That usually means the title resolver accepted a source-derived title because there was no stronger archive-entry, NFO, or external enrichment candidate.

### 3. Inspect candidate discovery is filename-driven

This is good for speed, but weak when posts are heavily obfuscated.

A complete release can still be inspect-ineligible if its file names are not recognizable.

## Practical Answers To Common Questions

### “Are the 548 complete releases not media files?”

Not necessarily.

More accurately:

- they are not currently identifiable as archive/media candidates by the inspect filename filters

Some may truly be non-media.
Many are probably obfuscated media/archive posts whose release file names degraded to `.bin`.

### “If assemble ran before all parts existed, can the binary improve later?”

Yes.

Later scraped headers are assembled as new pending raw headers, and they update the existing `binary_core` anchor, current binary side-table projections, and `binary_parts` rows for the same `binary_key`.

### “If a binary changes later, will the release update?”

Yes.

`UpsertBinary` marks the family dirty in `release_stage_dirty_families`, and `indexer release` re-forms the release family on the next pass.

### “Does inspect automatically revisit changed releases?”

Yes, when the candidate filter still matches.

Inspection candidate selection compares `b.updated_at` against the existing stage row and will re-run when the binary changed or the prior inspection failed.

If the file name shape no longer matches the stage filter, it still will not be selected.

## What To Look At When Debugging

When a release looks wrong:

1. `releases`
   - `title`
   - `title_source`
   - `completion_pct`
   - `classification`
   - `expected_file_count`
2. `release_files`
   - `file_name`
   - `is_pars`
   - `binary_id`
3. binary projection tables
   - `file_name`
   - `binary_name`
   - `release_name`
   - `release_family_key`
   - `base_stem`
   - `observed_parts`
   - `total_parts`
   - `match_confidence`
4. `binary_inspections`
   - per-stage status and summaries
5. `binary_archive_entries`
   - extracted archive filenames
6. `binary_media_streams`
   - codec/runtime/resolution stream evidence
7. `predb_entries`, `release_tmdb_matches`, `release_tvdb_matches`
   - external enrichment evidence

## Current Stabilization Status

The infrastructure stabilization work is largely complete:

- compact header storage
- dirty-family release queue
- maintenance stage
- backfill-until-date
- squashed migration baseline

But the broader stabilization scope should still be considered open until release identity quality is improved for obfuscated posts.

The biggest remaining product-quality gap is:

- too many release files degrade to `.bin`
- which blocks inspect archive/media coverage
- which leaves many releases under-titled or source-titled

That is now less of a schema problem and more of a release-identity / file-name recovery problem.
