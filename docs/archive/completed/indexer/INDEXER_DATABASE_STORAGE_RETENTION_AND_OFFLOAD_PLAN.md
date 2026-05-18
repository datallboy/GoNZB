# Indexer Database Storage, Retention, And Offload Plan

Snapshot date: 2026-04-29

This is the active execution plan for reducing PostgreSQL disk usage in the usenet indexer without losing release-serving correctness.

## Current Baseline

Measured against the live local `gonzb-postgres` database on `2026-04-29`.

Database total:

- `gonzb`: `42 GB`

Largest tables:

| Table | Total | Heap | Indexes | Estimated rows | Notes |
| --- | ---: | ---: | ---: | ---: | --- |
| `article_header_ingest_payloads` | `21 GB` | `17 GB` | `4258 MB` | `32.3M` | largest table; raw ingest payload and structured subject fields |
| `article_headers` | `12 GB` | `4914 MB` | `7444 MB` | `32.6M` | core header identity and serving path |
| `binary_parts` | `4731 MB` | `2857 MB` | `1874 MB` | `16.4M` | assembled article-to-binary mapping |
| `release_file_articles` | `2570 MB` | `965 MB` | `1605 MB` | `13.0M` | derived release-file article mapping |
| `binaries` | `832 MB` | `327 MB` | `505 MB` | `376k` | assembled binary/file records |
| `binary_grouping_evidence` | `394 MB` | `381 MB` | `13 MB` | `360k` | grouping trace payloads |
| `release_family_readiness_summaries` | `334 MB` | `241 MB` | `93 MB` | `536k` | release family readiness state |
| `release_stage_dirty_families` | `61 MB` | tiny | `61 MB` | `0-19` live rows | bloated after churn |

Largest indexes:

| Index | Size | Notes |
| --- | ---: | --- |
| `idx_article_header_ingest_payloads_structured_name` | `3475 MB` | supports assemble path A structured filename lookup |
| `article_headers_newsgroup_id_message_id_key` | `3211 MB` | duplicate prevention and message identity |
| `article_headers_newsgroup_id_article_number_key` | `1407 MB` | scrape checkpoint/idempotency support |
| `article_headers_pkey` | `1006 MB` | header primary key |
| `idx_article_headers_pending_assembly` | `740 MB` | pending assembly selector |
| `binary_parts_binary_id_part_number_key` | `740 MB` | binary part uniqueness |
| `binary_parts_article_header_id_key` | `620 MB` | one binary part per article header |
| `idx_article_headers_pending_assembly_claims` | `608 MB` | assemble claim exclusion |
| `release_file_articles_release_file_id_article_header_id_key` | `574 MB` | derived release-file article uniqueness |
| `release_file_articles_release_file_id_part_number_key` | `504 MB` | derived release-file part uniqueness |

Current row state:

- `article_headers`: `32,694,806` rows
- assembled headers: `16,938,281`
- pending headers: `15,795,429`
- active assemble claims: `0`
- `article_header_ingest_payloads`: `32,694,806` rows
- payloads currently purgeable by existing `7 day` assembled retention rule: `2,223,260`
- assembled headers within current `7 day` retention window: `14,714,037`
- `binary_parts`: about `16.9M` rows
- `release_file_articles`: about `13.0M` rows
- releases: `5,955`
- complete releases: `1,809`
- complete and not passworded releases: `1,307`
- `nzb_cache` currently stores status/hash only, not NZB bytes

## Key Findings

### Finding 1. Raw ingest payload retention is the largest disk consumer

`article_header_ingest_payloads` is about half the database by itself.

Current maintenance deletes payload rows only when the linked header is assembled and `assembled_at < NOW() - INTERVAL '7 days'`.

Observed implication:

- the existing rule is safe but conservative
- `14.7M` assembled payload rows are still retained inside the current seven-day window
- lowering retention to `1 day`, or deleting payloads immediately after assembly, is the largest near-term space lever

Important PostgreSQL behavior:

- `DELETE` makes space reusable inside PostgreSQL
- `DELETE` does not necessarily return disk to the OS
- returning disk requires `VACUUM FULL`, `CLUSTER`, table rewrite, dump/restore, or online tooling such as `pg_repack`

