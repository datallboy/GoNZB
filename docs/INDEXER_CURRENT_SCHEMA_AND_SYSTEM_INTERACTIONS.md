# Indexer Current Schema And System Interactions

Snapshot date: 2026-06-11

This document is the living reference for indexer schema ownership, stage boundaries, and allowed system interactions.

Use this doc before:

- adding a new stage write path
- changing purge behavior
- moving columns between release-side tables
- dropping or shrinking hot tables
- adding runtime/state bookkeeping to an existing fact table

This document is intentionally current and enforceable. Older schema audits and completed storage-trim plans remain useful as historical reference, but they do not override the ownership rules here.

Related reference docs:

- `docs/archive/completed/indexer/2026-05-14-indexer-database-growth-trim/INDEXER_DATABASE_SCHEMA_AUDIT.md`
- `docs/archive/completed/indexer/2026-05-14-indexer-database-growth-trim/INDEXER_DATABASE_GROWTH_TRIM_PLAN.md`
- `docs/active/INDEXER_NZB_ARCHIVAL_AND_SOURCE_PURGE_PLAN.md`
- `docs/active/INDEXER_DB_INTEGRITY_AND_STAGE_EXECUTION_AUDIT_PLAN.md`

## How To Use This Doc

Read this doc in this order:

1. Ownership rules
2. Table ownership matrix
3. Stage-by-stage allowed reads and writes
4. Forbidden write-backs
5. Purge contract
6. Migration path

If a proposed code change violates the rules here, the default answer is to change the design, not to add another exception.

## Ownership Rules

These rules are the working contract for the indexer.

### Rule 1: one canonical writer per table

Every hot table must have one primary owning stage or subsystem.

- Other stages may read it.
- Other stages may enqueue work derived from it.
- Other stages should not directly mutate it unless this doc explicitly permits the exception.

### Rule 2: downstream stages read upstream facts, not rewrite them

Downstream stages should consume upstream fact tables and write only:

- their own fact tables
- their own queue/work tables
- their own derived/materialized tables
- final release-catalog tables they explicitly own

What we want to avoid:

- downstream stages updating upstream facts only to record progress
- downstream stages recomputing shared derived tables inline
- many stages touching the same row family with different lock order

### Rule 3: shared derived tables get one heavy materializer

If a table is a shared derived read-model, one stage owns materialization.

Current example:

- `release_summary_refresh` is the only heavy writer of `release_family_readiness_summaries`

Other stages may:

- enqueue dirty keys
- acknowledge their own work if the write is narrow and explicitly allowed

Other stages may not:

- bulk recompute readiness summaries inline
- lock readiness rows across large fan-out batches

### Rule 4: stage runtime bookkeeping belongs in runtime/work tables

Operational state should live in dedicated runtime surfaces, not in upstream fact rows.

Examples:

- stage run state
- retry/backoff state
- reservation rows
- queue state
- transient eligibility markers

Scrape/newsgroup control state follows the same rule.

- explicit scrape groups are runtime settings state
- wildcard rules are runtime settings state
- provider group inventory is discovery state owned by the scrape configuration subsystem, not by scrape ingest tables
- materialized wildcard groups are runtime-derived control state owned by scrape configuration, not raw provider inventory
- `indexing.newsgroups` and `indexing.backfill_until_date_by_group` are compatibility mirrors of effective scrape state during transition

### Rule 5: purge is the only intentional downstream mutator of upstream source facts

`release_purge_archived_sources` is allowed to delete upstream source rows, but only as terminal cleanup after archive eligibility is satisfied.

Purge is not a general exception for write-back. It is the cleanup owner for rows that are no longer needed by the live pipeline.

### Rule 6: release catalog data is durable, source lineage is temporary

The long-term model is:

- `releases` and durable release-catalog tables stay enrichable and queryable
- article/binary/grouping lineage exists to build and validate releases
- once archive/NZB durability and required inspection are complete, temporary lineage can be purged

## Locking And Contention Guidance

Normal PostgreSQL reads are not the main contention problem.

