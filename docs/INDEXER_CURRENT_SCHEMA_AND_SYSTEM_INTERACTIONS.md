# Indexer Current Schema And System Interactions

Snapshot date: 2026-06-03

This doc is the current whole-system schema map for the indexer and should remain a living reference as the system evolves.

Use this doc before changing retention, removing columns, or compacting derived tables. It answers three questions up front:

1. what the current schema layers are
2. which tables are canonical versus derived
3. how each hot table interacts with assemble, recovery, release, inspect, and maintenance

This doc is intentionally current-state oriented. Use `docs/archive/completed/indexer/2026-05-14-indexer-database-growth-trim/INDEXER_DATABASE_SCHEMA_AUDIT.md` for the completed column-by-column audit detail and `docs/archive/completed/indexer/2026-05-14-indexer-database-growth-trim/INDEXER_DATABASE_GROWTH_TRIM_PLAN.md` for the completed execution sequence from that sprint.

## Current Policy Direction

The indexer is moving toward strict stage and table ownership.

The policy goal is:

- each stage writes only to its canonical fact tables, its own work tables, or its own derived tables
- downstream stages read upstream fact tables but do not write back into them to mark progress
- queue tables and materialized summary tables are owned by a single writer stage where practical
- runtime state and operational bookkeeping should live in dedicated stage-runtime tables, not in upstream fact rows

This policy is directly tied to the current contention work. The earlier `release_family_readiness_summaries` deadlocks and `out of shared memory` failures came from multiple stages writing the same derived table and from large advisory-lock fan-out. The current preferred shape is queue fan-in plus single-stage materialization, not cross-stage write-back.

## Desired Stage Ownership Model

### Scrape stages

Stages:

- `scrape_latest`
- `scrape_backfill`

Write ownership:

- `article_headers`
- `article_header_ingest_payloads`

Read ownership:

- provider state
- newsgroup bounds
- stage runtime state

Rule:

- scrape owns ingest fact creation
- no later stage should rewrite scrape facts except bounded maintenance cleanup on explicitly temporary support tables

### Assemble stages

Stages:

- `assemble_lane_a`
- `assemble_lane_b`

Write ownership:

- `binaries`
- `binary_parts`
- grouping and assembly evidence surfaces
- yEnc recovery work seeding
- release family refresh queue only, not readiness summary materialization

Read ownership:

- `article_headers`
- `article_header_ingest_payloads`

Rule:

- assemble reads ingest facts and produces binary identity
- assemble should not write back into `article_headers`
- assemble should not materialize release-readiness rows directly

### Recovery stage

Stage:

- `recover_yenc`

Write ownership:

- `binaries` identity refinements
- yEnc retry/backoff state
- release family refresh queue only

Read ownership:

- `article_headers`
- `article_header_ingest_payloads`
- `binaries`

Rule:

- recovery may refine canonical binary identity
- recovery should not write to scrape-owned article facts beyond its own retry/support surfaces

### Inspection stages

Stages:

- `inspect_discovery`
- `inspect_par2`
- `inspect_nfo`
- `inspect_archive`
- `inspect_password`
- `inspect_media`

Write ownership:

- `binary_inspections`
- inspection evidence tables such as archive entries, media streams, text evidence, par2 sets, par2 targets, password candidate surfaces
- release inspection rollup fields on durable release catalog rows
- archived preview assets and archive preview metadata
- release family refresh queue only when canonical binary identity changes

Read ownership:

- `binaries`
- `binary_parts`
- `article_headers`
- `releases`
- archive blob artifacts

Rule:

- inspection reads upstream identity and archive facts
- inspection writes its own evidence and release-facing metadata
- inspection should not rewrite scrape facts or assemble membership facts

### Release summary refresh stage

Stage:

- `release_summary_refresh`

Write ownership:

- `release_family_readiness_summaries`

Read ownership:

- release family refresh queue
- `binaries`

Rule:

- this stage is the sole heavy materializer for `release_family_readiness_summaries`
- other stages should enqueue dirty keys, not recompute or lock summary rows directly

