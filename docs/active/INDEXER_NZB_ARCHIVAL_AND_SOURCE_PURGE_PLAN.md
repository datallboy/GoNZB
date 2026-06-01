# Indexer NZB Archival And Database Storage Reduction Plan

Snapshot date: 2026-06-01

## Summary

Move completed local-indexer releases to a blob-backed archival model as soon as they are `nzb-ready`, then purge their heavy source lineage from PostgreSQL.

Key architectural decision:

- Postgres is the authoritative store for archival and purge-safety metadata.
- SQLite is not authoritative for this workflow.
- SQLite may continue to mirror local filesystem blob-cache state for node-local reconciliation, but it must not decide whether source records are safe to purge.

Reason:

- purge safety is an indexer catalog concern
- archival state must survive node loss and remain consistent across deployments
- SQLite blob metadata today is optional/local-runtime cache state, not durable catalog truth

This plan keeps archived releases searchable and downloadable indefinitely by retaining a compact release catalog plus blob-backed NZB, while deleting the large temporary/source tables that are driving database growth.

## Follow-Up Work: Background NZB Pre-Generation

Current gap:

- `nzb_cache.generation_status = 'ready'` is currently reached when the NZB resolver is invoked on demand.
- In practice this means archival eligibility can lag behind release readiness until a manual/API NZB fetch happens.

Required follow-up:

- Add a dedicated late pipeline stage:
  - `release_generate_nzb`
- This stage must pre-generate NZBs in the background for releases that have already crossed the intended public-ready threshold.
- `release_archive_nzb` should then archive from that ready state, and `release_purge_archived_sources` should continue to purge only after archival metadata is durable and the release is `purge_pending`.

Ready-policy direction:

- The system needs one shared runtime-configurable release-ready policy for:
  - public indexer visibility
  - background NZB pre-generation eligibility
- Any threshold-like rules that determine whether a release is ready enough to become public should not remain hidden as hardcoded literals.
- At minimum this follow-up should move the current public-ready threshold controls for:
  - confidence
  - completion percentage
  - identity strictness
  - optional inspect requirement
  - optional enrich requirement
  into runtime settings.

Expected pipeline order after this follow-up:

- `release_summary_refresh`
- `release`
- `release_generate_nzb`
- `release_archive_nzb`
- `release_purge_archived_sources`

## Implementation Status

Status date: 2026-06-01

### Sign-off: implemented in code

- Added dedicated Postgres archival ownership instead of extending `nzb_cache`:
  - `release_archive_state`
  - `release_archive_lineage_binaries`
  - `release_archive_lineage_article_headers`
- Added single-root blob-store configuration rooted at `store.blob_dir`.
  - Aggregator cache remains in `store.blob_dir`
  - Indexer archive lives in `store.blob_dir/indexer-archive`
- Added dedicated late stages:
  - `release_generate_nzb`
  - `release_archive_nzb`
  - `release_purge_archived_sources`
- Added shared runtime-configurable public-ready policy controls under `indexing.release` for:
  - `public_min_match_confidence`
  - `public_min_completion_pct`
  - `public_min_identity_status`
  - `public_require_inspection`
  - `public_require_enrichment`
- Applied the same public-ready policy to:
  - public indexer visibility
  - local `usenet_indexer` aggregator search source
  - background NZB generation eligibility
- Added archived NZB serving through the authoritative archive store before any live catalog rebuild path.
- Added purge-safe lineage snapshots in Postgres and purge execution that only deletes binary lineage when it is not still shared by non-archived releases.
- Added dashboard stats and stage-throughput visibility for archive and purge backlog/work.

### Sign-off: execution choices made

- `nzb_cache` remains generation/hash metadata only. It is not archival truth.
- The implemented release state flow is:
  - `active`
  - `archive_pending`
  - `archive_failed`
  - `purge_pending`
  - `purged`
- The plan’s intermediate steady `archived` state is collapsed in code into direct handoff from durable archive write to `purge_pending`.
  - Rationale: once the blob write and Postgres metadata commit succeed, the release is immediately purge-eligible in the current implementation.
- Implemented operational `nzb-ready` gate:
  - `releases.source_kind = 'usenet_index'`
  - `nzb_cache.generation_status = 'ready'`
  - release has persisted `release_files`
  - release has persisted `release_newsgroups`
