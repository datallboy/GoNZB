# Indexer Storage Retention And Purge Map

This document maps the large indexer tables to the jobs and stages that create,
consume, and purge them. It is the working reference for storage cleanup decisions.

Last audit: 2026-06-23, using direct Docker `psql` reads against `gonzb-postgres`
and the app storage-audit report path. Audit artifacts are local scratch files
under `.tmp/storage-audit/`.

## Current Storage Pressure

PostgreSQL database size was `266 GB`. The Docker volume mounted at
`/mnt/vm-store/docker-vols/gonzb_gonzb_postgres_data` had about `272 MB` free.
Under this pressure, even read-only exact counts can fail if PostgreSQL needs
temporary files. Audit/report code should therefore avoid broad joins, avoid
large sort/group operations, and treat expensive optional estimates as unavailable
rather than failing the whole report.

Largest audited tables:

| Table | Approx rows | Total size | Main data use |
| --- | ---: | ---: | --- |
| `binary_identity_current` | 39.9M | 59 GB | Current binary grouping identity for release formation, summaries, inspection selection, yEnc selection, catalog detail |
| `yenc_recovery_work_items` | 29.2M | 26 GB | Durable recover-yEnc backlog and completed/stale recovery state |
| `article_header_crosspost_groups` | 54.7M | 25 GB | Raw crosspost evidence from scraped Xref data |
| `binary_core` | 39.6M | 24 GB | Canonical binary root row; cascade anchor for binary source purge |
| `article_headers` | 48.3M | 19 GB | Scraped source headers and article identity |
| `binary_parts` | 47.8M | 18 GB | Binary-to-article part mapping and NZB segment source |
| `article_header_ingest_payloads` | 48.3M | 16 GB | Retained subject/poster/xref/yEnc parse payloads |
| `release_family_readiness_summaries` | 11.5M | 15 GB | Derived release readiness projection |
| `poster_materialization_queue` | 47.6M | 13 GB | Durable queue for poster dimension/ref materialization |

## Cleanup Classes

Low risk cleanup removes queue/history residue and should not change release
formation semantics. These deletes do not return OS space by themselves; they
only create reusable PostgreSQL free space until a table rewrite or equivalent
operation.

Medium risk cleanup removes derived evidence/projections. It is only safe when
the projection can be rebuilt from retained source rows or when current runtime
paths no longer read the raw evidence.

High risk cleanup removes source lineage. It can permanently prevent release
formation or rebuild unless the release is public-ready, 100% payload-complete
when required, has durable catalog files, and has a durable archived NZB.

## Table And Job Map