### Release formation stage

Stage:

- `release`

Write ownership:

- `releases`
- durable release catalog file/detail tables
- release-side catalog metadata
- summary acknowledgement state only

Read ownership:

- `release_family_readiness_summaries`
- `binaries`
- release-side inspection rollups

Rule:

- release formation consumes readiness and produces durable release catalog state
- it should not reach back into assemble or scrape fact tables to mutate them

### Archive and purge tail

Stages:

- `release_generate_nzb`
- `release_archive_nzb`
- `release_purge_archived_sources`

Write ownership:

- archive blob storage
- `release_archive_state`
- durable archived detail/catalog tables
- purge deletion of temporary source lineage tables

Read ownership:

- `releases`
- durable release catalog file/detail tables
- archive metadata

Rule:

- archive and purge operate at the release-catalog layer
- they should not recreate pressure on ingest or assembly fact tables beyond the one-time source purge they explicitly own

## Read And Locking Guidance

Normal PostgreSQL reads are not the main problem.

- plain `SELECT` uses MVCC and does not block ordinary `INSERT`/`UPDATE`/`DELETE` the way explicit row-locking reads do
- contention risk comes from `UPDATE`, `DELETE`, `SELECT ... FOR UPDATE`, foreign key enforcement under heavy write concurrency, large shared-table upserts, and multi-stage write-back to the same derived rows

Policy implication:

- reading upstream tables is acceptable
- cross-stage write-back into upstream fact tables or shared derived tables should be minimized or eliminated
- if a stage needs to signal downstream work, prefer queue rows over mutating upstream facts

## Known Current Deviations

The system is moving toward the model above but is not fully there yet.

Current notable deviations:

- some inspection paths still update release-facing rollup fields after binary work has already become purge-eligible
- release summary acknowledgement still updates `release_family_readiness_summaries`, although heavy recomputation has been isolated to `release_summary_refresh`
- legacy support tables such as `article_header_ingest_payloads` still mix ingest support state and recovery retry state

These deviations should be treated as explicit debt, not as the desired long-term model.

## Up-Front Questions And Answers

### Which schema source is authoritative?

The live Docker Postgres database is the schema source of truth for this sprint.

`internal/store/pgindex/migrations` is the only active migration history to reconcile against.

`internal/store/pgindex/migrations_archive` is historical only and must not drive current cleanup decisions.

### What should be preserved even if storage is tight?

Preserve canonical identity and membership surfaces first:

- `article_headers`
- `binaries`
- `binary_parts`

Preserve derived queue state only while it is still actively consumed:

- `release_family_readiness_summaries`

Treat audit/debug retention as optional and bounded unless code proves it is still needed:

- `article_header_ingest_payloads`
- `binary_grouping_evidence`
- verbose JSON retained only for historical debugging

### What is the current system-level storage problem?

The current growth problem is not release formation. It is oversized retention in ingest and identity-audit surfaces:

- `article_header_ingest_payloads` behaves like a large temporary shadow copy of ingest input
- `binary_grouping_evidence` behaves like a universal one-row-per-binary audit blob
- `release_family_readiness_summaries` retains far more weak-family residue than active queue state

### What does “safe cleanup” mean for this sprint?

Safe cleanup means:

- do not remove a field until every reader and writer is identified
- prefer stopping unnecessary writes before dropping columns
- prefer shortening retention on derived surfaces before touching canonical identity tables
- preserve the ability for summary refresh or recovery logic to recreate derived rows later

## Current Schema Layers

The live indexer schema is best understood as five layers:

1. ingest capture
2. assembly identity
3. recovery and identity refinement
4. release readiness and release formation
5. inspection and evidence enrichment

### Layer 1. Ingest capture

Primary tables:

- `article_headers`
- `article_header_ingest_payloads`

Purpose:

- store per-article scrape facts
- hold enough structured subject/yEnc state for assemble and yEnc recovery

Rule:

- `article_headers` is canonical ingest fact storage
- `article_header_ingest_payloads` is supporting workflow state, not a second durable source of truth