- Implemented operational background generation gate:
  - release satisfies the shared public-ready policy
  - release has persisted `release_files`
  - release has persisted `release_newsgroups`
  - `nzb_cache.generation_status <> 'ready'`
  - archive state is effectively `active` or `archive_failed`
- Inspect/enrich requirements are now runtime-configurable on the public-ready policy and default to disabled.
  - Rationale: this preserves prior behavior by default while allowing stricter public/generation readiness when desired.
- Archived object key convention implemented:
  - `releases/<provider_id>/<release_id>/<sha256>.nzb`

### Sign-off: metrics implemented

- Generate stage metrics:
  - `generate_candidates`
  - `generate_attempted`
  - `generated_ready_count`
  - `generate_failures`
- Archive stage metrics:
  - `archive_candidates`
  - `archive_claimed`
  - `archived_count`
  - `archive_failures`
- Purge stage metrics:
  - `purge_candidates`
  - `purged_count`
  - `skipped_shared_lineage_rows`
  - `rows_deleted_by_table`
- Dashboard stats added:
  - `generate_nzb_pending_releases`
  - `archive_pending_releases`
  - `archived_waiting_for_purge_releases`
  - `purged_archived_releases`
  - `blob_backed_archived_releases`
- Stage throughput added:
  - `release_generate_nzb`
  - `release_archive_nzb`
  - `release_purge_archived_sources`

### Sign-off: testing completed

- Added focused unit coverage for:
  - archive stage object-key generation and metadata persistence
  - background generate stage readiness metric aggregation
  - purge stage metric aggregation
  - filesystem blob store nested archive-key support
- Full repository validation run completed:
  - `go test ./...`
- Runtime settings were enabled through the admin settings API and persisted in SQLite runtime settings:
  - revision `73`
  - `release_archive_nzb.enabled = true`
  - `release_archive_nzb.interval_minutes = 1`
  - `release_archive_nzb.batch_size = 100`
  - `release_purge_archived_sources.enabled = true`
  - `release_purge_archived_sources.interval_minutes = 1`
  - `release_purge_archived_sources.batch_size = 50`
- Admin stage API smoke pass completed:
  - `POST /api/v1/indexer/stages/release_archive_nzb/run`
  - `POST /api/v1/indexer/stages/release_purge_archived_sources/run`
- Background generation stage is now implemented and available through:
  - runtime settings stage key `release_generate_nzb`
  - CLI command `gonzb indexer release generate-nzb`
  - stage API name `release_generate_nzb`
- Live runtime enablement and baseline capture completed after implementation:
  - server restarted on the updated code
  - stage enabled through `PATCH /api/v1/admin/indexer/stages/release_generate_nzb`
  - runtime settings revision advanced to `74`
  - persisted runtime value:
    - `release_generate_nzb.enabled = true`
    - `release_generate_nzb.interval_minutes = 1`
    - `release_generate_nzb.batch_size = 100`

### Sign-off: performance/baseline note

- Measured rollout baseline was captured after enabling both stages in runtime settings.
- Current environment state at measurement time:
  - `archive_pending_releases = 0`
  - `archived_waiting_for_purge_releases = 0`
  - `purged_archived_releases = 0`
  - `blob_backed_archived_releases = 0`
- Observed archive-stage empty-pass run:
  - run id `70752`
  - status `completed`
  - duration about `75 ms`
  - metrics: `archive_candidates = 100`, `archive_claimed = 0`, `archived_count = 0`, `archive_failures = 0`
- Observed purge-stage empty-pass run:
  - run id `70759`
  - status `completed`
  - duration about `24 ms`
  - metrics: `purge_candidates = 0`, `purged_count = 0`, `rows_deleted_by_table = {}`, `skipped_shared_lineage_rows = 0`
- Observed stage-throughput windows after the smoke pass:
  - `release_archive_nzb`: `2` completed runs in the 1h window, `0` items processed, `79.04 ms` average run duration
  - `release_purge_archived_sources`: `1` completed run in the 1h window, `0` items processed, `23.95 ms` average run duration