| Table | Producer | Consumer | Done/terminal state | Purge risk |
| --- | --- | --- | --- | --- |
| `poster_materialization_queue` | Scrape ingest materializer queues one row per `article_headers.id` with normalized poster key. If the poster key changes, the row is reset to `pending`. | `poster_materialize` claims `pending`/`failed` rows with `FOR UPDATE SKIP LOCKED`, upserts `posters`, and upserts `article_header_poster_refs`. | `done` means the poster dimension row and article-header poster reference were successfully upserted for that queued poster key. | Low for `status='done'` rows only. Deleting `done` rows does not remove `posters` or `article_header_poster_refs`; it only removes completed work receipts. The maintenance task is batch-limited and records storage snapshots. |
| `posters` | `poster_materialize` inserts canonical poster names. | Release grouping, poster references, search/detail views, diagnostics. | No queue state; dimension rows are durable catalog-ish data. | High to purge unless proven unreferenced. Not a first reclaim target. |
| `article_header_poster_refs` | `poster_materialize` maps article headers to poster IDs and keys. | Grouping, release formation context, diagnostics. | Current projection for each article header. | Medium/high. Can be rebuilt only while source payloads and headers are retained, but rebuilding tens of millions of rows is expensive. |
| `article_headers` | Scrape ingest. | Assemble queue, yEnc recovery source, binary parts, release/archive lineage, catalog article refs. | Queue/relationship state is authoritative: `article_header_assembly_queue` means pending assemble work, `binary_parts` means the header has been mapped to a binary. `assembled_at` is legacy and not maintained by the current queue-based assemble path. | High. Do not purge source headers directly unless strict no-queue/no-binary/no-yEnc/no-release/no-archive guards pass in an audit-only preflight. |
| `article_header_ingest_payloads` | Scrape ingest payload split. | Assemble filename/part parsing, yEnc recovery metadata, poster queue, crosspost data, catalog/raw debug. | One support row per retained article header. The legacy payload purge is audit-blocked because it depended on `assembled_at`. | High. Payload deletion can break recovery/debug/rebuild workflows unless archive/source lineage gates and durable replacement snapshots are proven. |
| `article_header_crosspost_groups` | Scrape payload/crosspost materialization from Xref groups. | Crosspost popularity and grouping context. | Raw evidence; summaries/projections may supersede some uses. | Medium. Large (`25 GB`) but not implemented as a purge. Only purge rows after verifying summaries/projections are complete and no current release/grouping/debug path needs raw rows. |
| `binary_core` | Assemble and yEnc recovery promotion. | Binary root for observation, identity, recovery, inspection, release files, archive lineage. FK cascade anchor. | Deleted only through terminal release source purge when the binary is safe and not shared with active releases. | High. Do not purge directly except via `maintenance.release_source_purge` preflight. |
| `binary_parts` | Assemble maps article headers to binary parts. | NZB segment generation, completion, release/catalog detail, source purge lineage. | Release source purge may delete parts for archived/purged releases if the binary is not shared with active releases. | High. Deleting early destroys segment lineage and NZB rebuild ability. |
| `binary_identity_current` | Assemble creates/updates current identity; yEnc and binary recovery can promote stronger identity. | Release formation fan-out, release summaries, inspection ready queues, yEnc candidate selection, catalog/admin detail. | Current projection for each `binary_core` row. Removed by cascade when `binary_core` is terminal-purged. | High. It is not disposable cache while binaries are active. Its 59 GB footprint mostly reflects active/current binary rows, not archived purge residue. |
| `binary_observation_stats` | Assemble/yEnc recovery maintain observed/total parts, posted time, bytes. | Completion, release readiness, source-age audit, candidate selection. | Current projection for each binary. | High unless parent binary is terminal-purged. |
| `binary_completion_keys` | Derived from binary identity/observation. | Release matching and completion key lookups. | Derived projection. | Medium; potentially rebuildable but expensive and currently not a cleanup target. |
| `binary_recovery_current` | Binary/yEnc recovery. | Avoids repeated recovery, promotes identity, release formation detail. | Current recovery state for a binary. | High/medium; purge only with binary root or a proven stale-derived policy. |
| `binary_grouping_evidence` | Grouping/matcher. | Debug/evidence and some cleanup predicates. | Stable evidence can be removed when current identity is strong and non-fallback. | Medium. Existing cleanup has conservative predicates; audit found `0` eligible rows. |
| `release_family_readiness_summaries` | Release summary refresh. | Release formation, yEnc candidate targeting, dashboard/backlog. | Derived summary. | Medium; rebuildable from retained binary/source projections, but expensive. Cleanup should be conservative and dry-run first. |
| `yenc_recovery_work_items` | yEnc work-item sync from weak/opaque binary identities and selected source headers. | `recover_yenc` claims `ready` rows, marks `running`, retries transient failures, marks recovered rows `done`, retires ineligible rows as `stale`. Release source purge deletes rows for purged binary roots. | `done` means yEnc header recovery succeeded or the binary was recognized as recovered; `ready` remains actionable backlog; `running` has an active lease. | High/medium. `ready` rows are active backlog, not trash. `done` rows may be removable only after proving current code no longer uses them as recovery suppression/audit state or after parent binary is terminal-purged. |
| `binary_inspection_ready_queue` | Pipeline ready-refresh stages. | Inspect stages claim ready rows. | `completed`/`blocked` rows with durable `binary_inspections` history are low-risk queue residue. | Low for completed/blocked rows with inspection history. Audit found `55,653`. |
| `indexer_stage_runs`, `scrape_runs` history | Supervisors/stages. | Admin UI, diagnostics, last-run status. | Old completed/failed history after retention window. | Low for old operational history; audit found `0` current eligible rows by configured predicates. |
| `binary_inspections` | Inspect stages. | Inspect rerun suppression, source freshness checks, ready-queue cleanup, release generation/archive gates, admin diagnostics. | Completed rows remain current inspection state until the binary/release is terminal-purged or a safer inspection-retention policy exists. | Medium/high. Do not purge merely because `status='completed'`; deleting completed rows can cause reinspection or block release archive gates. |
| `release_archive_state`, `release_archive_lineage_*` | NZB archive and source purge stages. | Source purge preflight, durable archive lineage, audit. | `purge_pending` means the release has archived NZB state and is waiting for terminal source cleanup. | High. Only `maintenance.release_source_purge` should delete source lineage, and only after release-ready/catalog/archive gates pass. |