### Layer 2. Assembly identity

Primary tables:

- `binaries`
- `binary_parts`

Purpose:

- turn article rows into canonical binary/file identity
- attach article membership to the chosen binary row

Rule:

- `binaries` is the canonical current identity row
- `binary_parts` is the canonical article-to-binary membership bridge

### Layer 3. Recovery and identity refinement

Primary tables:

- `article_header_ingest_payloads`
- `binaries`
- `binary_grouping_evidence`

Purpose:

- recover stronger file identity when subject parsing was weak
- preserve enough audit evidence to explain why identity changed

Rule:

- recovery may improve canonical identity on `binaries`
- audit evidence should remain bounded and selective

### Layer 4. Release readiness and release formation

Primary tables:

- `release_family_readiness_summaries`
- `releases`
- `release_files`

Purpose:

- aggregate binary identity into release-family queue state
- form user-visible releases from readiness candidates

Rule:

- `release_family_readiness_summaries` is a derived read-model and queue surface
- `releases` and `release_files` are downstream outputs, not the source of identity

### Layer 5. Inspection and evidence enrichment

Primary tables:

- `binary_inspections`
- `binary_archive_entries`
- `binary_media_streams`
- `binary_text_evidence`
- `binary_par2_sets`
- `binary_par2_targets`

Purpose:

- extract evidence that can strengthen metadata or promote identity quality

Rule:

- inspection enriches and sometimes promotes upstream identity
- these tables are evidence surfaces, not primary identity ownership

## Canonical Versus Derived Surfaces

### Canonical tables

These are the tables the system would rebuild around if everything else were trimmed:

- `article_headers`
- `binaries`
- `binary_parts`

Why:

- they hold durable article facts, current binary identity, and membership relationships

### Active derived tables

These are derived but still directly consumed by runtime stages:

- `article_header_ingest_payloads`
- `release_family_readiness_summaries`

Why:

- assemble and yEnc recovery still read structured payload fields
- release and recovery still read readiness summaries as the active queue model

### Audit and debug tables or fields

These are the first place to seek storage reduction:

- `binary_grouping_evidence`
- verbose JSON in `grouping_evidence_json`
- any unnecessary transient payload columns on `article_header_ingest_payloads`

Why:

- they exist to explain decisions, not to define the current identity itself

## Hot Tables And Their Roles

### `article_headers`

System role:

- durable per-article scrape fact row

Primary writers:

- scrape ingest via `InsertArticleHeaders` in `internal/store/pgindex/repository.go`
- assemble claim/update paths in `internal/store/pgindex/assembly_store.go`

Primary readers:

- assemble claim and hydration queries
- maintenance payload cleanup joins
- downstream joins through `binary_parts`

Why it exists:

- every later stage needs stable article identity and fact history

Trim posture:

- not an early drop-column target
- only consider retention or partitioning later if ingest volume still requires it

### `article_header_ingest_payloads`

System role:

- structured ingest-side support row for assemble and yEnc recovery

Primary writers:

- scrape ingest payload upsert in `internal/store/pgindex/repository.go`
- yEnc retry/backoff updates in `internal/store/pgindex/assembly_store.go`

Primary readers:

- assemble candidate hydration
- yEnc recovery candidate selection
- maintenance cleanup

Why it exists:

- subject-derived fields still matter after scrape
- yEnc recovery still needs retry state and structured file hints

Trim posture:

- keep structured hot-path fields
- stop depending on unnecessary raw payload JSON
- shorten assembled-row retention sharply

### `binaries`

System role:

- canonical current file/binary identity row

Primary writers:

- assemble upsert in `internal/store/pgindex/assembly_store.go`
- yEnc recovery promotion in `internal/store/pgindex/yenc_recovery_store.go`
- inspection-driven enrichment in `internal/store/pgindex/inspection_store.go`

Primary readers:

- readiness summary refresh
- release selection and formation
- inspect/admin/catalog reads

Why it exists:

- this is the current best known identity for file grouping and release formation

