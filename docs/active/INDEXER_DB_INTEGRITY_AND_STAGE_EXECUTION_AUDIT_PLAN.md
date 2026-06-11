# Indexer DB Integrity And Stage Execution Audit Plan

Snapshot date: 2026-06-11

This is the active execution guide for the database-integrity follow-up, full stage/DBO audit, and stage-execution hardening workstream.

Use this doc together with:

- `docs/INDEXER_CURRENT_SCHEMA_AND_SYSTEM_INTERACTIONS.md` for the living ownership matrix and stage/table interaction rules
- `docs/active/INDEXER_FOUNDATION_DOCS.md` for current routing of active versus archived docs

## Summary

The audit goal is no longer just “understand the corruption incident.” It is now:

- prove which write/query paths are safe enough for a stable release
- document how every major stage reaches the database
- identify query, lock, and overlap risks in execution order
- freeze schema only after the hot-path audit shows no remaining structural gaps

Working decisions already locked:

- do not switch the product to a permanently sequential global pipeline
- keep the long-term execution model concurrent where hot-table ownership is disjoint
- treat `scrape_*` as the highest-risk canonical writer and isolate it during bootstrap/recovery
- prefer stage overlap gates and phased runtime profiles over introducing multi-process topology first

## Current Live Bootstrap Status

Validated on `2026-06-11` against a clean database:

- `indexer maintenance check-integrity --ensure-extension` passed for:
  - `article_headers_pkey`
  - `article_headers_newsgroup_id_article_number_key`
  - `article_headers_newsgroup_id_message_id_key`
- scrape-only bootstrap is the active rebuild profile
- `indexer scrape latest` and `indexer scrape backfill` were launched in CLI mode, not via full `serve`
- concurrent scrape-only execution is currently healthy on the fresh DB

Observed live behavior after the restart:

- `scrape_latest` resumed completing runs after the first bad historical run
- `scrape_backfill` started cleanly and began inserting 20k-row ranges
- `article_headers` moved past the initial bootstrap threshold and is continuing to grow under concurrent scrape-only load

Fresh issue found and fixed during bootstrap:

- some NNTP strings included embedded NUL bytes
- Go still treats those strings as valid UTF-8, so the old sanitizer let them through
- PostgreSQL then rejected poster inserts with:
  - `ERROR: invalid byte sequence for encoding "UTF8": 0x00`
- fix applied:
  - strip `\x00` in `sanitizeUTF8()` before any header/payload/poster DB write
  - add regression coverage on `InsertArticleHeaders`

Operational guidance until the next phase:

- keep building header backlog with CLI scrape-only commands first
- do not enable assemble/recover/release stages until the scrape-only bootstrap has accumulated a meaningful backlog and remains stable over a longer soak window

## Audit Execution Order

Audit stages in this exact order:

1. `scrape_latest` / `scrape_backfill`
2. `assemble_lane_a` / `assemble_lane_b`
3. `recover_yenc`
4. `release_summary_refresh`
5. `release`
6. `inspect_discovery`, `inspect_par2`, `inspect_nfo`, `inspect_archive`, `inspect_password`, `inspect_media`
7. `release_generate_nzb`, `release_archive_nzb`, `release_purge_archived_sources`
8. `indexer_maintenance`, integrity/admin/runtime/stats support queries

This order is intentional:

- it follows upstream fact creation to downstream materialization
- it lets later audits assume upstream ownership is already mapped
- it forces the highest-risk write paths to be audited first
- it keeps release/inspection findings grounded in the ingest and assembly truth they depend on

## Required Audit Method Per Stage

Every stage audit must cover three layers.

### 1. Service layer

For each stage service:

- entrypoint shape (`RunOnce`, `RunOnceWithMetrics`, scheduler loop)
- batch/concurrency/backoff controls
- repo/store interface methods invoked
- current metrics emitted
- missing metrics needed to reason about throughput or contention

### 2. Store / DBO layer

For every hot store/repository method the stage uses:

- SQL shape
- transaction scope
- tables touched
- expected index path
- conflict/locking behavior
- whether it is:
  - canonical fact write
  - derived/materialized write
  - queue/runtime write
  - read-only operational query
  - cleanup/purge query

### 3. Schema / overlap layer

For each hot method, record:

- owning stage
- allowed overlapping stages
- forbidden overlapping stages
- runtime profile classification:
  - bootstrap-only
  - build/regroup
  - steady-state-safe
- whether it assumes a prior upstream stage has already completed

## Stage Audit Checklists

### Pass 1: Scrape ingest and checkpointing

Audit:

- provider validation and scrape-group selection
- latest-range selection
- backfill-range selection
- `InsertArticleHeaders`
- poster-batch writes
- `article_header_ingest_payloads` writes
- checkpoint updates, especially `UpsertBackfillCheckpoint`
- integrity preflight

Required outputs:

- exact list of scrape-owned tables and hot indexes
- conflict/write pattern for header ingest
- duplicate-resolution behavior
- whether any support-table writes should be further reduced or isolated
- final statement of which scrape queries are bootstrap-safe only versus steady-state-safe

### Pass 2: Assemble

Audit:

- header selection/claim path
- `UpsertBinaries`
- `UpsertBinaryParts`
- binary refresh/requeue behavior
- grouping evidence writes
- lane A vs lane B selector differences

Required outputs:

- exact difference in intent and query behavior between lane A and lane B
- hottest indexes on `article_headers`, `binaries`, and `binary_parts`
- remaining redundant lookups or write-backs
- whether scrape/assemble overlap is structurally unsafe or just operationally discouraged