- plain `SELECT` uses MVCC and does not conflict with normal writes the way row-locking reads do
- `AccessShareLock` on table reads is not the same class of problem as row-level write contention

The dangerous operations are:

- `UPDATE`
- `DELETE`
- `INSERT ... ON CONFLICT` against shared hot rows
- `SELECT ... FOR UPDATE`
- `SELECT ... FOR SHARE` or `FOR KEY SHARE` when overused on hot rows
- explicit `LOCK TABLE`
- foreign key checks and index maintenance under heavy concurrent write load
- cross-stage write-backs to the same derived row family

Policy implication:

- upstream reads are acceptable
- shared row families should not have multiple heavy writers
- if a stage needs to signal downstream work, prefer a queue row over mutating upstream facts

## Execution Model Guidance

The preferred long-term runtime model remains concurrent stages with strict ownership boundaries and explicit overlap rules.

We do not assume that one global sequential pipeline is the target model. Instead:

- concurrent stages are allowed when they write different hot row/index families
- high-risk overlapping writers should be gated or staggered through runtime policy
- `scrape_*` is the highest-risk canonical writer and should be isolated during fresh-database bootstrap or integrity recovery

### Runtime profiles

#### Bootstrap / fresh database

Allowed:

- `scrape_latest`
- `scrape_backfill`

Held back:

- `assemble_*`
- `recover_yenc`
- `release_summary_refresh`
- `release`
- inspect stages
- archive / NZB tail stages

Validated current bootstrap posture:

- prefer CLI stage commands over full `serve` during first ingest on a fresh DB
- `scrape_latest` and `scrape_backfill` may run together during bootstrap
- keep every non-scrape stage disabled until scrape-only backlog is established and integrity remains clean

#### Build / regroup

Allowed:

- `assemble_lane_a`
- `assemble_lane_b`
- `recover_yenc`
- `release_summary_refresh`
- `release`

Held back:

- `scrape_*`
- inspect stages
- archive / NZB tail stages

#### Steady state

Allowed:

- concurrent operation only where the overlap is proven safe for the hot tables involved
- inspect and archive-tail stages after release formation is healthy

Guidance:

- `scrape_*` should not overlap with the hottest regroup/materialization stages by default
- prerequisite and stage-gate policy is preferred over ad hoc operator choreography

### Integrity guardrail

Before scrape writes to `article_headers`, critical ingest indexes must pass the current integrity preflight.

Current protected relations:

- `article_headers_pkey`
- `article_headers_newsgroup_id_article_number_key`
- `article_headers_newsgroup_id_message_id_key`

If those checks fail, scrape should idle/fail fast rather than continue applying write pressure to a damaged cluster.

Additional ingest guardrail:

- all NNTP text fields written through scrape must strip embedded NUL bytes before any PostgreSQL text insert/upsert path
- this applies at minimum to message IDs, subjects, posters, and xref text

## Table Ownership Matrix

This matrix is the schema contract for current and near-term code changes.