- Interpretation:
  - this is an enabled empty-queue baseline, not a throughput-under-load baseline
  - there were no eligible archival or purge candidates in the current catalog during measurement
  - the measurement still validates scheduler wiring, SQLite-backed runtime settings, stage execution, metrics emission, and dashboard visibility
- Background generation follow-up note:
  - this implementation now exposes the missing pre-generation stage and runtime public-ready thresholds
  - a refreshed live baseline for `generate_nzb_pending_releases` should be captured after deployment because the earlier empty-queue archival baseline was measured before the background generation stage existed

### Sign-off: live background-generation baseline

- Measurement date:
  - `2026-06-01`
- Live public-ready catalog size during measurement:
  - public indexer releases total: `3337`
- Initial refreshed backlog snapshot after enabling `release_generate_nzb`:
  - `generate_nzb_pending_releases = 3333`
  - `archive_pending_releases = 0`
  - `archived_waiting_for_purge_releases = 0`
  - `purged_archived_releases = 0`
  - `blob_backed_archived_releases = 0`
- First live generate runs:
  - run `71226` scheduled, completed in about `8.32 s`
    - `generate_candidates = 100`
    - `generate_attempted = 100`
    - `generated_ready_count = 100`
    - `generate_failures = 0`
  - run `71247` scheduled, completed in about `6.14 s`
    - `generate_candidates = 100`
    - `generate_attempted = 100`
    - `generated_ready_count = 100`
    - `generate_failures = 0`
- First live archive movement:
  - run `71236` manual, completed in about `20.22 s`
    - `archive_candidates = 100`
    - `archive_claimed = 100`
    - `archived_count = 100`
    - `archive_failures = 0`
- Purge validation:
  - initial purge run on the pre-fix build failed:
    - run `71248`
    - `purge_candidates = 50`
    - error: stale reference to dropped table `release_file_articles`
  - follow-up code fix applied in commit `016b1c1`
  - post-fix purge run completed:
    - run `71279` scheduled, completed in about `16.14 s`
    - `purge_candidates = 50`
    - `purged_count = 50`
    - `skipped_shared_lineage_rows = 0`
    - deleted rows:
      - `binaries = 604`
      - `binary_parts = 131649`
      - `article_headers = 131649`
      - `article_header_ingest_payloads = 131649`
      - `binary_archive_entries = 651`
      - `binary_inspections = 151`
      - `binary_inspection_artifacts = 118`
      - `binary_grouping_evidence = 85`
      - `binary_media_streams = 85`
      - `binary_par2_sets = 51`
      - `binary_par2_targets = 437`
- Refreshed backlog snapshot after live runs and purge fix:
  - `generate_nzb_pending_releases = 2941`
  - `archive_pending_releases = 100`
  - `archived_waiting_for_purge_releases = 191`
  - `purged_archived_releases = 50`
  - `blob_backed_archived_releases = 241`
- Stage throughput snapshot after the live baseline:
  - `release_generate_nzb`: `4` completed runs, `0` failed runs, `400` items processed, `6486.70 ms` average run duration
  - `release_archive_nzb`: `43` completed runs, `0` failed runs, `200` items processed, `821.40 ms` average run duration
  - `release_purge_archived_sources`: `40` completed runs, `4` failed runs, `50` items processed, `425.37 ms` average completed-run duration
- Interpretation:
  - live generation and archive movement are now confirmed under SQLite-backed runtime settings
  - purge also works on the current schema after removing the stale `release_file_articles` reference
  - the remaining non-zero archive and purge backlogs are expected because the scheduler continued processing new batches during measurement

### Sign-off: live storage baseline

- Measurement method:
  - archive blob-store bytes measured from `store/nzbs/indexer-archive`
  - archived object bytes measured from `SUM(release_archive_state.object_size_bytes)` over `purge_pending` and `purged`
  - PostgreSQL on-disk bytes measured from `pg_database_size('gonzb')`
  - note: PostgreSQL file size does not immediately shrink after deletes; physical reclaim still depends on vacuum/rewrite
- Before additional manual tail-stage passes:
  - archive filesystem bytes: `166,459,763`
  - archived object bytes tracked in Postgres: `166,459,763`
  - PostgreSQL database bytes: `242,758,903,475`
  - archived release count (`purge_pending` + `purged`): `841`
  - purged release count: `350`
  - purge-pending release count: `491`
