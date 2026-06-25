# Indexer Schema And Partitions

## Partition Key

High-volume source, work, and projection tables use daily UTC range partitions
keyed by `source_posted_at`.

Provider and newsgroup are not partition keys in this sprint. They remain
indexed predicates, runtime tier/profile dimensions, and explicit admin purge
filters.

## Unpartitioned Durable Roots

These tables stay unpartitioned:

- `binary_core`
- `releases`
- `release_files`
- `release_catalog_files`
- `release_newsgroups`
- `release_archive_state`
- `release_archive_detail_*`
- `release_archive_lineage_*`
- `nzb_cache`
- enrichment and override tables

## Partitioned Source And Work Tables

- source/header lineage: `article_headers`,
  `article_header_ingest_payloads`, `article_header_crosspost_groups`,
  `article_header_poster_refs`, `article_header_assembly_queue`,
  `poster_materialization_queue`
- binary work/projection: `binary_parts`, `binary_observation_stats`,
  `binary_identity_current`, `binary_recovery_current`, `binary_lifecycle`,
  `binary_completion_keys`, `binary_grouping_evidence`,
  `binary_projection_events`, `binary_superseded_sources`
- yEnc/inspect work and evidence: `yenc_recovery_work_items`,
  `binary_inspection_ready_queue`, `binary_inspections`,
  `binary_inspection_artifacts`, `binary_archive_entries`,
  `binary_text_evidence`, `binary_media_streams`, `binary_par2_sets`,
  `binary_par2_targets`
- release-derived work: `release_family_readiness_summaries`,
  `release_ready_candidates`, `release_recovered_file_set_candidates`,
  `release_stage_dirty_families`

## Query Shape Rules

- Joins to partitioned source tables must include `source_posted_at`.
- Upserts into partitioned tables must use conflict targets that include
  `source_posted_at`.
- Long-running stages must claim from stage-owned work tables and hydrate
  source facts after claiming exact keys.
- Daily bucket scans should use bounded `source_posted_at >= day_start AND
  source_posted_at < day_end` predicates.
