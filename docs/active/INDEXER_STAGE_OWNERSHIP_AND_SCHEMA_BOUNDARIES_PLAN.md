# Indexer Stage Ownership And Schema Boundaries Plan

Snapshot date: 2026-06-03

This is the active execution plan for enforcing strict stage ownership and schema boundaries across the indexer.

Use this plan together with:

- `docs/INDEXER_CURRENT_SCHEMA_AND_SYSTEM_INTERACTIONS.md`
- `docs/active/INDEXER_NZB_ARCHIVAL_AND_SOURCE_PURGE_PLAN.md`

This plan turns the ownership policy into concrete repository, schema, and purge-boundary changes.

## Sprint Goal

Reduce write contention and long-term schema drift by making stage ownership explicit and enforceable.

The target model is:

- upstream fact tables are written by their owning stages only
- downstream stages consume upstream facts without writing back into them for progress bookkeeping
- shared derived tables have one heavy materializer
- release-catalog tables remain durable after archive and purge
- purge becomes the explicit terminal owner of source-lineage deletion

## In Scope

- repository/query boundary changes that remove cross-stage write-backs
- schema and ownership clarifications needed to separate fact, queue, derived, and durable catalog tables
- narrowing readiness-summary writes to the refresh path
- finishing the durable release catalog boundary
- formalizing purge eligibility and delete scope
- reducing transitional overlap once the durable boundaries are in place

## Out Of Scope

- new frontend feature work unrelated to durable release hydration
- large topology changes such as separate processes or services unless boundary enforcement proves they are required
- new media-preview or archive-extraction features

## Reviewable Chunks

### Chunk 1: readiness-summary ownership isolation

Goal:

- keep `release_summary_refresh` as the only heavy writer of `release_family_readiness_summaries`

Expected changes:

- remove remaining inline summary recomputation outside the refresh stage
- narrow or eliminate release-side summary ack writes where possible
- keep dirty-key fan-in queue based

Validation:

- no multi-stage summary recomputation paths remain
- release and assemble can run concurrently without reintroducing deadlock or shared-memory pressure on summary writes

### Chunk 2: inspection boundary cleanup

Goal:

- keep inspection writes inside inspection-owned tables plus explicit release-facing metadata

Expected changes:

- reduce stale-binary races and claim/finish failures
- keep inspection runtime/progress state out of upstream fact rows
- ensure inspection does not become an alternate owner of binary or release orchestration state

Validation:

- inspect stages tolerate already-deleted binaries safely
- inspection does not need upstream fact mutations to complete

### Chunk 3: durable release catalog completion

Goal:

- make the release catalog fully durable without source-lineage dependence for normal frontend/detail reads

Expected changes:

- continue moving frontend/admin detail reads onto `releases` plus durable release-catalog tables
- reduce dependence on `release_files` as a permanent detail source
- preserve post-purge enrichment capability against durable catalog rows

Validation:

- archived and purged releases remain enrichable and viewable
- release detail hydration does not require binary/article lineage for standard UI paths

### Chunk 4: purge contract enforcement

Goal:

- make purge a safe, explicit terminal cleanup stage

Expected changes:

- encode purge preconditions more strictly
- document and implement exact delete scope
- preserve durable catalog rows and archive state while removing source lineage

Validation:

- purge deletes only rows that are no longer needed by any non-purged release
- purge does not break release detail, download, or enrichment behavior

### Chunk 5: transitional surface reduction

Goal:

- shrink or remove transitional overlap after durable ownership is enforced

Expected changes:

- review `release_files`, `release_archive_detail_*`, and `nzb_cache`
- remove or shrink transitional tables only after all required readers are moved

Validation:

- transitional table removal does not reintroduce source-lineage retention requirements

## Immediate Backlog

1. Audit any remaining direct writes to `release_family_readiness_summaries` outside the refresh stage and remove or narrow them.
2. Audit inspection write paths for progress/runtime state leakage into non-inspection tables.
3. Document the exact purge delete order and shared-lineage safety checks in repository code comments and tests.
4. Identify which remaining `release_files` reads are still user-facing versus transitional-only.
5. Decide which transitional archive-support tables are still required once durable catalog reads are complete.

## Purge-Specific Policy

Purge is the only downstream stage allowed to delete upstream source facts, and only when:

- release catalog state is already durable
- archive state is durable
- required inspection gates are complete
- the lineage is no longer needed by another active release

Purge should remain batch-based and claim-driven so it does not become a new contention hotspot.

## Sign-Off Requirements

Before this sprint is considered complete:

- the ownership matrix in `docs/INDEXER_CURRENT_SCHEMA_AND_SYSTEM_INTERACTIONS.md` matches live code paths
- summary materialization has one heavy writer
- durable release catalog reads no longer depend on temporary source lineage for normal UI flows
- purge delete scope is explicit, tested, and documented
- at least one live serve-monitor pass shows no regression in release/assemble contention after the boundary changes land