### Low-Risk Queue And History Tables

`binary_inspection_ready_queue` is a claimable work queue, not durable
inspection history. Ready-refresh stages populate rows when a binary should be
inspected by discovery, PAR2, archive, media, password, or related inspect
stages. Inspect workers claim `ready` rows, transition them through `running`,
and leave `completed` or `blocked` queue rows after durable inspection history
exists in `binary_inspections`. The low-risk cleanup deletes only
`completed`/`blocked` queue rows that already have corresponding
`binary_inspections` history. It does not delete inspection history, binary
state, release detail, catalog files, or archive metadata.

`article_header_assembly_queue` is the current assemble backlog. Scrape inserts
headers here so assemble can claim and convert raw headers into `binary_core`
and `binary_parts`. A row is stale only when the same `article_header_id` is
already represented in `binary_parts`. A `0` stale count means the table is
currently doing useful backlog tracking; it does not mean the table is unused.
Purging the whole table would strand unassembled headers and stop release
formation for those queued articles.

`indexer_stage_runs` and `scrape_runs` are operational history. They feed the
admin UI, last-run status, diagnostics, and failure forensics. A `0` eligible
count means no rows exceed the configured retention windows. The cleanup deletes
only old completed/abandoned rows and older failed rows; it must not truncate
these tables because current UI and debugging flows depend on recent history.

`binary_inspections` looks like history, but completed rows are also current
inspection state. Release generation and archive selection require completed
archive/media inspection rows, inspect candidate selection uses rows as rerun
suppression and source freshness checks, and ready-queue cleanup uses them as
the durable proof that a queue row can be discarded. Do not purge completed
inspection rows simply because they are done.

### Crosspost Group Telemetry

`article_header_crosspost_groups` stores normalized groups parsed from scraped
`Xref` header text. Scrape/latest and scrape/backfill insert these raw
observations through the ingest materializer. Manual crosspost backfill can also
populate the same raw table from retained `article_header_ingest_payloads.xref`.
Each raw row is keyed by `(article_header_id, observed_group_name)` and keeps
the provider, source scrape newsgroup, message ID, observed group, observed
article number, and observation time.

The current runtime consumer of raw crosspost rows is
`crosspost_popularity_refresh`. That stage claims group names from
`crosspost_popularity_refresh_queue`, reads raw rows newer than each group's
`article_header_crosspost_group_summary.last_refreshed_article_header_id`, and
increments the summary's article count, distinct message count, distinct source
group count, last seen time, and watermark. After refresh, the queue row is
marked `done`.

The admin scrape crosspost popularity report does not read the raw table. It
reads `article_header_crosspost_group_summary` through
`GetIndexerCrosspostNewsgroupPopularity`, then overlays whether the group is
already in the effective scrape configuration. This report is discovery and
operator telemetry; it is not canonical file lineage.

Release formation, release summary refresh, assemble, and matcher code paths do
not currently read `article_header_crosspost_groups` directly. Cross-newsgroup
release formation is handled through binary identity and release-family
projections instead:

- `binary_core.newsgroup_id` keeps each binary tied to one source newsgroup.
- `ListExistingReleaseCandidates` builds release-family candidates at provider
  scope and returns `newsgroup_id = 0` for those candidates.
- `ListBinariesForReleaseCandidate` intentionally ignores the candidate
  newsgroup for `release_family` and recovered-file-set candidates, so matching
  binaries can come from multiple newsgroups.
- `release.Service` clusters those binaries, derives the participating
  newsgroup IDs from the cluster binaries, and `PersistReleaseSnapshot` writes
  them into `release_newsgroups`.

Planning docs describe Xref groups as contextual evidence for weak family
formation, but the current implementation stores raw Xref observations as
scrape/reporting telemetry and does not use raw crosspost rows to rewrite binary
provenance or release file membership.

Live audit on 2026-06-23:

| Item | Count/size |
| --- | ---: |
| Raw rows | `54,706,065` |
| Raw table total size | `25 GB` |
| Heap size | `7.6 GB` |
| Index size | `18 GB` |
| Summary rows | `181` |
| Refresh queue rows | `181`, all `done` |
| Raw rows with a summary group | `54,706,065` |
| Raw rows at or below summary watermark | `54,706,065` |
| Raw rows above summary watermark | `0` |
| Raw rows without summary group | `0` |
| Raw rows older than 72h | `39,519,896` |
| Raw rows newer than 72h | `15,186,169` |

Interpretation: every current raw crosspost row has been incorporated into the
summary watermark. From the current code path, raw rows are not needed for the
admin popularity report until new observations arrive and queue rows are
refreshed. However, deleting all raw rows would make future incremental
watermark accounting unable to recompute or validate historical distinct counts
from source, and it would remove forensic Xref evidence. A safe purge policy
should therefore be explicit that crosspost raw rows are reporting telemetry,
not release lineage, and should retain at least a recent window for diagnostics
and future summary corrections.

Implemented maintenance policy:

- keep raw rows newer than a configured retention window, for example `72h`;
- delete only rows whose `article_header_id` is less than or equal to the
  matching summary row's `last_refreshed_article_header_id`;
- do not delete rows for groups currently `pending`, `failed`, or `processing`
  in `crosspost_popularity_refresh_queue`;
- keep `article_header_crosspost_group_summary` and
  `crosspost_popularity_refresh_queue` intact;
- run as medium-risk batch-limited maintenance task
  `crosspost_group_raw_purge`, default batch size `250,000`, manual scheduling
  disabled by default.

The task does not delete source headers, payloads, binaries, release rows,
release catalog files, archive metadata, or NZBs. The tradeoff is that raw
historical Xref telemetry older than the retention window is no longer available
for forensic debugging or summary recompute.

### Post-Delete Vacuum Policy

Maintenance purge tasks run a normal `VACUUM (ANALYZE)` on tables where they
delete rows. This is safe to run as the application database user and keeps the
deleted space reusable inside PostgreSQL while refreshing planner statistics.
The maintenance UI reports the vacuumed table list separately from the delete
result and labels this as reusable PostgreSQL space, not OS space.

The maintenance API/UI also exposes `vacuum_dead_tuple_tables`, a low-risk,
non-destructive task that selects public tables from `pg_stat_user_tables` when
dead tuples are high enough to matter and runs plain `VACUUM (ANALYZE)` on at
most `batch_size` tables per run. It deletes no application rows, enqueues no
pipeline work, and is useful after high-churn writes or after manual purge
tasks. Schedule is disabled by default because large-table vacuum can add I/O
while scrape, assemble, yEnc recovery, release, or inspect stages are running.
Candidate selection intentionally avoids repeatedly vacuuming huge low-bloat
tables: it targets tables with at least `1M` dead tuples, at least `50k` dead
tuples and `2%` dead-tuple share, or at least `250k` dead tuples plus `1%`
dead-tuple share on relations over `1 GB`.

Do not run `VACUUM FULL`, `CLUSTER`, `pg_repack`, or table-swap rewrites
automatically after a purge. Those actions can return space to the filesystem,
but they carry stronger locking, extension, operational, or disk-headroom
requirements and should remain explicit admin actions.

Rewrite decision rule:

- `pg_stat_user_tables.n_dead_tup` answers "does this table need normal
  vacuum/analyze soon?"
- `pg_total_relation_size`, `pg_relation_size`, `pg_indexes_size`, and
  `pgstattuple_approx` answer "is enough reusable/free space stranded inside
  this table to justify a rewrite that returns OS disk?"
- PostgreSQL does not keep a single built-in "safe to VACUUM FULL" flag. The
  operator must combine bloat/free-space estimates, table size, lock tolerance,
  current stage activity, and volume free-space headroom.
- `pg_repack` is preferred for production-style OS-space reclaim when the
  extension/package is installed because it rewrites with much less blocking
  than `VACUUM FULL`, though it still needs extra disk and a final lock.
- `VACUUM FULL` is the built-in fallback when downtime/locks are acceptable. It
  rewrites one table and its indexes, returns disk to the filesystem, and should
  be run one table at a time with supervisors stopped or guarded.
- `CLUSTER` is a table rewrite with physical ordering by an index. Use it only
  when the ordering itself is desired; otherwise it is not better than
  `VACUUM FULL` for this reclaim use case.
- Manual table swap/rewrite is an emergency, table-specific operation for
  simple queue tables after constraints and indexes are audited. It is not a
  general substitute for `pg_repack`.

