# Indexer Scrape And Newsgroup Management Sprint

Snapshot date: 2026-06-04

This is the active execution guide for the scrape/newsgroup management sprint.

## Scope

- fix runtime reconfiguration so scrape groups can be removed, re-added, and saved without restart traps
- add persistent wildcard rules plus provider-backed manual group discovery
- add a dedicated admin workflow for scrape/newsgroup management
- keep `docs/INDEXER_CURRENT_SCHEMA_AND_SYSTEM_INTERACTIONS.md` as the permanent schema and ownership reference
- preserve downloader-safe per-file newsgroup assignment in generated NZBs

## Working Decisions

- wildcard rules are persistent runtime settings
- wildcard scope is global across configured indexer providers
- provider inventory refresh is manual-only through explicit scan/rescan
- effective scrape groups are derived from explicit groups plus materialized wildcard groups
- `indexing.newsgroups` and `indexing.backfill_until_date_by_group` remain compatibility mirrors during transition
- zero effective groups is valid runtime state
- stages that require missing prerequisites should idle through gating/no-op behavior instead of blocking settings persistence
- release/catalog behavior stays single-release with multi-group provenance
- file-level downloader correctness wins over cross-group unioning: one file payload’s article set must stay bound to one newsgroup

## Implementation Checklist

- [x] add active sprint routing and permanent schema notes
- [x] add explicit-group, wildcard-rule, provider-inventory, and materialized-group runtime settings surfaces
- [x] remove the current indexer newsgroup save trap and allow zero effective groups
- [x] gate prerequisite-sensitive stages so missing groups or NNTP servers do not hard-fail settings changes
- [x] add provider-backed group listing and wildcard preview/apply APIs
- [x] add a dedicated admin scrape page and move group editing out of generic runtime settings
- [x] emit per-file NZB newsgroups from the file’s binary provenance instead of the release-wide union when available

## Acceptance Criteria

- runtime settings can be saved with zero effective scrape groups
- removing and re-adding scrape groups no longer requires disabling stages first
- provider inventory can be scanned manually and wildcard preview/apply is deterministic from persisted state
- the runtime settings page points operators to the dedicated scrape workflow
- generated NZBs keep each file bound to its own newsgroup when that provenance exists

## Migration Order

1. land the runtime settings model and keep compatibility mirrors derived
2. route admin workflows to the dedicated scrape page and APIs
3. migrate callers to effective-group helpers instead of raw `indexing.newsgroups`
4. retire direct editing assumptions around the flat legacy newsgroup fields once the remaining callers are moved

## Sign-Off

- active doc created and routed as the primary execution guide
- permanent schema doc updated with scrape ownership and file-level provenance rules
- code path now supports explicit groups, wildcard materialization, manual provider scan, and per-file NZB group output