- After additional live `generate -> archive -> purge` movement:
  - archive filesystem bytes: `186,291,361`
  - archived object bytes tracked in Postgres: `186,291,361`
  - PostgreSQL database bytes: `242,918,680,243`
  - archived release count (`purge_pending` + `purged`): `941`
  - purged release count: `418`
  - purge-pending release count: `523`
- Delta observed during the measurement window:
  - archive filesystem bytes: `+19,831,598`
  - archived object bytes tracked in Postgres: `+19,831,598`
  - PostgreSQL database bytes: `+159,776,768`
  - archived releases tracked: `+100`
  - purged releases completed: `+68`
- Interpretation:
  - blob-store growth is the cleanest direct measure of archival storage movement for this workflow
  - Postgres logical source-row deletion happened during the same window, but `pg_database_size` still increased because background ingest and MVCC table bloat dominate short-window physical file size changes
  - for “space reclaimed on disk” reporting, this workflow needs either per-table size snapshots plus vacuum cadence, or a dedicated reclaim maintenance measurement after purge

## Blob Storage Direction

### Chosen direction

Use one shared blob-storage implementation, but two separate logical stores:

- `indexer archive store`
- `aggregator cache store`

This means:

- shared blob client abstractions and code where practical
- separate configuration entries
- separate metadata ownership
- separate retention semantics
- separate object namespaces

Preferred deployment shape:

- local filesystem:
  - separate root directories
  - example:
    - `data/indexer-archive/`
    - `data/aggregator-cache/`
- S3-compatible object storage:
  - separate buckets by default
  - example:
    - `gonzb-indexer-archive`
    - `gonzb-aggregator-cache`

Fallback if one bucket must be used:

- allow one shared bucket only with strict namespace separation
- example:
  - `indexer/archive/...`
  - `aggregator/cache/...`
- still keep metadata and lifecycle policies separate

Why this is the best forward path:

- the indexer archive is durable and authoritative
- the aggregator cache is disposable and performance-oriented
- separate buckets or roots make retention policies, IAM, storage metrics, and cleanup much safer
- mixing immutable archived NZBs with cache objects in the same namespace increases the chance of accidental deletion or bad lifecycle rules

### Non-negotiable ownership rules

- Postgres remains the source of truth for indexer archival state and purge safety.
- SQLite remains optional local cache metadata only.
- Aggregator cache metadata must never decide whether indexer source rows are safe to purge.
- Aggregator cache eviction must never affect archived release availability.

## 1. Storage Model And Ownership

### Decisions

- Keep release catalog ownership in Postgres.
- Store authoritative archival metadata in Postgres next to the indexer catalog.
- Keep NZB payload bytes in blob storage using two logical stores:
  - indexer archive store
  - aggregator cache store
- Use separate filesystem roots or separate S3 buckets by default.
- Keep SQLite `blob_cache_index` only as an optional local cache mirror for aggregator cache state on filesystem-backed deployments.

### Action items

- Define Postgres archival metadata as part of the indexer catalog:
  - archival status
  - archive store identifier
  - blob object key
  - object store kind
  - content hash
  - object size
  - content encoding
  - archived-at
  - purge-eligible-at
  - purge-completed-at
  - last archival error
- Define aggregator cache metadata separately from archival metadata:
  - cache key
  - source kind
  - source release id or upstream identifier
  - cached blob location
  - cached-at
  - last-accessed-at
  - cache eviction status
- Reuse or extend `nzb_cache` only if it remains semantically clean.
  - If not clean, add dedicated archival tables in Postgres instead of overloading `nzb_cache`.
- Do not place purge-safety metadata in SQLite.
- Document that SQLite cache state is advisory only and cannot block or authorize purge.
- Document the boundary between:
  - authoritative archived NZB objects
  - optional aggregator-side hot cache objects

### Tasks

- Review current `nzb_cache` responsibilities and decide:
  - extend `nzb_cache`, or
  - add `release_archive_state` plus `release_archive_lineage`
- Define the recommended store configuration model rooted at `store.blob_dir`.
- Define a stable blob-key convention for archived local-indexer releases.
- Define a stable blob-key convention for aggregator cache entries.
- Define how blob-provider type and store identifier are stored so local FS and S3-compatible storage are both supported.