| Table / Surface | Type | Primary Owner | Other Allowed Writers | Notes |
| --- | --- | --- | --- | --- |
| `article_headers` | canonical fact | `scrape_*` | none | Durable ingest fact row per article. |
| `article_header_ingest_payloads` | work/support | `scrape_*` | `recover_yenc` for bounded retry/support state only | Transitional table; should trend toward less mixed ownership. |
| `scrape_checkpoints` | runtime/work | `scrape_*` | none | Canonical latest/backfill cursor and cutoff state per provider/newsgroup. |
| `scrape_runs` | runtime/work | `scrape_*` | `indexer_maintenance` stale-run cleanup only | Scrape run history and current running/completed/failed state. |
| `posters` | support dimension | scrape ingest path today | `assemble_*` via `EnsurePoster` | Shared support dimension; ownership is transitional and should be minimized in later audits. |
| `binaries` | canonical fact | `assemble_*` | `recover_yenc`, `inspect_*` for explicit identity/refinement fields only | Current canonical binary identity. |
| `binary_parts` | canonical fact | `assemble_*` | `recover_yenc` merge/refinement only | Canonical article-to-binary membership bridge. |
| `binary_grouping_evidence` | derived/audit | `assemble_*` | none | Bounded audit/evidence surface. |
| `yenc_recovery_work_items` | queue/work | `recover_yenc` | `assemble_*` seed only | Recovery-owned work queue. |
| `binary_inspections` | queue/work | `inspect_*` | none | Inspection stage tracking only. |
| `binary_archive_entries` | derived/evidence | `inspect_archive` | none | Archive evidence owned by archive inspection. |
| `binary_media_streams` | derived/evidence | `inspect_media` | none | Media evidence owned by media inspection. |
| `binary_text_evidence` | derived/evidence | `inspect_nfo` | none | Text evidence owned by text inspection. |
| `binary_par2_sets` | derived/evidence | `inspect_par2` | none | PAR2 evidence owned by par2 inspection. |
| `binary_par2_targets` | derived/evidence | `inspect_par2` | none | PAR2 target mapping owned by par2 inspection. |
| `binary_password_candidates` and similar password evidence | derived/evidence | `inspect_password` | none | Password evidence owned by password inspection. |
| release-family refresh queue tables | queue/work | `release_summary_refresh` | any stage may enqueue dirty keys through repository helpers | Queue fan-in is allowed; summary materialization is not. |
| `release_family_readiness_acks` | queue/work | `release` | none | Release-owned ack state for consumed readiness keys. |
| `release_family_readiness_summaries` | derived/materialized read-model | `release_summary_refresh` | none | Shared hot table; one heavy writer only. |
| `releases` | durable catalog fact | `release` | `inspect_*`, enrichment, overrides, archive tail for explicit catalog/archive fields only | Permanent release catalog header. |
| `release_catalog_files` | durable catalog fact | `release` | archive-maintenance/backfill only | Durable UI/detail file metadata. |
| `release_files` | transitional source/detail bridge | `release` | purge deletes only | Transitional and should shrink over time. |
| `release_newsgroups` | durable catalog support | `release` | purge deletes only if replaced by another durable source | Current release provenance/catalog support. |
| `release_archive_state` | durable archive state | archive tail | none | Blob/archive lifecycle state. |
| `release_archive_detail_*` tables | frozen transitional archive detail | none for active runtime flows | none | Legacy compatibility surface; no longer part of the active detail or maintenance path. |
| `nzb_cache` | transitional runtime/archive support | generate/archive tail | purge deletes | Transitional and should continue shrinking in importance. |
| enrichment and override tables | durable catalog support | enrichment / override subsystem | none | Catalog-facing metadata ownership. |
| stage runtime / run history tables | runtime/work | supervisor/runtime subsystem | none | Must not be mixed into fact rows. |

## Stage-By-Stage Allowed Reads And Writes

This section defines what each stage may read and write.

## Stage DBO Audit Map

This section is the living audit companion to the ownership matrix. It does not replace the detailed implementation audit, but it does name the primary hot DBO/store entry points and the expected execution profile for each stage.

Use this section when:

- auditing a stage’s query paths
- deciding whether a query belongs in bootstrap, build/regroup, or steady state
- checking whether a stage is operating through queue/runtime state versus direct fact-table mutation

### Scrape audit map

Primary hot DBO/store paths:

- `InsertArticleHeaders`
- scrape checkpoint update helpers in `repository.go`
- `UpsertBackfillCheckpoint`
- `CheckCriticalIndexerIntegrity`

Execution profile:

- bootstrap-safe
- not allowed to overlap with the hotter regroup/materialization stages by default

Audit focus:

- `INSERT ... ON CONFLICT` pressure on `article_headers`
- support-table writes into `article_header_ingest_payloads`
- poster dimension writes
- duplicate-resolution and follow-up reads
- checkpoint write frequency and lock scope

### Assemble audit map

Primary hot DBO/store paths:

- `UpsertBinaries`
- `UpsertBinaryParts`
- binary refresh/update helpers in `assembly_store.go`
- release-refresh queue enqueue helpers called from assemble

Execution profile:

- build/regroup
- unsafe to overlap with scrape by default until the full ingest/assembly contention story is proven safe