Trim posture:

- keep core identity fields
- compact evidence attached to the row, not identity itself

### `binary_parts`

System role:

- canonical article membership map for each binary

Primary writers:

- assemble and recovery merge/update paths

Primary readers:

- yEnc recovery seed selection
- release formation and NZB file construction
- inspect and admin detail joins

Why it exists:

- binds article headers to the chosen binary identity

Trim posture:

- preserve
- this is structural, not optional audit retention

### `binary_grouping_evidence`

System role:

- side-table explanation of why binary identity was chosen

Primary writers:

- assemble evidence upsert in `internal/store/pgindex/assembly_store.go`

Primary readers:

- inspect/admin detail reads
- admin UI JSON detail views

Why it exists:

- explains grouping choices and weak/fallback identity outcomes

Trim posture:

- strongest oversized derived surface
- move toward sparse and compact retention, not universal retention

### `release_family_readiness_summaries`

System role:

- active release queue and readiness read-model

Primary writers:

- summary refresh in `internal/store/pgindex/release_family_summary_store.go`
- release processing acknowledgements in `internal/store/pgindex/release_store.go`

Primary readers:

- release candidate selection
- yEnc recovery candidate discovery
- inspect/dashboard pending metrics

Why it exists:

- provides a fast aggregate surface so release does not need to rediscover family quality from raw binaries

Trim posture:

- keep as an active read-model
- aggressively prune non-pending residue once recovery and cleanup no longer need it

## Stage Interactions

### Scrape -> Assemble

Flow:

- scrape stores durable article facts in `article_headers`
- scrape also stores structured ingest payload support in `article_header_ingest_payloads`
- assemble claims unassembled headers and hydrates candidates from both tables

Key interaction:

- assemble already reconstructs `RawOverview` from structured fields rather than relying on stored raw JSON

### Assemble -> Binary Identity

Flow:

- assemble groups article candidates into binaries
- assemble writes canonical identity to `binaries`
- assemble writes article membership to `binary_parts`
- assemble writes summary/audit evidence to inline and side-table evidence surfaces

Key interaction:

- `binary_parts` links ingest facts to canonical binary identity

### Binary Identity -> Recovery

Flow:

- weak or obfuscated readiness states feed yEnc recovery candidate selection
- yEnc recovery selects a representative article through `binary_parts`
- recovered header data may rewrite `binaries` identity and merge rows

Key interaction:

- recovery improves canonical identity on `binaries`; it should not need long-lived raw ingest blobs if structured fields are enough

### Identity -> Readiness -> Release

Flow:

- summary refresh aggregates `binaries` into `release_family_readiness_summaries`
- release consumes pending summary rows and forms `releases` plus `release_files`

Key interaction:

- `release_family_readiness_summaries` is a queue surface derived from binary identity, not a durable truth source

### Inspect -> Identity Refinement

Flow:

- inspect stages add archive, media, text, and PAR2 evidence
- some evidence updates `binaries` fields such as expected archive counts or classification hints
- readiness refresh then re-evaluates family quality

Key interaction:

- inspect should strengthen upstream identity and readiness, not fork a second identity system

## Current Cleanup Implications

### Low-risk first moves

- stop writing raw ingest JSON that current code can reconstruct
- shorten retention on assembled payload rows
- stop universal retention of side-table grouping evidence when the row is already stable and high confidence

### Medium-risk follow-ups

- move yEnc retry state out of bulky payload rows if short retention is still insufficient
- drop unused columns after readers are removed and retention has cleared old data
- add dedicated readiness pruning for non-pending weak-family residue

### Things to avoid early

- dropping canonical identity fields from `binaries`
- removing membership fields from `binary_parts`
- pruning readiness rows that are still pending or still needed by yEnc recovery targeting

## Working Rule For This Sprint

When deciding whether to keep or remove data, use this order:

1. preserve canonical identity
2. preserve active queue state only while it is actionable
3. keep compact summaries where they help operations
4. remove or shorten retention on verbose duplicate audit payloads first