Explicit OS reclaim commands:

```bash
# Application CLI: safe preflight for the allowlisted reclaim tables.
go run ./cmd/gonzb --config config.yaml indexer maintenance reclaim-storage --check

# Application CLI: plain vacuum/analyze, makes space reusable inside Postgres.
go run ./cmd/gonzb --config config.yaml indexer maintenance reclaim-storage yenc-work headers payloads

# Postgres-user CLI: built-in OS-space reclaim for one table at a time.
# Requires an exclusive table lock and enough temporary headroom for the rewrite.
docker exec -u postgres gonzb-postgres psql -d gonzb -c 'VACUUM (FULL, ANALYZE) "article_header_crosspost_groups";'

# Optional lower-lock production path when pg_repack is installed in the image/host.
docker exec -u postgres gonzb-postgres psql -d gonzb -c 'CREATE EXTENSION IF NOT EXISTS pg_repack;'
docker exec -u postgres gonzb-postgres pg_repack -d gonzb -t article_header_crosspost_groups
```

Use `pg_repack` first when the package is available and there is enough free
space for a shadow copy plus indexes. Use `VACUUM FULL` when the table can be
locked and downtime is acceptable. If neither has enough disk headroom, add
temporary storage or use a table-specific swap/rewrite only after auditing the
schema, constraints, indexes, and retained-row predicate.

Live bloat notes from 2026-06-23:

- `article_header_ingest_payloads` had about `1.78M` estimated dead tuples in
  `pg_stat_user_tables`, but `pgstattuple_approx` showed only about `24 MB`
  dead tuple bytes and `416 MB` heap free space. Run normal vacuum/analyze
  first; it is not an OS-space rewrite priority based on this sample.
- `binary_identity_current` is large (`59 GB` total including indexes) but had
  low dead-tuple pressure (`~0.67%` by stats, `~0.02%` dead bytes by
  `pgstattuple_approx`). It is current release-formation projection data and
  should not be purged directly. Consider rewrite only after a dedicated
  bloat/lock audit.
- `article_header_crosspost_groups` had zero dead tuples after cleanup/vacuum,
  but `pgstattuple_approx` showed about `5.4 GB` free inside the heap. If OS
  space must be returned next, this table is a stronger explicit rewrite or
  `pg_repack` candidate than `article_header_ingest_payloads`.
- `yenc_recovery_work_items` had zero dead tuples after cleanup/vacuum and
  about `2.0 GB` heap free space. It is a possible later rewrite candidate, but
  less attractive than `article_header_crosspost_groups` on the current audit.

## Live Audit Interpretation

### Poster Materialization Queue

`poster_materialization_queue` had about `48,119,923` `done` rows. These are
completed work receipts. The durable data they produced is in `posters` and
`article_header_poster_refs`.

Pros of purging `done` rows:

- Reduces a 13 GB table and its indexes internally.
- Does not delete source headers, binaries, releases, catalog files, or NZBs.
- Does not require recomputing poster refs because refs are already materialized.

Cons and constraints:

- The cleanup must remain batch-limited. On the current full disk, a large
  unbounded delete is risky because it can generate large WAL and table churn.
- Deletes do not return OS space without a table rewrite or similar maintenance.
- The maintenance task records before/after database, filesystem, and
  `poster_materialization_queue` relation snapshots. Expect table dead tuples
  and reusable PostgreSQL space to change before host free space changes.
- Use repeated manual runs or scheduled runs with a conservative `batch_size`
  to drain the queue before considering `VACUUM FULL`, `pg_repack`, `CLUSTER`,
  or another table rewrite to return space to the OS.

Emergency reclaim note from 2026-06-23:

- Live status counts were `48,118,923 done` and `186,644 pending`.
- Because the retained non-`done` set was tiny, the safe emergency path was a
  table swap: create a replacement table with the same schema, copy only
  `status <> 'done'`, recreate constraints/index names, swap names under an
  exclusive table lock, and drop the old table.
- Result: `poster_materialization_queue` dropped from about `13 GB` to `53 MB`,
  database size dropped from `266 GB` to `253 GB`, and host free space on
  `/mnt/vm-store` rose from about `912 MB` to `15 GB`.
- This is a stronger operation than the normal batch delete. It is appropriate
  only when app writers are stopped or guarded, the retained set is verified,
  and constraints/indexes are verified after the swap.

### Binary Identity