Audit focus:

- header selection/claim path
- binary upsert chunking and conflict behavior
- `binary_parts` write amplification
- lane A versus lane B selector/query differences
- downstream dirty-key enqueue behavior

### Recover yEnc audit map

Primary hot DBO/store paths:

- `BackfillYEncRecoveryWorkItems`
- `ListYEncRecoveryCandidates`
- recovery result persistence helpers in `yenc_recovery_store.go`

Execution profile:

- build/regroup
- may overlap with assemble and release refresh only where the work-queue and refinement paths are shown to be safe

Audit focus:

- queue-first versus seed/backfill selection
- stale/noop/backoff behavior
- joins against transient support tables
- refinement writes into `binaries` / `binary_parts`
- downstream dirty-key enqueue behavior

### Release summary refresh audit map

Primary hot DBO/store paths:

- `RefreshQueuedReleaseFamilySummaries`
- `RefreshQueuedReleaseFamilySummariesWithMetrics`
- Phase A and Phase B helpers in `release_family_summary_store.go`

Execution profile:

- build/regroup
- may overlap with `release` only through the documented queue/materialized boundaries
- should defer or coordinate with maintenance cleanup on the same derived tables

Audit focus:

- queue claim/dequeue shape
- Phase A aggregate/dominant-row queries
- Phase B ready-candidate materialization
- recovered-file-set follow-up work
- cleanup and maintenance interaction

### Release audit map

Primary hot DBO/store paths:

- `ListReleaseCandidates`
- `ListBinariesForReleaseCandidate`
- `UpsertRelease`
- `ReplaceReleaseFiles`
- `ReplaceReleaseNewsgroups`

Execution profile:

- build/regroup, then steady-state-safe once upstream readiness production is stable

Audit focus:

- ready-candidate selection versus persistence split
- release file/newsgroup replacement transaction scope
- cross-newsgroup release behavior
- release-ready ack and candidate consumption behavior

### Inspect audit map

Primary hot DBO/store paths:

- `ListBinaryInspectionCandidates`
- `ListBinaryInspectionCandidatesWithOptions`
- `ClaimBinaryInspectionCandidates`
- stage-specific evidence upsert/update helpers in `inspection_store.go`

Execution profile:

- steady state after release formation is healthy

Audit focus:

- candidate listing and claim shape
- owned evidence tables per inspect stage
- whether any inspect path still mutates upstream fact state unnecessarily
- reservation/runtime-state isolation

### Archive / purge audit map

Primary hot DBO/store paths:

- `MarkReleaseArchiveStored`
- `MarkReleaseArchiveFailed`
- `PurgeArchivedReleaseSources`

Execution profile:

- steady state only after release formation and inspection/archive prerequisites are healthy

Audit focus:

- durable archive state writes
- transitional `nzb_cache` dependence
- purge eligibility gating
- delete order and shared-lineage safety

### Maintenance / runtime audit map

Primary hot DBO/store paths:

- maintenance cleanup queries in `maintenance_store.go`
- integrity tooling in `integrity_store.go`
- stats/dashboard query surfaces in `inspect_reads.go` and related read models

Execution profile:

- steady state, but some maintenance cleanup should defer while higher-priority rebuild/backlog work is active

Audit focus:

- cleanup overlap with release summary refresh
- integrity preflight coverage
- misleading queue/stat surfaces
- runtime policy and scheduler support queries

### `scrape_latest` and `scrape_backfill`

Allowed reads:

- provider state
- provider group inventory and effective scrape-group control state
- newsgroup bounds
- runtime/stage config

Allowed writes:

- `article_headers`
- `article_header_ingest_payloads`
- provider progress/runtime bookkeeping

Not allowed:

- writing `binaries`
- writing release-side catalog tables
- writing readiness summaries

Rationale:

- scrape owns ingest fact creation only
- wildcard evaluation and provider inventory do not bypass effective scrape-group selection

Primary DBO entry points:

- `StartScrapeRun`
- `FinishScrapeRun`
- `GetLatestCheckpoint`
- `UpsertLatestCheckpoint`
- `GetBackfillCheckpoint`
- `GetBackfillCheckpointState`
- `HasBackfillCutoffReachedForGroup`
- `SetBackfillCheckpointState`
- `InsertArticleHeaders`
- scrape checkpoint update helpers in `repository.go`
- `UpsertBackfillCheckpoint`
- `CheckCriticalIndexerIntegrity`

Current audit note:

- the scrape metric/log field `articles_inserted` currently reflects resolved/processed headers through the ingest path, not guaranteed newly unique `article_headers` rows
- `InsertArticleHeaders` duplicate resolution must remain split by article-number and message-id match branches; combining them into one `OR` join defeats the unique indexes and forces a broad scan at scale
- `scrape_runs` is not a sufficient source by itself to distinguish latest versus backfill mode; operator-facing mode reporting must continue to use stage/runtime surfaces, not only scrape run history

## Scrape Configuration Ownership

The scrape configuration subsystem owns four distinct state classes:

1. explicit configured groups
2. wildcard rules
3. provider-discovered group inventory
4. materialized effective wildcard groups

Ownership rules:

- provider inventory is discovery data only and must not directly imply scrape selection
- wildcard rules are global across configured indexer providers
- wildcard refresh is manual through explicit scan/rescan plus preview/apply
- scrape stages consume only the effective group list derived from explicit groups plus enabled materialized groups
- saving zero effective groups is valid; scrape stages should idle rather than force persistence failure

## Cross-Group Release And File Rules

Cross-group release formation is intentionally asymmetric:

- one logical release may accumulate provenance from multiple groups
- `release_newsgroups` should retain all contributing groups for that release
- release/catalog duplication should be suppressed when identity is strong enough
- release/file-set availability may union across groups
- one file payload’s article membership must remain bound to one newsgroup

Downloader-safety invariant:

- NZB generation must emit the newsgroup that belongs to that file payload’s binary provenance
- article sets for a single file must not mix articles from different newsgroups even when the surrounding release is multi-group

### `assemble_lane_a` and `assemble_lane_b`

Allowed reads:

- `article_headers`
- `article_header_ingest_payloads`
- runtime/stage config

Allowed writes:

- `binaries`
- `binary_parts`
- `binary_grouping_evidence`
- `yenc_recovery_work_items` seeding
- release-family refresh queue enqueue only

Not allowed:

- mutating `article_headers`
- bulk recomputing `release_family_readiness_summaries`
- mutating release catalog rows to show assembly progress

Rationale:

- assemble turns article facts into binary identity and membership

Primary DBO entry points:

- `UpsertBinaries`
- `UpsertBinaryParts`
- binary refresh/update helpers in `assembly_store.go`

### `recover_yenc`

Allowed reads:

- `article_headers`
- `article_header_ingest_payloads`
- `binary_parts`
- `binaries`
- `yenc_recovery_work_items`

Allowed writes:

- `yenc_recovery_work_items`
- bounded refinement fields on `binaries`
- merge/refinement updates on `binary_parts` where identity changes require it
- release-family refresh queue enqueue only

Not allowed:

- mutating `article_headers`
- materializing readiness summaries directly
- writing release catalog rows to reflect recovery progress

Rationale:

- recovery may improve canonical binary identity, but it does not own ingest facts or release materialization
- recovery work selection should stay bounded and group-fair; queue seeding and candidate claims should not let one large newsgroup backlog starve all other eligible groups indefinitely

Primary DBO entry points:

- `BackfillYEncRecoveryWorkItems`
- `ListYEncRecoveryCandidates`
- recovery result persistence helpers in `yenc_recovery_store.go`

### `inspect_discovery`, `inspect_par2`, `inspect_nfo`, `inspect_archive`, `inspect_password`, `inspect_media`

Allowed reads:

- `binaries`
- `binary_parts`
- `article_headers`
- `releases`
- archive blob artifacts where applicable
- durable release catalog metadata when needed for rollups

Allowed writes:

- `binary_inspections`
- stage-owned evidence tables
- explicit release-facing metadata fields on `releases`
- release-family refresh queue enqueue only when identity/readiness-affecting fields change