### Finding 2. `release_stage_dirty_families` is small logically but bloated physically

The table had `0` live rows but about `61 MB` allocated, almost all index space.

This is a low-risk maintenance target:

- `REINDEX TABLE release_stage_dirty_families`
- optionally `VACUUM FULL release_stage_dirty_families` if a short lock is acceptable

### Finding 3. `release_file_articles` appears derivable from `binary_parts`

Current code path:

- release formation reads `binary_parts` through `ListBinaryPartArticlesBatch`
- `ReplaceReleaseFiles` writes those rows into `release_file_articles`
- NZB generation reads `release_file_articles` through `ListCatalogReleaseFileArticles`
- public/admin file summaries use `release_file_articles` only for article counts
- inspect materialization also uses `ListCatalogReleaseFileArticles`

Live invariant check:

- `release_files`: `77,705`
- `release_files` without `binary_id`: `0`
- duplicate `release_files.binary_id` links: `0`
- article-count mismatches between `release_file_articles` and `binary_parts`: initially observed `2`, then `0` on the follow-up detail query, likely transient during release rewrite

Conclusion:

- `release_file_articles` is very likely redundant if `release_files.binary_id` is mandatory and one release file maps to one binary
- this can save about `2.5 GB` now and reduce release write amplification
- it should be removed only after adding explicit invariant tests and migrating all reads to `binary_parts`

### Finding 4. NZBs are generated on demand, not cached as blobs

`nzb_cache` currently stores:

- release id
- generation status
- hash
- timestamps
- last error

It does not store generated NZB bytes.

Current serving path:

- resolver loads release files
- resolver loads per-file articles
- resolver loads newsgroups
- resolver builds NZB XML in memory
- resolver updates `nzb_cache` metadata

Implication:

- `article_headers`, `binary_parts`, and currently `release_file_articles` remain part of the serving path
- durable NZB blob caching is the unlock for pruning older serving rows more aggressively

## Action Plan

## 1. Configurable Maintenance Retention

Goal:

- make disk retention policy configurable instead of hard-coded in maintenance SQL

Tasks:

- add runtime/bootstrap settings for indexer maintenance retention
- include at least:
  - `indexing.maintenance.header_payload_retention_hours`
  - `indexing.maintenance.stage_run_retention_hours`
  - `indexing.maintenance.completed_inspection_retention_hours`
  - `indexing.maintenance.failed_inspection_retention_hours`
  - `indexing.maintenance.reclaim_dry_run_enabled` or a command/API dry-run mode
- replace hard-coded maintenance intervals:
  - assembled header payloads: currently `7 days`
  - completed inspections: currently `14 days`
  - failed inspections: currently `30 days`
  - stale scrape/stage run cleanup where applicable
- choose conservative defaults initially:
  - payload retention: `24h` or `168h` if we want no behavioral change
  - stage run retention: `7d`
  - completed inspection retention: `14d`
  - failed inspection retention: `30d`
- allow operators to set payload retention to `0` for immediate payload deletion after assembly

Acceptance criteria:

- maintenance behavior is driven by settings, not literal SQL intervals
- settings are visible in runtime settings APIs
- changing retention does not require code changes
- defaults preserve current behavior unless intentionally changed

## 2. Maintenance Reporting And Frontend Controls

Goal:

- let the operator run maintenance and see reclaim estimates before and after cleanup

Tasks:

- add a maintenance report API for dry-run estimates:
  - purgeable header payload rows
  - purgeable inspection rows
  - purgeable stage runs
  - orphan releases
  - bloated relation candidates from `pg_stat_user_tables` and relation sizes
  - table/index size before cleanup
- add a run-maintenance API action that returns:
  - rows deleted by category
  - table/index size before
  - table/index size after
  - notes that space may be reusable but not returned to OS until compaction
- add optional admin-only maintenance actions:
  - `VACUUM ANALYZE` selected tables
  - `REINDEX TABLE` selected small bloated tables
  - do not expose `VACUUM FULL` casually because it takes stronger locks
- add frontend maintenance page/card:
  - current DB total size
  - largest tables
  - largest indexes
  - purge estimate by category
  - run maintenance button
  - before/after report display
  - warning when OS disk reclaim requires table rewrite/`pg_repack`

