# Indexer Schema And Partitions

## Partition Key

High-volume source, work, and projection tables use daily UTC range partitions
keyed by `source_posted_at`. Child names and bounds must both use UTC days:
`*_20260326` covers `2026-03-26T00:00:00Z` through
`2026-03-27T00:00:00Z`, regardless of the PostgreSQL session timezone.

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

- Native daily partitions must be pre-provisioned before the indexer
  supervisor starts active stages. Startup calls the partition provisioner for
  the configured retention horizon, then scrape, assemble, yEnc, inspect, and
  release work paths only verify that the child partitions already exist.
- Runtime stage write paths must not run partition DDL. Even single-parent
  `CREATE TABLE ... PARTITION OF` statements require relation locks that can
  deadlock with active scrape/assemble/yEnc transactions touching other
  partition parents. Missing partitions are a startup/provisioning blocker, not
  something a hot writer should repair.
- Startup provisioning may call `pgindex_ensure_daily_partition` for one
  parent/day child at a time while no indexer stage transactions are running.
  Multi-parent partition helper functions and runtime partition conversion
  helpers are not part of the v0.8.0 baseline.
- Runtime settings `indexing.retention.create_partitions_days_before` and
  `indexing.retention.create_partitions_days_ahead` define the startup
  provision horizon. Defaults are intentionally backfill-friendly
  (`180` days before, `8` days ahead) so older configured groups do not fall
  into default partitions or trigger live DDL.
- Default partitions are emergency-only. They are monitored by retention
  dry-runs and should remain empty for normal scrape/backfill/stage work.
- Joins to partitioned source tables must include `source_posted_at`.
- Upserts into partitioned tables must use conflict targets that include
  `source_posted_at`.
- Inserts into partitioned parents must provide `source_posted_at`; do not rely
  on defaults or follow-up updates to route rows.
- Deletes and updates against partitioned parents should include
  `source_posted_at` whenever the row's day is known so PostgreSQL can prune
  partitions.
- Long-running stages must claim from stage-owned work tables and hydrate
  source facts after claiming exact keys.
- Daily bucket scans should use bounded `source_posted_at >= day_start AND
  source_posted_at < day_end` predicates.

## Index Alignment

Parent indexes are recreated on native partition parents so child partitions
inherit local indexes for the hot query shapes:

- queue claim queries start with `source_posted_at`, status/priority/lease
  columns, and the stage-specific identity key;
- source hydration joins use `(source_posted_at, article_header_id)` or
  `(source_posted_at, id)`;
- binary projection joins use `binary_id` plus `source_posted_at` when joining
  back to partitioned source/work rows;
- release-family work uses `(source_posted_at, provider_id, newsgroup_id,
  key_kind, family_key)`;
- retention dry-runs and drops use daily partition metadata and bounded
  `source_posted_at` predicates.

If a DBO query changes shape, update the matching parent index or document why
an existing index still supports it before merging.