Not allowed:

- mutating `article_headers`
- mutating `binary_grouping_evidence`
- materializing readiness summaries directly
- writing progress flags into release rows that belong in inspection/runtime tables

Rationale:

- inspection owns evidence extraction and release-facing metadata enrichment, not upstream source facts

Primary DBO entry points:

- `ListBinaryInspectionCandidates`
- `ListBinaryInspectionCandidatesWithOptions`
- `ClaimBinaryInspectionCandidates`
- stage-specific evidence upsert/update helpers in `inspection_store.go`

### `release_summary_refresh`

Allowed reads:

- release-family refresh queue
- `binaries`
- selected release-facing rollup inputs if required by readiness logic

Allowed writes:

- `release_family_readiness_summaries`
- queue claim/ack state owned by the refresh path

Not allowed:

- mutating `binaries`
- mutating ingest facts
- materializing release rows

Rationale:

- this stage is the sole heavy writer for the readiness read-model

Primary DBO entry points:

- `RefreshQueuedReleaseFamilySummaries`
- `RefreshQueuedReleaseFamilySummariesWithMetrics`
- Phase A and Phase B helpers in `release_family_summary_store.go`

### `release`

Allowed reads:

- `release_family_readiness_summaries`
- `binaries`
- inspection rollups and release-facing metadata needed for catalog formation

Allowed writes:

- `releases`
- `release_catalog_files`
- `release_files`
- `release_newsgroups`
- `release_family_readiness_acks`

Not allowed:

- mutating `article_headers`
- mutating `binary_parts`
- recomputing readiness summaries from raw binaries inline
- writing inspection evidence tables

Rationale:

- release consumes readiness and writes durable catalog state

Current boundary note:

- user-facing and admin file/detail reads should anchor on `release_catalog_files`
- `release_files` remains a transitional live-lineage bridge for binary/article drilldown and purge-time source cleanup only

Primary DBO entry points:

- `ListReleaseCandidates`
- `ListBinariesForReleaseCandidate`
- `UpsertRelease`
- `ReplaceReleaseFiles`
- `ReplaceReleaseNewsgroups`

### `release_generate_nzb`

Allowed reads:

- `releases`
- `release_catalog_files`
- `release_files`
- `release_newsgroups`
- source lineage needed to construct NZB
- inspection completion state

Allowed writes:

- archive blob/object store
- `release_archive_state`
- transitional `nzb_cache` if still required by current implementation

Not allowed:

- mutating ingest/assembly fact tables to track generation progress
- writing readiness summaries

Rationale:

- generate is the archive-tail start and should operate at the release layer

Primary DBO entry points:

- NZB generation reads and archive-state writes in the release/archive store surfaces

### `release_archive_nzb`

Allowed reads:

- `releases`
- `release_archive_state`
- transitional archive/NZB support tables

Allowed writes:

- archive blob/object store
- `release_archive_state`

Not allowed:

- mutating article/binary facts except where current transitional implementation still needs a release-layer pointer update

Rationale:

- legacy/transitional archive stage only

Primary DBO entry points:

- `MarkReleaseArchiveStored`
- `MarkReleaseArchiveFailed`

### `release_purge_archived_sources`

Allowed reads:

- `releases`
- `release_archive_state`
- durable release catalog tables
- source lineage tables that are candidates for deletion

Allowed writes:

- delete from temporary source lineage tables
- delete from transitional archive support tables where allowed
- update `release_archive_state`
- write purge metrics/runtime bookkeeping

Not allowed:

- deleting durable catalog rows needed for frontend or enrichment
- deleting rows before archive durability and required inspection gates are met

Rationale:

- purge is terminal cleanup, not a general-purpose downstream mutator

Primary DBO entry points:

- `PurgeArchivedReleaseSources`

### `indexer_maintenance`

Allowed reads:

- any table needed for bounded cleanup, backfill, runtime metrics, or reclaim preparation

Allowed writes:

- maintenance-owned cleanup tables
- bounded cleanup deletes on temporary/support rows
- maintenance backfills for durable catalog/archive compatibility tables where explicitly defined

Not allowed:

- taking over ownership of stage fact tables
- inventing new permanent write paths outside documented maintenance responsibilities

Rationale:

- maintenance may clean, prune, or backfill, but it is not an alternate owner for primary pipeline data

Primary DBO entry points:

- maintenance cleanup queries in `maintenance_store.go`
- integrity checks and reindex helpers in `integrity_store.go`
- operational stats read models used by admin/dashboard flows

## Forbidden Write-Backs

These are explicit anti-patterns for future changes.

### Scrape stages may not

- update `binaries`
- update `releases`
- mark readiness directly on `release_family_readiness_summaries`

### Assemble stages may not

- update `article_headers` to record assembly progress
- update `releases` to record assembly progress
- bulk recompute `release_family_readiness_summaries`

### Recovery may not

- write retry/backoff state into `article_headers`
- recompute readiness summaries inline
- use release rows as recovery work state

### Inspection stages may not

- write progress markers into `binaries` or `releases` when a stage-runtime table should own the state
- update ingest tables to record inspection outcomes
- directly upsert heavy readiness summary rows

### Release may not

- mutate ingest or assembly facts to record candidate consumption
- rediscover family readiness from raw binaries inside the hot formation transaction
- own inspection evidence writes

### Archive stages may not

- push archive lifecycle state into ingest or assembly tables
- mutate upstream source facts except where the purge contract explicitly allows deletion

### Purge may not

- delete durable release catalog rows required for frontend detail, enrichment, or archive access
- delete source lineage before the purge contract is satisfied

## Purge Contract

This section defines when purge is allowed and what purge owns.

### Purge purpose

Purge exists to free database space by removing temporary source lineage after:

- release identity is already formed
- the NZB is durably stored in blob storage
- required inspection has completed
- the durable release catalog can still serve frontend and enrichment use cases

### Purge preconditions

A release is purge-eligible only when all of the following are true:

- a `releases` row still exists for the release
- at least one `release_catalog_files` row still exists for the release
- archive state says the NZB is durable and available, including a non-blank archive object key
- required inspection gates for archive/purge are satisfied, currently including a completed `inspect_media`
- no earlier stage still depends on live source lineage for that release
- purge state has been explicitly recorded as eligible or pending

### What purge should delete

Purge owns deletion of temporary source lineage and heavy build surfaces, including:

- `article_headers` rows that exist only to support the purged release lineage
- `article_header_ingest_payloads` tied only to that purged lineage
- `binaries`
- `binary_parts`
- `binary_grouping_evidence`
- inspection evidence tables tied only to purged binaries
- recovery work/support rows tied only to purged binaries
- transitional release-source bridge tables where durable replacements now exist

Exact delete scope must continue to honor shared-lineage safety. If a row is still referenced by another non-purge-eligible release, purge must not delete it.

Current delete order:

1. lock and validate `release_archive_state`
2. compute purgeable lineage binaries that are not shared with another non-purged release
3. delete binary-owned evidence/runtime rows by deleting the owning `binaries` rows
4. delete release-scoped transitional rows:
   - `release_files`
   - `release_newsgroups`
   - `nzb_cache`
5. delete `article_header_ingest_payloads` and `article_headers` only when no surviving `binary_parts` rows still reference them
6. delete `release_archive_lineage_*` tracking rows
7. mark `release_archive_state.archive_status = 'purged'`

### What purge must preserve

Purge must preserve:

- `releases`
- `release_catalog_files`
- durable release metadata needed for frontend detail
- durable enrichment and override tables
- `release_archive_state`
- archive blob/object storage
- any still-required provenance/catalog support tables until a durable replacement exists

### Locking expectations for purge

Purge is allowed to issue deletes on upstream lineage because those rows are terminal.

To keep purge from becoming a new contention source:

- purge should claim eligible releases first, then delete only claimed lineage
- purge should delete in stable batches
- purge should avoid mixed ownership updates on hot shared tables
- purge should rely on release/archive eligibility state instead of rescanning or re-locking the whole pipeline