### Recommended object layout

Indexer archive objects:

- immutable once written
- preferred key example:
  - `releases/<provider_id>/<release_id>/<sha256>.nzb`
- acceptable alternative:
  - `release_id=<release_id>/sha256=<hash>/release.nzb`

Required properties:

- deterministic path derivable from release metadata
- hash-addressable verification
- no dependence on SQLite or node-local state

Aggregator cache objects:

- disposable
- keyed by effective NZB source identity, not only by release id
- example keys:
  - `source=indexer/release_id=<release_id>/sha256=<hash>.nzb`
  - `source=remote/provider=<provider>/guid=<guid>/sha256=<hash>.nzb`

Required properties:

- cache entries can be evicted without harming indexer correctness
- multiple upstream sources can coexist without key collision

## 1A. Module Interaction Workflow

### Indexer archival workflow

1. `release` forms a release and marks it `nzb-ready`.
2. `release_archive_nzb` claims the release.
3. The indexer generates or verifies the NZB bytes from the compact release catalog.
4. The indexer writes the NZB to the indexer archive store.
5. The indexer verifies:
   - object write succeeded
   - hash matches
   - stored metadata is complete
6. Postgres archival metadata is committed.
7. The release moves to `archived` and then `purge_pending`.
8. `release_purge_archived_sources` deletes heavy lineage only after the archive record is durable.

### Aggregator search workflow when indexer is a source

1. Aggregator calls the indexer search API or shared source adapter.
2. Indexer returns release metadata plus NZB availability metadata.
3. Search results do not require access to the deleted lineage tables.
4. Aggregator stores no purge-safety state for these results.

### Aggregator NZB retrieval workflow when indexer is a source

1. Aggregator receives a request for an indexer-backed release.
2. Aggregator checks its local cache first:
   - by source kind
   - by release id or upstream source identity
   - by known content hash where available
3. On cache hit:
   - aggregator serves cached bytes immediately
4. On cache miss:
   - aggregator fetches the NZB from the indexer source path
   - if the release is archived, the indexer serves from the authoritative archive store
   - if the release is still active, the indexer may still generate or serve from the live catalog path
5. Aggregator may write the fetched bytes into the aggregator cache store for later reuse.
6. Aggregator updates only cache metadata, not archive metadata.

### Why this interaction model is preferred

- the indexer remains the only authority on whether a release is archived and purge-safe
- the aggregator remains free to optimize for retrieval latency without inheriting retention responsibility
- a cache miss in aggregator never makes an archived release unavailable
- a cache eviction in aggregator never risks deleting authoritative archive objects

## 1B. Configuration Direction

### Decisions

Preferred configuration model:

- `store.blob_dir`

Internal layout:

- aggregator cache: `store.blob_dir`
- indexer archive: `store.blob_dir/indexer-archive`
- root directory or bucket
- optional prefix
- compression behavior
- retention policy
- access policy

### Action items

- Add separate config blocks for indexer archive and aggregator cache.
- Ensure the aggregator can cache indexer-sourced NZBs without sharing metadata tables with the indexer archive.
- Ensure the indexer can serve archived NZBs without depending on aggregator cache presence.

### Tasks

- Define local-FS config examples.
- Define S3-compatible config examples.
- Decide which deployment shapes are supported first:
  - local FS for both stores
  - local FS archive plus local FS cache
  - local FS archive with S3-compatible archive later
- Document that separate buckets are preferred over shared prefixes for S3-compatible deployments.

## 2. Release Lifecycle And Eligibility

### Decisions

A release becomes archive-eligible immediately when it is:

- local-indexer owned
- `nzb-ready`
- sufficiently inspected/enriched for the product’s ready state
- not actively being reprocessed
- successfully persisted to blob storage

Chosen default for “claimed”:

- “claimed” means internally claimed into blob-backed archived state
- it does not require a user or downloader to have fetched the NZB

Archived releases remain in the compact catalog indefinitely by default.

### Action items

- Add explicit release archival states:
  - `active`
  - `archive_pending`
  - `archived`
  - `archive_failed`
  - `purge_pending`
  - `purged`
- Define the exact release-ready gate for archival:
  - release formed
  - NZB generation succeeds
  - required inspect/enrich gates satisfied
  - release not currently under reform/reinspect/reenrich