`binary_identity_current` is large because it is the current identity projection
for nearly every active binary. It is heavily used by release formation,
inspection ready queues, yEnc candidate selection, catalog/admin detail, and
readiness summaries.

Live family-kind distribution:

| Family kind | Rows |
| --- | ---: |
| `opaque_set` | 29,439,995 |
| `contextual_obfuscated` | 8,573,257 |
| `readable_title` | 1,649,400 |
| `archive_stem` | 317,128 |

This is not mostly archived-release residue. Only `620` binaries were present
in `release_archive_lineage_binaries`, and only `554` `release_files` rows were
associated with `purge_pending` archive state. A release-source dry-run for the
first 50 candidates estimated only `294` `binary_core` rows and their cascaded
identity rows would be removed.

Conclusion: do not purge `binary_identity_current` directly. It shrinks safely
only when `binary_core` roots are terminal-purged after archive/catalog gates.

### yEnc Recovery Work Items

Live status distribution:

| Status | Rows |
| --- | ---: |
| `ready` | 25,633,585 |
| `done` | 3,602,363 |
| `running` | 500 |

Age distribution:

| Status | <72h | 72h-7d | >7d | Total |
| --- | ---: | ---: | ---: | ---: |
| `ready` | 9,246,131 | 16,387,454 | 0 | 25,633,585 |
| `done` | 2,229,084 | 1,373,279 | 0 | 3,602,363 |
| `running` | 500 | 0 | 0 | 500 |

`ready` is active recovery backlog. It may be old, but it is still work the
`recover_yenc` stage can consume to promote opaque/weak identities. `running`
is an active blocker for source cleanup. `done` is a completed work receipt.
Recovery durability lives in `binary_recovery_current`, promoted binary
identity/stat projections, `binary_superseded_sources`, release files, and
archive lineage rather than in the queue receipt itself.

Code audit:

- `BackfillYEncRecoveryWorkItems` seeds queue rows from opaque/weak binary
  identity and readiness state. It only suppresses re-seeding when an
  up-to-date `ready` row exists. It also excludes binaries whose
  `binary_recovery_current.recovered_source` is `yenc_header`.
- `recover_yenc` claims only `ready` rows with nonblank message IDs and expired
  backoff, temporarily marks them `running`, and then writes success/failure
  state.
- Successful recovery writes durable projection state and marks related work
  items `done`.
- Failed/not-found/no-op recovery does not become purgeable; those rows are put
  back to `ready` with backoff.
- Release source purge deletes yEnc work rows only when the parent binary root
  itself is terminal-purged.

Safe implemented cleanup:

- task key: `yenc_done_work_item_cleanup`;
- medium risk, batch-limited, manual scheduling disabled by default;
- deletes only `status='done'` rows older than `72h`;
- requires `binary_recovery_current.recovered_source='yenc_header'`;
- skips rows referenced by `release_files` or
  `release_archive_lineage_binaries`;
- skips rows with running inspect ready/history work;
- keeps `ready` and `running` recovery backlog intact;
- keeps article headers, payloads, binary roots, recovery projections, release
  files, archive lineage, catalog data, and NZBs.

Live audit on 2026-06-23 found `1,371,604` rows matching the implemented
cleanup predicate. That is the only yEnc work-item pool currently considered
safe to purge.

### Release Source Purge

Direct audit:

- `release_archive_state purge_pending`: `88`
- Missing catalog among purge-pending: `0`
- Missing media inspect among purge-pending: `0`

App dry-run for the first 50 default-policy candidates estimated:

| Table | Rows |
| --- | ---: |
| `release_archive_state` | 50 |
| `release_files` | 306 |
| `release_newsgroups` | 129 |
| `nzb_cache` | 50 |
| `release_archive_lineage_article_headers` | 46,036 |
| `release_archive_lineage_binaries` | 294 |
| `binary_core` | 294 |
| `binary_parts` | 46,036 |
| `yenc_recovery_work_items` | 234 |
| `article_headers` | 0 |
| `article_header_ingest_payloads` | 0 |

This is high-risk but properly gated. It will not reclaim large OS space by
itself, and the first batch is small relative to the database.

### Emergency Source Window Reset

`emergency_source_window_reset` is the high-risk test/admin path for a host that
accidentally scraped or assembled outside the intended active window and now
needs to discard stale, unreleased backlog without dropping the database. It is
manual by default and uses the same fixed `7` day retention window as the
source-window guard.

