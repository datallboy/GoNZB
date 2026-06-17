# Indexer Release Formation Playbook

Snapshot date: 2026-06-16

This is the live reference for how the indexer should turn scraped article headers into release families, releases, hydrated public releases, archived NZBs, and safely purged source rows.

Use this document when changing assemble, yEnc recovery, release-summary-refresh, release formation, inspection gates, or purge. Regressions in this path directly reduce release quality.

## Formation Contract

A public release should be formed from a coherent family of binaries that represent the same Usenet upload. The family must have enough identity and completeness evidence to produce a usable NZB.

Minimum functional path:

1. `scrape_latest` / `scrape_backfill` ingest article headers and immutable ingest payloads.
2. `assemble_lane_a` and `assemble_lane_b` claim headers and write canonical binary projections.
3. `recover_yenc` or assemble inline yEnc recovery promotes true recovered filenames when subject/XOVER data is obfuscated.
4. `release_summary_refresh` summarizes binary families into readiness rows and release-ready candidates.
5. `release` forms durable release rows and release-file mappings from ready candidates.
6. `inspect_*` hydrates metadata and readiness gates.
7. `release_generate_nzb` writes the NZB.
8. `release_archive_nzb` archives the NZB and captures catalog/detail snapshots.
9. `release_purge_archived_sources` deletes source lineage only after archival and required inspection gates are satisfied.

## Source Tables And Ownership

### Scrape

Primary writes:

- `article_headers`
- `article_header_ingest_payloads`
- scrape-owned newsgroup/provider inventory tables
- materializer queues for poster and crosspost summaries

Scrape must not own binary grouping state. It can seed parseable facts, but assemble/recovery own binary identity.

### Assemble

Primary writes:

- `binary_core`
- `binary_observation_stats`
- `binary_identity_current`
- `binary_recovery_current` seed rows
- `binary_lifecycle`
- `binary_parts`
- `binary_completion_keys`
- assembly claim/progress tables

Assemble creates one canonical binary per recovered or observed file identity. It must not create one binary per article when the article belongs to a multipart file.

Expected binary identity:

- `binary_core.binary_key` is the unique file-level anchor for `(provider_id, newsgroup_id, binary_key)`.
- `binary_identity_current.release_family_key` groups files that likely belong to the same release.
- `binary_identity_current.file_set_key` groups recovered files across compatible release-family contexts.
- `binary_observation_stats.observed_parts` and `total_parts` describe file completeness.

### yEnc Recovery

yEnc recovery is a release-formation input, not an optional decoration.

Primary writes:

- `binary_core.binary_key`
- `binary_identity_current`
- `binary_observation_stats`
- `binary_recovery_current`
- `binary_parts`
- `release_files` filename repair when a release already exists
- `yenc_recovery_work_items`
- release-family dirty queue

Critical invariant:

- A recovered yEnc filename must canonicalize to one binary key per provider/newsgroup/file.
- For recovered archive/media files, the binary key should be `file_set_key::recovered_file_name` after normalization.
- Recovered rows with the same provider, newsgroup, file set, and filename must merge parts into the same binary instead of remaining separate one-part binaries.

Why this matters:

- If yEnc recovery stores the recovered filename but preserves a per-subject binary key, release-summary-refresh sees thousands of incomplete one-part binaries.
- Those fragments correctly fail release formation because `observed_parts` stays low and expected-file coverage cannot be trusted.
- A healthy recovered family should show increasing `observed_parts` on file-level binaries and eventually produce `release_recovered_file_set_candidates` when cross-group/file-set gates are satisfied.

## Release Summary Refresh

Primary reads:

- `binary_core`
- `binary_identity_current`
- `binary_observation_stats`
- `binary_recovery_current`
- `release_family_dirty_queue`

Primary writes:

- `release_family_readiness_summaries`
- `release_ready_candidates`
- `release_recovered_file_set_candidates`
- release candidate ack tables

Summary responsibilities:

- Convert dirty binary family keys into readiness buckets.
- Promote actionable family/base-stem candidates into `release_ready_candidates`.
- Sync recovered yEnc file sets into `release_recovered_file_set_candidates`.
- Avoid broad rescans when a narrow dirty-key batch can answer the question.

Healthy signs:

- Dirty queue drains in bounded batches.
- `release_ready_candidates` contains actionable `release_family`, `base_stem`, and when available `recovered_file_set` candidates.
- Most release-stage skips are explainable by real low coverage, not duplicated recovered fragment rows.

Warning signs:

- `binary_recovery_current` has many `recovered_source = 'yenc_header'` rows but `release_recovered_file_set_candidates` stays at zero.
- Recovered filename groups have many binary rows where every row has `observed_parts = 1` and the same `total_parts`.
- `release` repeatedly cools down low-coverage candidates while yEnc recovery reports high recovered counts.

## Release Formation

Primary reads:

- `release_ready_candidates`
- `release_recovered_file_set_candidates`
- `binary_core`
- `binary_identity_current`
- `binary_observation_stats`
- `binary_parts`
- inspection/recovery evidence needed for gating

Primary writes:

- `releases`
- `release_files`
- `release_newsgroups`
- release candidate acknowledgements

Candidate kinds:

- `release_family`: normal family-key grouping from assemble.
- `base_stem`: archive/media stem grouping when the stem is stronger than noisy subject context.
- `recovered_file_set`: yEnc-recovered file-set grouping, including cross-newsgroup release composition.

Formation should reject:

- fragment-only families with no usable main payload.
- weak single-binary families lacking usable identity.
- low expected-file coverage when expected counts are known.
- passworded releases from public-ready visibility until password status is resolved.

Formation should not reject:

- valid software/category releases solely because of category.
- recovered yEnc file sets that have coherent file-level binaries and sufficient completeness.

## Inspection And Public Readiness

Inspection stages hydrate formed releases or eligible binaries.

Important gates:

- `inspect_discovery` can run before release formation when binary evidence is enough.
- `inspect_par2` can run on eligible binaries.
- `inspect_archive` and `inspect_media` are release hydration gates for public-ready output and NZB generation when runtime settings require them.
- `release_generate_nzb` should remain blocked by required inspection gates if configured; do not bypass hydration to inflate public counts.

## Archive And Purge

Purge is terminal cleanup only.

Purge prerequisites:

- release is archived and durable.
- catalog/detail snapshots exist.
- required inspection gate is complete.
- source lineage rows were captured before archive/purge.

Purge may delete binary source rows through `binary_core`, but it must not overlap active binary writers. It is serialized with assemble lanes under the supervisor `binary-source-write` group.

## Regression Checks

Run these checks during soak or after changing formation logic:

```sql
SELECT recovered_source, count(*), count(*) FILTER (WHERE recovered_file_name <> '') AS with_name
FROM binary_recovery_current
GROUP BY recovered_source
ORDER BY count(*) DESC;

SELECT count(*) AS recovered_candidates
FROM release_recovered_file_set_candidates;

SELECT count(*) AS duplicate_file_groups, sum(rows - 1) AS excess_binary_rows
FROM (
  SELECT bc.provider_id, bc.newsgroup_id, bic.file_set_key, bic.file_name, count(*) AS rows
  FROM binary_core bc
  JOIN binary_identity_current bic ON bic.binary_id = bc.binary_id
  JOIN binary_recovery_current brc ON brc.binary_id = bc.binary_id
  WHERE brc.recovered_source = 'yenc_header'
    AND bic.file_set_key <> ''
    AND bic.file_name <> ''
  GROUP BY 1,2,3,4
  HAVING count(*) > 1
) s;

SELECT readiness_bucket, recover_pending, count(*)
FROM release_family_readiness_summaries
GROUP BY 1,2
ORDER BY count(*) DESC;

SELECT key_kind, ready_reason, count(*)
FROM release_ready_candidates
GROUP BY 1,2
ORDER BY count(*) DESC;
```

Expected after fresh recovery:

- recovered yEnc duplicates should stay near zero for the same provider/newsgroup/file-set/file-name.
- recovered candidates should appear when enough recovered files are complete and pass file-set gates.
- release logs should show formed releases mixed with explainable low-coverage cooldowns, not only low-coverage cooldowns.

## Current Known Risk

Databases processed before the 2026-06-16 recovered-yEnc canonicalization fix may contain polluted recovered fragment binaries. A clean database is the preferred validation path. A targeted repair pass can be added later if preserving such data becomes necessary.