- Freeze release archival inputs at archive time so purge scope is deterministic.

### Tasks

- Define what `nzb-ready` means operationally for this pipeline.
- Decide which inspect/enrich outputs are mandatory before archival.
- Define how later changes to an archived release are handled:
  - default: archived releases are immutable
  - if release changes before archival commit, abort and retry
- Define the archive retry policy for failed blob writes.

## 3. Archival Metadata And Purge Safety

### Decisions

Purge must be driven by durable lineage snapshots, not by re-deriving relationships later from mutable queue/read-model state.

The compact catalog retained after purge should include:

- `releases`
- `release_files`
- `release_newsgroups`
- release-level inspect/enrich rollups that are still useful after lineage deletion
- authoritative blob-backed NZB metadata

### Action items

- Add a release-scoped archival manifest or lineage snapshot in Postgres.
- Snapshot enough lineage to purge safely:
  - binary ids
  - article header ids
  - payload ids
  - grouping evidence ids
  - binary inspection ids or artifacts if they are purgeable
- Track whether any lineage rows are shared with non-archived releases before deletion.

### Tasks

- Define the minimum lineage snapshot shape needed to:
  - prove purge safety
  - support audit and debug
  - avoid keeping the whole source graph forever
- Decide whether purge logic deletes by archived lineage membership table or by archived-release foreign-key traversal plus shared-row checks.
- Define how to detect and skip shared lineage still needed by active releases.

## 4. Archival And Purge Stages

### Decisions

Split this work into dedicated late stages. Do not overload `release` or `release_summary_refresh`.

Required new stages:

- `release_archive_nzb`
- `release_purge_archived_sources`

### Action items

- `release_archive_nzb`
  - claim `archive_pending` releases
  - generate or verify NZB
  - write NZB to the indexer archive store
  - persist archival metadata and lineage snapshot
  - mark release `archived` or `purge_pending`
- `release_purge_archived_sources`
  - claim `purge_pending` archived releases
  - delete eligible heavy lineage rows
  - skip rows still shared with active releases
  - mark release `purged`

### Tasks

- Add runtime stage config for both stages.
- Add supervisor stage names and API/frontend stage visibility.
- Define whether the aggregator needs any cache-warm or cache-prefetch hooks for newly archived indexer releases.
- Define stage metrics:
  - archive candidates
  - archived count
  - archive failures
  - purge candidates
  - purge count
  - skipped shared-lineage rows
  - rows deleted by table
  - duration and retry metrics
- Define ordering in pipeline and supervisor:
  - `release` stays formation-only
  - archival runs after release readiness is stable
  - purge runs after successful archival

## 5. Purge Scope By Table

### Decisions

The real storage win comes from purging source lineage, not just keeping NZBs out of Postgres.

Primary purge targets:

- `binaries`
- `binary_parts`
- `article_headers`
- `article_header_ingest_payloads`
- `binary_grouping_evidence`
- binary-scoped inspection rows or artifacts no longer needed after archival
- stale queue or read-model rows tied only to archived releases

Retained compact catalog:

- `releases`
- `release_files`
- `release_newsgroups`
- archival metadata
- compact NZB metadata or blob key metadata

### Action items

- Define exact delete order to satisfy FK and ownership constraints.
- Define whether any binary-scoped inspection artifacts should be retained as compact release/file rollups before source purge.
- Ensure purged releases still support:
  - search
  - release detail view
  - NZB download
- Ensure purged releases can still be served to the aggregator through the archive store without touching deleted lineage.

### Tasks

- Map all tables that reference binary/article lineage for local-indexer releases.
- Decide which inspection artifacts are copied upward before purge.
- Define whether archive metadata needs enough information for cross-module tracing:
  - source module
  - archive store
  - object key
  - object hash
- Define cleanup for queue/work tables:
  - `release_family_readiness_summaries`
  - `release_family_summary_refresh_queue`
  - `yenc_recovery_work_items`
- Define `purge complete` criteria for a release.

## 6. Dashboard, Visibility, And Maintenance

### Decisions

Operators need explicit visibility into archive and purge backlog, plus storage pressure.

### Action items

Add dashboard/backlog stats for:

- archive-pending releases
- archived releases waiting for purge
- purged archived releases
- blob-backed archived release count
- aggregator cache object count
- aggregator cache bytes
- indexer archive bytes
- per-table reclaim candidate bytes
- current DB size
- top-table bytes
- dead tuple ratios on major tables

Add stage throughput visibility for:

- `release_archive_nzb`
- `release_purge_archived_sources`

### Tasks

- Define dashboard stat keys and labels.
- Define stage command hints shown in UI/admin dashboard.
- Add storage-oriented maintenance views for:
  - current DB size
  - current archive-store size
  - current aggregator-cache size
  - reclaim candidates
  - purge throughput
- Add alert thresholds for:
  - DB volume free space
  - archive backlog growth
  - purge backlog growth

## 7. Physical Reclaim Strategy

### Decisions

Plain `VACUUM` is not enough to return bytes to the OS.

Long-term reclaim should use rewrite-based maintenance after logical purge:

- prefer `pg_repack` or equivalent online rewrite where possible
- use `VACUUM FULL` only for bounded/manual maintenance windows
- use `TRUNCATE` for pure work-queue tables only when safe

### Action items

- Add a maintenance playbook for reclaim after purge waves.
- Prioritize reclaim by real dead-space payoff, not just table size.
- Avoid rewriting giant low-bloat tables unless justified.

### Tasks

- Define reclaim priority order after archival rollout:
  - `article_header_ingest_payloads`
  - `release_family_readiness_summaries`
  - `article_headers`
  - `binaries`
  - only then others as justified
- Define minimum free-space requirements before rewrite operations.
- Define when to use:
  - ordinary `VACUUM`
  - `VACUUM FULL`
  - `pg_repack`
  - `TRUNCATE`

## 8. Testing And Acceptance Criteria

### Action items

Add tests for:

- archival eligibility from `nzb-ready` releases
- successful blob-backed archival
- failed archival with retryable state
- immutable archived compact catalog behavior
- safe purge of unique lineage
- no over-delete when lineage is shared with active releases
- continued NZB fetch from blob storage after purge
- aggregator cache miss then fill from archived indexer source
- aggregator cache hit after prior fill
- aggregator cache eviction without affecting archived release availability
- dashboard/archive/purge backlog visibility
- runtime stage config and frontend stage exposure

### Acceptance criteria

- a blob-backed archived release remains searchable and downloadable after purge
- archived releases no longer require `binaries` or `article_headers` for normal UI/API NZB fetch behavior
- aggregator can serve indexer-backed NZBs from cache when available and from the indexer archive path when cache is cold
- purge materially reduces lineage row counts in the heavy source tables
- rewrite-based reclaim can return disk to the OS after purge
- archival/purge stages are restart-safe and idempotent

## 9. Immediate Execution Backlog For This Sprint

### Section A: Catalog and metadata design

- Decide whether to extend `nzb_cache` or add dedicated archival tables.
- Define archival state machine and blob-key metadata.
- Define lineage snapshot schema.
- Define indexer archive metadata versus aggregator cache metadata boundaries.

### Section B: Stage implementation design

- Specify `release_archive_nzb` stage behavior.
- Specify `release_purge_archived_sources` stage behavior.
- Specify runtime settings, supervisor stages, and UI/API exposure.
- Specify the archived NZB serving path used by aggregator fetches.

### Section C: Purge safety design

- Map all lineage tables and dependencies.
- Define shared-lineage safety checks.
- Define compact catalog fields retained after purge.

### Section D: Maintenance and storage control

- Define dashboard stats and backlog metrics.
- Define reclaim playbook and disk-space thresholds.
- Define operational order for purge then reclaim.
- Define archive-store versus cache-store retention and cleanup rules.

## Assumptions And Defaults

- Authoritative archival metadata belongs in Postgres, not SQLite.
- SQLite blob metadata remains optional local cache state only.
- Archival eligibility is immediate upon blob-backed `nzb-ready` success, not age-based by default.
- Archived releases remain in the compact catalog indefinitely by default.
- Reinspection/reformation of purged archived releases is out of scope unless a future rehydration feature is explicitly designed.
- Preferred deployment uses separate logical stores, with separate buckets for S3-compatible storage and separate root directories for local filesystem storage.