What it deletes in each batch:

- eligible `binary_core` rows older than `7` days by
  `binary_observation_stats.posted_at`;
- FK-cascaded binary rows: `binary_parts`, `binary_identity_current`,
  `binary_observation_stats`, `binary_recovery_current`,
  `binary_grouping_evidence`, `binary_inspection_ready_queue`,
  `binary_inspections`, `binary_inspection_artifacts`,
  `binary_archive_entries`, `binary_text_evidence`, `binary_media_streams`,
  `binary_par2_sets`, `binary_par2_targets`, `binary_completion_keys`,
  `binary_lifecycle`, `binary_projection_events`,
  `binary_superseded_sources`, and `yenc_recovery_work_items`;
- then, after the binary cascade, fully orphaned old `article_headers` and
  their cascaded `article_header_ingest_payloads`,
  `article_header_crosspost_groups`, `article_header_poster_refs`, and
  `poster_materialization_queue` rows.

What it preserves:

- any binary referenced by `release_files`;
- any binary referenced by `release_archive_lineage_binaries`;
- binaries with running inspect queue/history work;
- binaries with running yEnc work;
- public release detail, `release_catalog_files`, archive metadata, archived
  NZB objects, and archive lineage rows.

Tradeoff:

- This is intentionally destructive for old unformed work. Future releases
  cannot be created from the purged binary/source/yEnc rows.
- It is the right test for a too-small database partition scenario where the
  admin would rather discard old unreleased backlog than drop the whole
  database.
- It still does not return OS space by itself. Run plain vacuum for internal
  reuse, then use `pg_repack` or `VACUUM FULL` on the affected largest tables
  when the system has lock/disk headroom.

Run examples:

```bash
# Dry-run is the default for CLI task execution. It estimates staged binary
# cascades without deleting. Use stale_nonrelease_source_purge dry-run for
# source-header-only estimates.
go run ./cmd/gonzb --config config.yaml indexer maintenance task emergency_source_window_reset --batch-size 10000

# Commit one batch after reviewing the dry-run.
go run ./cmd/gonzb --config config.yaml indexer maintenance task emergency_source_window_reset --dry-run=false --batch-size 10000
```

## Current Recommendations

1. Run low-risk cleanup deletes only through batch-limited maintenance tasks.
   `poster_queue_done_cleanup` is the highest-volume low-risk target and now
   deletes at most the configured `batch_size` per run with before/after storage
   snapshots.
2. Do not use `article_headers.assembled_at` for purge decisions.
   The live queue-based assemble path consumes `article_header_assembly_queue`
   and writes `binary_parts`; `assembled_at` is legacy state and can be null for
   rows that already have `binary_parts`.
3. Keep stale source purge separate from scrape gating. Runtime source windows
   now pause scheduled scrape on open assemble or blocking yEnc backlog and cap
   backfill to `source_window.backfill_window_days` (default: 7 days), but they
   do not delete existing old headers.
4. Run `crosspost_group_raw_purge` as medium-risk cleanup only after reviewing
   the dry-run count. It removes raw crosspost telemetry older than 72h after
   summary watermark consumption, but it does not participate in current release
   formation.
5. Run `yenc_done_work_item_cleanup` only for completed receipts older than 72h
   after durable yEnc recovery projection exists and release/archive/running
   inspect guards pass. It keeps ready/running recovery backlog intact.
6. Use `emergency_source_window_reset` only after dry-run when old unreleased
   binary/source/yEnc backlog outside the 7-day window must be discarded. It
   preserves release/archive lineage but prevents future release formation from
   the purged old source rows.
7. Run `vacuum_dead_tuple_tables` after large purge batches or when the storage
   audit shows high dead tuples. This is low-risk and non-destructive, but it
   can add I/O on selected tables.
8. Treat `header_payload_purge` as disabled/audit-blocked until it is rewritten
   with relationship guards instead of `assembled_at`.
9. Use `maintenance.release_source_purge` only as terminal cleanup.
   It is safe by design for eligible archived releases, but it is not the
   emergency space lever for this dataset.

## Report Fields Added To The App

The maintenance storage audit now includes:

- largest tables
- largest indexes
- source age windows
- purge guard counts
- cleanup decision matrix with low/medium/high risk, data effect, supervisor
  effect, space effect, and release-safety notes

Expensive optional counts are allowed to report `-1` with an error note instead
of failing the entire maintenance page.