### Pass 3: Recover yEnc

Audit:

- `BackfillYEncRecoveryWorkItems`
- `ListYEncRecoveryCandidates`
- hot queue vs seed/backfill path
- stale/backoff/noop handling
- persistence of recovered identity into `binaries` / `binary_parts`

Required outputs:

- exact distinction between queue-first and seed/backfill query paths
- overlap rules with assemble and release refresh
- whether candidate selection is inferential or fully materialized
- which joins are truly required and which are legacy/transitional

### Pass 4: Release summary refresh

Audit:

- queue claim/dequeue logic
- Phase A summary recompute
- Phase B ready-candidate materialization
- recovered-file-set follow-up work
- cleanup interactions with maintenance

Required outputs:

- exact DBO function list for Phase A and Phase B
- query shapes for:
  - key selection
  - family aggregate recompute
  - dominant row selection
  - ready-candidate sync
- overlap rules with `release`, `recover_yenc`, and maintenance
- explicit identification of batch-size sensitive vs scan-shape sensitive work

### Pass 5: Release formation

Audit:

- `ListReleaseCandidates`
- `ListBinariesForReleaseCandidate`
- title candidate reads
- `UpsertRelease`
- `ReplaceReleaseFiles`
- `ReplaceReleaseNewsgroups`
- ready-candidate ack behavior
- auxiliary sibling cleanup

Required outputs:

- exact split between candidate selection and release persistence
- proof of current cross-newsgroup release behavior in code
- confirmation that release is not writing upstream fact state for orchestration
- final overlap rules with refresh and inspect

### Pass 6: Inspect stages

Audit each inspection stage separately:

- `inspect_discovery`
- `inspect_par2`
- `inspect_nfo`
- `inspect_archive`
- `inspect_password`
- `inspect_media`

Required outputs:

- owned evidence/runtime tables per stage
- candidate listing and claim strategy
- whether any stage still writes fields that should belong to assemble/recover only
- overlap-safe vs overlap-risky inspection combinations

### Pass 7: Archive / NZB / purge tail

Audit:

- NZB generation reads/writes
- archive status writes
- purge selection and deletes
- transitional archive/NZB support surfaces

Required outputs:

- exact terminal cleanup contract
- confirmation that purge is the only intentional downstream mutator of upstream lineage
- list of transitional tables that can remain frozen for `0.8` versus ones still needing redesign

### Pass 8: Maintenance / runtime / admin

Audit:

- maintenance cleanup queries
- integrity tooling
- dashboard/stats queries
- runtime settings state and serve/scheduler support reads

Required outputs:

- which stats surfaces are misleading versus true operational queues
- which maintenance queries are safe but should be deferred under backlog
- final inputs for steady-state `serve` overlap policy

## Known Primary DBO Entry Points By Stage

These are the baseline methods the audit should start from. The audit may expand from here, but it should not skip these.

### Scrape

- `InsertArticleHeaders`
- `UpsertBackfillCheckpoint`
- scrape checkpoint update helpers in `repository.go`
- integrity guard path:
  - `CheckCriticalIndexerIntegrity`

### Assemble

- `UpsertBinaries`
- `UpsertBinaryParts`
- binary refresh/update helpers in `assembly_store.go`

### Recover yEnc

- `BackfillYEncRecoveryWorkItems`
- `ListYEncRecoveryCandidates`
- recovery result persistence methods in `yenc_recovery_store.go`

### Release summary refresh

- `RefreshQueuedReleaseFamilySummaries`
- `RefreshQueuedReleaseFamilySummariesWithMetrics`
- Phase A/Phase B helpers inside `release_family_summary_store.go`

### Release

- `ListReleaseCandidates`
- `ListBinariesForReleaseCandidate`
- `UpsertRelease`
- `ReplaceReleaseFiles`
- `ReplaceReleaseNewsgroups`

### Inspect

- `ListBinaryInspectionCandidates`
- `ListBinaryInspectionCandidatesWithOptions`
- `ClaimBinaryInspectionCandidates`
- stage-specific evidence upsert/update methods in `inspection_store.go`

### Archive / purge

- `MarkReleaseArchiveStored`
- `MarkReleaseArchiveFailed`
- `PurgeArchivedReleaseSources`

## Documentation Deliverables

### `docs/INDEXER_CURRENT_SCHEMA_AND_SYSTEM_INTERACTIONS.md`

Expand the living doc during the audit so each stage section includes:

- primary DBO/store functions
- tables and indexes touched
- allowed writes
- forbidden writes
- overlap policy
- runtime profile classification

Do not turn it into a file inventory. Name functions only where they are needed to pin down a critical hot path.

### Audit findings capture

For each pass, record:

- what was audited
- what is safe as-is
- what should change
- whether the issue is:
  - query shape
  - transaction scope
  - overlap policy
  - schema gap
  - observability gap

## Commit Strategy

Commit after each meaningful audit/documentation slice:

1. scrape audit + doc updates
2. assemble + recover_yenc audit + doc updates
3. release_summary_refresh + release audit + doc updates
4. inspect/archive/maintenance/runtime audit + doc updates

If a code change is required by the same audit finding, keep it separate from the doc-only commit unless the doc would be incorrect without the code.

## Acceptance Criteria

- every major stage has an explicit audit checklist
- the audit order is fixed and implementation-safe
- the living schema doc is the source of truth for stage overlap, ownership, and hot DBO entry points
- the next engineer can run the audit without re-deciding what order or depth to use
- the schema remains freeze-targeted, but not frozen, until all audit passes are complete and no unresolved structural gap remains
