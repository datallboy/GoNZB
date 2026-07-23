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
  `binary_superseded_sources`
- yEnc/inspect work and evidence: `yenc_recovery_work_items`,
  `binary_inspection_ready_queue`, `binary_inspections`,
  `binary_inspection_artifacts`, `binary_archive_entries`,
  `binary_text_evidence`, `binary_media_streams`, `binary_par2_sets`,
  `binary_par2_targets`
- release-derived work: `release_family_readiness_summaries`,
  `release_ready_candidates`, `release_recovered_file_set_candidates`,
  `release_stage_dirty_families`

## Provisioning Contract

Partitions are created for source days actually observed in work, not for a
retention-sized calendar horizon. Provisioning is grouped by stage ownership:

- scrape provisions only header, ingest-support, assembly-input, and poster
  queue parents;
- scheduler, assemble, yEnc, inspect, and release stages provision their own
  output parents immediately before beginning a write transaction;
- each parent/day child is created in its own short DDL transaction so no
  partition operation holds locks across several hot parents;
- every write path verifies the required child after provisioning and refuses
  to use a default partition.

Startup creates the scrape bundle for the current UTC day and the configured
small look-ahead window. A UTC-rollover task refreshes that look-ahead. These
proactive children reduce first-write latency; they are not required for
correctness and do not replace exact-day provisioning.

One scrape pass may introduce at most 32 previously unseen source days by
default. Work beyond that cap is recorded as a durable deferred article range
before a latest/backfill checkpoint advances. Existing days do not consume the
cap.

Explicit historical timeframes use the same exact-day provisioning and cap.
Their configured UTC dates are search inputs, not a request to precreate every
calendar partition in the window. Resolved article bounds and next-article
progress remain in the unpartitioned
`indexer_scrape_timeframe_progress` control table.

Default partitions are quarantine/fallback surfaces for legacy or external
writes. Application stages must keep them empty. Rows found there block the
affected source day and are moved only by the offline default-rehome workflow.

## Query Shape Rules

- Partition DDL runs outside stage data transactions. Multi-parent DDL inside
  a hot writer transaction remains forbidden.
- `pgindex_ensure_daily_partition` is called for one parent/day child per short
  transaction with a bounded lock timeout. A missing child defers/retries the
  affected work; it never permits a default-partition write.
- `indexing.partitions.precreate_days_ahead` controls proactive scrape children
  and defaults to `2`. The legacy `create_partitions_days_before` setting is
  ignored because retention duration and partition provisioning are unrelated.
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