Acceptance criteria:

- frontend can show expected reclaim candidates before mutation
- maintenance run output is persisted in stage/run metrics or a maintenance report record
- user can distinguish logical cleanup from physical disk return

## 3. Header Payload Retention Cleanup

Goal:

- reduce the `21 GB` `article_header_ingest_payloads` footprint safely

Tasks:

- implement configurable payload retention from section 1
- add a dry-run query for payload deletion count
- consider deleting payloads immediately after `article_headers.assembled_at` is set, if no retry/debug workflow requires retained subject/xref/raw overview payloads after assembly
- ensure pending assemble selectors still have structured payload rows available
- add tests proving pending headers keep payloads and assembled old headers lose payloads

Operational recommendation:

- first lower retention to `24h`
- run maintenance
- run `VACUUM ANALYZE article_header_ingest_payloads`
- if the system is stable, evaluate `0h` retention
- use `pg_repack` or planned downtime with `VACUUM FULL` only when OS-level disk return is required

Acceptance criteria:

- pending headers still assemble correctly
- path A selector retains required structured subject fields for pending rows
- deleting assembled payloads does not affect NZB generation

## 4. `release_file_articles` And `binary_parts` Consolidation

Goal:

- remove redundant per-release article mappings if `release_files.binary_id -> binary_parts` is truly the source of truth

Current code usage:

- release service:
  - builds release files by reading `binary_parts`
  - persists copied article refs into `release_file_articles`
- resolver:
  - builds NZB by calling `ListCatalogReleaseFileArticles`
- inspect materialization:
  - reads file articles via `ListCatalogReleaseFileArticles`
- public/admin reads:
  - use `release_file_articles` for article counts and file detail article lists

Consolidation plan:

- add invariant tests:
  - every `release_files` row has `binary_id`
  - no two active `release_files` rows point at the same `binary_id`
  - `COUNT(binary_parts WHERE binary_id = rf.binary_id)` equals current article count for release files
  - part ordering by `binary_parts.part_number` matches NZB segment order
- change `ListCatalogReleaseFileArticles` to join `release_files -> binary_parts -> article_headers` instead of `release_file_articles -> article_headers`
- change public/admin article counts to use `b.observed_parts` or count `binary_parts`
- change `GetIndexerFileDetail` article list to use the new `binary_parts` path
- stop writing `release_file_articles` in `ReplaceReleaseFiles`
- keep the table for one compatibility migration window, but no longer populate it
- add a later migration to drop `release_file_articles` and its indexes after validation

Risks and checks:

- if a future release file can represent only a subset/range of a binary, the table may still be needed
- if one binary can legitimately appear in more than one release, removing copied rows needs a different ownership model
- if release files can be manually overridden away from binary ids, direct binary-part lookup would break

Acceptance criteria:

- NZB generation output is byte-equivalent before and after changing `ListCatalogReleaseFileArticles`
- inspect archive/media materialization still works
- public/admin file article counts remain correct
- release formation no longer inserts millions of derived article rows
- `release_file_articles` can be dropped or retained empty

## 5. NZB Blob Cache And Offload

Goal:

- store generated NZBs as durable blobs so older article mappings can eventually be pruned more aggressively

Design direction:

- extend `nzb_cache` with:
  - `blob_key`
  - `blob_backend`
  - `blob_size_bytes`
  - `content_encoding`
  - `nzb_hash_sha256`
  - `generated_at`
  - `last_verified_at`
- support blob backends:
  - local filesystem/HDD path
  - S3-compatible object storage
  - existing app blob abstraction if it is compatible
- resolver flow:
  - check ready blob first
  - if present and hash-valid, serve blob
  - if missing/stale, build from DB and write blob atomically
  - update `nzb_cache`

Retention unlock after blob cache:

- for blob-backed complete releases, serving no longer requires per-article DB joins
- this enables optional pruning tiers:
  - keep release metadata and files
  - keep blob NZB
  - prune `release_file_articles` if still present
  - later consider pruning old `binary_parts` and old assembled `article_headers` only for blob-backed, verified releases

Acceptance criteria:

- generated NZB hash is stable
- blob read path is preferred
- DB fallback still works if blob is missing
- operators can choose local disk or object storage
- pruning never removes the only copy of a downloadable NZB

## 6. Index And Bloat Maintenance

Goal:

- reclaim obvious index bloat and track future bloat growth

Tasks:

- add maintenance report section for:
  - relation total size
  - heap size
  - index size
  - live/dead tuples
  - last vacuum/analyze
- add special-case recommendation for tiny-live-row, large-index tables
- immediately target:
  - `release_stage_dirty_families`: `0` live rows, `61 MB` allocated
- consider periodic `REINDEX CONCURRENTLY` for large churn indexes only after measuring need

Acceptance criteria:

- operator can identify bloated tables from UI/API
- small bloated tables can be fixed with low-risk reindex operations
- large-table compaction remains an explicit operator choice, not automatic

## 7. Data Retention Tiers

Goal:

- define what must be kept for each release/header lifecycle state

Proposed tiers:

- pending headers:
  - keep `article_headers`
  - keep `article_header_ingest_payloads`
  - no `binary_parts` yet
- assembled headers for active/incomplete binaries:
  - keep `article_headers`
  - keep `binary_parts`
  - payload retention configurable, likely short
- formed releases without blob NZB:
  - keep release metadata
  - keep `release_files`
  - keep `binary_parts` and `article_headers` needed to generate NZB
- formed releases with verified blob NZB:
  - keep release metadata
  - keep `release_files`
  - keep blob metadata/hash
  - optionally prune derived article mapping
  - only prune `article_headers`/`binary_parts` if no inspection/rebuild workflow needs them

Acceptance criteria:

- every retention rule maps to a lifecycle state
- no cleanup rule breaks scrape, assemble, inspect, release, or NZB serving

## Open Questions

- Should payload retention default preserve current `7 days`, or should the next migration intentionally lower it to `24h`?
- Do we need per-provider or per-newsgroup retention settings?
- Should NZB blobs live in the existing downloader blob store, or should indexer blobs be separate?
- How long should DB article mappings be kept after a verified NZB blob exists?
- Should the frontend expose physical compaction actions, or only report that external DBA action is needed?

## Validation Queries

Largest tables:

```sql
SELECT
  c.relname AS table,
  pg_size_pretty(pg_total_relation_size(c.oid)) AS total,
  pg_size_pretty(pg_relation_size(c.oid)) AS heap,
  pg_size_pretty(pg_indexes_size(c.oid)) AS indexes,
  c.reltuples::bigint AS est_rows
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE c.relkind = 'r'
  AND n.nspname = 'public'
ORDER BY pg_total_relation_size(c.oid) DESC
LIMIT 30;
```

Payload purge estimate:

```sql
SELECT COUNT(*) AS purgeable_payloads
FROM article_header_ingest_payloads p
WHERE EXISTS (
  SELECT 1
  FROM article_headers ah
  WHERE ah.id = p.article_header_id
    AND ah.assembled_at IS NOT NULL
    AND ah.assembled_at < NOW() - INTERVAL '7 days'
);
```

`release_file_articles` consolidation invariant:

```sql
SELECT
  COUNT(*) AS release_files,
  COUNT(*) FILTER (WHERE binary_id IS NULL) AS without_binary,
  COUNT(DISTINCT binary_id) AS distinct_binary_ids
FROM release_files;

SELECT COUNT(*) AS duplicate_binary_links
FROM (
  SELECT binary_id
  FROM release_files
  WHERE binary_id IS NOT NULL
  GROUP BY binary_id
  HAVING COUNT(*) > 1
) d;

SELECT COUNT(*) AS article_mismatch_files
FROM release_files rf
LEFT JOIN LATERAL (
  SELECT COUNT(*) AS c
  FROM release_file_articles rfa
  WHERE rfa.release_file_id = rf.id
) rfa ON TRUE
LEFT JOIN LATERAL (
  SELECT COUNT(*) AS c
  FROM binary_parts bp
  WHERE bp.binary_id = rf.binary_id
) bp ON TRUE
WHERE COALESCE(rfa.c, 0) <> COALESCE(bp.c, 0);
```
