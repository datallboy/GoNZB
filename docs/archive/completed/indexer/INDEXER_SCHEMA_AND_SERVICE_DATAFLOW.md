# Indexer Schema And Service Dataflow

Snapshot date: 2026-05-13

This doc is the current working map for how the indexer tables should be organized, which stages own them, and where the current missing links are.

Use this doc when:

- deciding whether a new field belongs on `binaries`, `releases`, or a side table
- tracing why a stage updated one table but a downstream stage did not react
- identifying which `binary_*` tables are hot-path identity tables versus enrichment/evidence tables

## Core Model

The database should be read as four layers:

1. ingest
2. binary assembly identity
3. release readiness
4. downstream inspection/evidence

The most important rule is that each layer should do one job well:

- scrape persists article/header facts
- assemble turns article facts into binary/file identity
- release turns binary identity into user-facing release candidates
- inspect/enrich adds stronger evidence and may promote or refine upstream identity

## Table Groups

### Ingest and article facts

Primary tables:

- `article_headers`
- `article_header_ingest_payloads`
- `scrape_runs`
- `scrape_checkpoints`

Purpose:

- preserve NNTP/XOVER facts as scraped
- provide pending work for assemble

Current guidance:

- `article_headers` is the durable parsed article row
- `article_header_ingest_payloads` is staging/debug retention and should not grow into a second source of truth for identity
- we should keep parsed fields, not retain full raw XOVER lines long term

### Binary assembly identity

Primary tables:

- `binaries`
- `binary_parts`
- `binary_grouping_evidence`

Purpose:

- `binaries` is the canonical per-file/binary identity row
- `binary_parts` is the canonical article-to-binary membership map
- `binary_grouping_evidence` is the audit trail for how a binary was keyed or promoted

Stage ownership:

- `assemble` creates and refreshes `binaries` and `binary_parts`
- `recover_yenc` may re-key, merge, or promote `binaries`, and updates `binary_parts` / `release_files` references when identity improves

Important columns on `binaries`:

- `binary_key`: strongest current file identity
- `file_name`: current best known filename
- `file_index`, `expected_file_count`: poster/NZB file-set coverage, such as `[01/10]`
- `expected_archive_file_count`: stronger archive/protected-file denominator from PAR2 or archive evidence
- `release_family_key`, `source_release_key`, `file_set_key`, `file_family_key`: grouping identity at different strengths
- `is_main_payload`, `is_auxiliary`: whether the binary is payload versus sidecar

### Release readiness and user-facing release state

Primary tables:

- `release_family_readiness_summaries`
- `releases`
- `release_files`

Purpose:

- `release_family_readiness_summaries` is the pre-release queue/read model
- `releases` is the user-facing release record
- `release_files` is the membership map from release to binaries/files

Stage ownership:

- `assemble` and `recover_yenc` refresh readiness summaries
- `release` consumes readiness summaries and writes `releases` and `release_files`

Important rule:

- release should consume readiness summaries, not rediscover binary identity from scratch

### Inspection and evidence side tables

Primary tables:

- `binary_inspections`
- `binary_inspection_artifacts`
- `binary_archive_entries`
- `binary_media_streams`
- `binary_text_evidence`
- `binary_par2_sets`
- `binary_par2_targets`

Purpose:

- `binary_inspections` is the stage run ledger and rerun gate
- `binary_inspection_artifacts` stores sampled/materialized outputs and metadata
- evidence tables store structured results from a specific inspect stage

Ownership:

- `inspect_discovery` writes stage summary plus content filtering evidence
- `inspect_nfo` writes `binary_text_evidence`
- `inspect_archive` writes `binary_archive_entries`
- `inspect_media` writes `binary_media_streams`
- `inspect_par2` writes `binary_par2_sets` and `binary_par2_targets`

These tables should remain derived evidence, not the canonical source of binary membership.

## What Each `binary_*` Table Means

### Hot-path identity tables

- `binaries`: canonical file/binary row
- `binary_parts`: canonical article membership row
- `binary_grouping_evidence`: why the current identity was chosen

These are used directly by assemble, recover_yenc, release, and summary refresh.

### Inspection ledger tables

- `binary_inspections`
- `binary_inspection_artifacts`

These answer:

- was a stage run already attempted
- should the stage rerun
- where is the sampled/materialized evidence

### Structured evidence tables

- `binary_archive_entries`: archive listing results
- `binary_media_streams`: ffprobe/media results
- `binary_text_evidence`: text/NFO/discovery-derived text evidence
- `binary_par2_sets`: PAR2 set rows
- `binary_par2_targets`: PAR2 protected target filenames and sizes

These should inform promotion and metadata, but they should not replace `binaries` as the assembled identity row.

## Current Missing Links

### PAR2 target persistence existed in code, but old completed inspections blocked reruns

Live finding:

- `binary_par2_sets` contains rows
- `binary_inspections` contains many completed `inspect_par2` rows
- `binary_par2_targets` is empty

What that means:

- the old `inspect_par2` implementation ran and marked binaries completed before target extraction existed
- after the new target-persistence code landed, completed inspection rows prevented reruns because `b.updated_at` had not changed

Required behavior:

- `inspect_par2` must rerun a PAR2 binary when its completed inspection exists but no `binary_par2_targets` rows exist for that binary

### PAR2 evidence is downstream identity help, not primary assembly

`inspect_par2` can tell us:

- protected filenames
- protected file count
- likely archive volume indexes

But it only becomes actionable when matching binaries exist or later arrive with names that can match those targets.

That means:

- `recover_yenc` is still the promotion engine for weak obfuscated binaries
- `inspect_par2` improves denominators and file indexes after a PAR2 binary is inspectable

### Discovery is a filter/evidence stage, not a grouping engine

`inspect_discovery` should:

- identify junk/pointer/filtered content by data
- emit evidence rows and filter markers

It should not be expected to solve weak multipart grouping by itself.

## Recommended Organization Rules

### Keep one canonical owner per fact

- article membership belongs in `binary_parts`
- binary identity belongs in `binaries`
- release membership belongs in `release_files`
- stage execution state belongs in `binary_inspections`
- parsed stage outputs belong in their own evidence tables

### Keep readiness as a read model

`release_family_readiness_summaries` should remain the fast queue surface for release selection.

It should aggregate from `binaries` and evidence-derived promotions, not become another source of truth that stores unrelated stage state.

### Prefer promotion over replacement

When downstream evidence improves identity:

- update or merge the owning `binaries` row
- refresh readiness summaries
- let release consume the improved family

Do not create separate parallel release/grouping systems per inspect stage.

## Immediate Follow-Up

1. Rerun `inspect_par2` for binaries whose completed inspections have no target rows.
2. Validate that `binary_par2_targets` begins filling and that `expected_archive_file_count` starts appearing on matched archive binaries.
3. Continue tightening weak obfuscated grouping through `recover_yenc`, PAR2 target matching, and archive-volume inference.
4. Revisit whether `article_header_ingest_payloads` still needs to retain anything not already parsed into durable article fields.

## Sign-Off

This doc reflects the current intended organization and the current live PAR2 rerun gap discovered on 2026-05-13.