## Current Known Deviations From The Target Model

These are accepted transitional debts, not permanent design goals.

- `article_header_ingest_payloads` still mixes ingest support state and recovery retry/backoff state
- `assemble_*` still updates `article_headers` claim/lease and assembled markers instead of moving all assembly runtime bookkeeping into a separate work surface
- `recover_yenc` and binary-recovery refinement still rewrite `release_files` and `binary_parts` when binary identity or recovered filenames change
- `release_files` and `nzb_cache` still exist as transitional compatibility surfaces
- `release_archive_detail_*` still exists in schema history, but active runtime writes and maintenance backfill have been removed
- some inspection/refinement flows still reach across boundaries more than the target model intends

## Migration Path To Strict Ownership

The migration path should stay incremental and reviewable.

### Phase 1: freeze shared derived writes

Goal:

- ensure `release_summary_refresh` remains the only heavy writer of `release_family_readiness_summaries`

Actions:

- remove any remaining multi-stage bulk summary recomputation
- keep dirty-key queue fan-in, not direct summary materialization
- move release-side readiness ack state out of `release_family_readiness_summaries` and into stage-owned ack/work state

### Phase 2: finish the durable release catalog boundary

Goal:

- make release-side frontend/detail data live in durable catalog tables from the start

Actions:

- keep `releases` as the permanent catalog header
- keep `release_catalog_files` as durable file/detail state
- reduce reliance on `release_files` for frontend/detail reads
- stop maintaining archive-detail snapshot tables once durable catalog reads fully replace them
- keep enrichment and override updates pointed at durable catalog rows

### Phase 3: separate work state from fact state

Goal:

- move retry/progress/runtime state out of mixed-purpose fact tables

Actions:

- reduce mixed ownership on `article_header_ingest_payloads`
- keep stage bookkeeping in dedicated work/runtime tables
- remove any release or binary progress flags that exist only for stage orchestration

### Phase 4: tighten purge eligibility and delete scope

Goal:

- make purge the sole terminal cleanup owner for source lineage

Actions:

- document exact lineage delete order
- keep shared-lineage safety checks explicit
- ensure purge leaves the durable release catalog fully usable

### Phase 5: shrink or remove transitional tables

Goal:

- reduce schema overlap once durable boundaries are fully landed

Actions:

- shrink or remove `release_files` where durable catalog tables fully replace it
- shrink or remove `release_archive_detail_*` compatibility tables if no longer needed
- continue reducing `nzb_cache` dependence

## Reviewable Execution Chunks

Use these as the default code-review breakdown for this workstream.

### Chunk 1: readiness-summary ownership isolation

Deliverables:

- one heavy writer for readiness summaries
- queue-only dirty fan-in from other stages
- no cross-stage bulk recomputation

### Chunk 2: inspection boundary cleanup

Deliverables:

- inspection writes only to inspection/evidence tables plus explicit release-facing metadata
- no stale-binary races that require write-backs into upstream ownership domains
- clearer separation between inspection runtime state and durable facts

### Chunk 3: durable release catalog completion

Deliverables:

- frontend/admin detail reads use durable catalog tables
- enrichment continues to work after source purge
- transitional release-source bridge usage is reduced

### Chunk 4: purge contract enforcement

Deliverables:

- purge eligibility is explicit
- purge delete scope is documented and encoded
- purge preserves all durable release catalog surfaces

### Chunk 5: transitional surface reduction

Deliverables:

- identify which transitional tables can now shrink or be removed
- reduce overlap between legacy archive support tables and durable catalog tables

## Working Rule For Code Review

For any schema or repository change in this workstream, answer these questions in the PR or commit notes:

1. Which stage owns the table being changed?
2. Is this a canonical fact table, a derived/materialized table, a queue/work table, or a durable release-catalog table?
3. Does the change add a new cross-stage write-back?
4. If yes, why is that exception necessary, and can it be replaced by a queue or stage-owned table?
5. Does this make purge safer, smaller, or more explicit?

If those answers are unclear, the change is probably crossing boundaries too loosely.
