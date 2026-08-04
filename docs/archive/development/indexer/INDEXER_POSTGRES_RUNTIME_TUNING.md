# Indexer Postgres Runtime Tuning

Status: archived development reference

Snapshot date: 2026-04-22

This document records a historical PostgreSQL and runtime tuning baseline captured during the backlog burn-down phase.

Keep it for:

- the current developer-laptop PostgreSQL baseline
- the exact settings applied to the local `gonzb-postgres` container
- the hot-table autovacuum and statistics policy now in effect
- before/after baseline numbers for the current dev environment
- tiered tuning guidance for lower-end and production systems

Use `docs/archive/completed/indexer/INDEXER_BACKLOG_BURNDOWN_PERFORMANCE_PLAN.md` for the completed execution history of the backlog burn-down pass.

This file is preserved for historical context and old local validation notes. It should not be treated as the current primary runtime guidance for the project.

## Environment Snapshot

Current host profile used for this baseline:

- host: `Linux T14 6.19.9-arch1-1 x86_64`
- CPU: `8` cores
- RAM: `23 GiB`
- storage: NVMe-backed host volume
- free space on working volume: about `43 GiB`
- PostgreSQL container: `gonzb-postgres`
- PostgreSQL image: `postgres:17`

Interpretation:

- this is a solid developer workstation
- it is not the production sizing target
- the baseline should optimize for reliable local validation without consuming the whole machine

## Work Completed

Completed on `2026-04-22`:

1. captured the active host and PostgreSQL baseline
2. updated the tracked Docker Compose PostgreSQL settings for the local dev container
3. applied tighter hot-table autovacuum/analyze policy with live `psql` commands
4. raised statistics targets on selector-critical columns
5. restarted the PostgreSQL container and verified the new runtime settings
6. ran fresh `VACUUM (ANALYZE)` on:
   - `article_headers`
   - `binaries`
   - `release_stage_dirty_families`
7. measured recent stage-run timings and inspected the current hot query plans with `EXPLAIN (ANALYZE, BUFFERS)`

## Before And After Settings

Observed before tuning:

- `shared_buffers = 128MB`
- `effective_cache_size = 4GB`
- `work_mem = 4MB`
- `maintenance_work_mem = 64MB`
- `random_page_cost = 4`
- `effective_io_concurrency = 1`
- `track_io_timing = off`
- `default_statistics_target = 100`
- `jit = on`

Current live baseline after tuning:

- `shared_buffers = 1GB`
- `effective_cache_size = 8GB`
- `work_mem = 16MB`
- `maintenance_work_mem = 512MB`
- `random_page_cost = 1.1`
- `effective_io_concurrency = 64`
- `track_io_timing = on`
- `default_statistics_target = 250`
- `jit = off`

Notes:

- the application already forces session `jit=off` in the pgx connection config for the indexer store
- keeping `jit=off` at the PostgreSQL server level avoids surprises during local validation and ad hoc SQL inspection
- `effective_io_concurrency = 64` was chosen as a laptop-safe baseline even though the host storage is NVMe-backed
- for dedicated production NVMe hosts, moving this higher is reasonable after measurement

## Compose Baseline

The tracked baseline now lives in [docker-compose.postgres.yml](../../docker-compose.postgres.yml).

Current Postgres command overrides:

```yaml
command:
  - postgres
  - -c
  - shared_buffers=1GB
  - -c
  - effective_cache_size=8GB
  - -c
  - work_mem=16MB
  - -c
  - maintenance_work_mem=512MB
  - -c
  - random_page_cost=1.1
  - -c
  - effective_io_concurrency=64
  - -c
  - track_io_timing=on
  - -c
  - default_statistics_target=250
  - -c
  - jit=off
```

## Hot-Table Maintenance Policy

The following table-level storage parameters were applied live with `ALTER TABLE ... SET (...)`:

- `article_headers`
  - `autovacuum_vacuum_scale_factor = 0.02`
  - `autovacuum_vacuum_threshold = 5000`
  - `autovacuum_analyze_scale_factor = 0.005`
  - `autovacuum_analyze_threshold = 2000`
- `binaries`
  - `autovacuum_vacuum_scale_factor = 0.02`
  - `autovacuum_vacuum_threshold = 1000`
  - `autovacuum_analyze_scale_factor = 0.01`
  - `autovacuum_analyze_threshold = 500`
- `release_stage_dirty_families`
  - `autovacuum_vacuum_scale_factor = 0.01`
  - `autovacuum_vacuum_threshold = 500`
  - `autovacuum_analyze_scale_factor = 0.005`
  - `autovacuum_analyze_threshold = 200`

Higher statistics targets were also applied to selector-critical columns:

- `article_headers.assembled_at`
- `article_headers.provider_id`
- `article_headers.newsgroup_id`
- `binaries.provider_id`
- `binaries.newsgroup_id`
- `binaries.release_family_key`
- `binaries.base_stem`
- `binaries.file_name`
- `binaries.expected_file_count`
- `release_stage_dirty_families.provider_id`
- `release_stage_dirty_families.newsgroup_id`
- `release_stage_dirty_families.key_kind`
- `release_stage_dirty_families.family_key`

Target used:

- `SET STATISTICS 500`

## Fresh Maintenance Verification

After the settings change, manual `VACUUM (ANALYZE)` completed successfully on all three hot tables.

Verification snapshot after the manual pass:

- `article_headers`
  - `n_live_tup = 2,374,221`
  - `n_dead_tup = 0`
  - `last_vacuum = 2026-04-22 10:41:25 EDT`
  - `last_analyze = 2026-04-22 10:41:27 EDT`
- `binaries`
  - `n_live_tup = 68,142`
  - `n_dead_tup = 0`
  - `last_vacuum = 2026-04-22 10:41:27 EDT`
  - `last_analyze = 2026-04-22 10:41:31 EDT`
- `release_stage_dirty_families`
  - `n_live_tup = 18,801`
  - `n_dead_tup = 0`
  - `last_vacuum = 2026-04-22 10:41:31 EDT`
  - `last_analyze = 2026-04-22 10:41:31 EDT`

## Current Backlog Snapshot

Measured after the tuning pass:

- pending unassembled headers: `1,120,596`
- near-complete releases (`90%` to `<100%`): `45`
- dirty-family rows:
  - `release_family = 7,742`
  - `base_stem = 7,716`

These numbers confirm that this system is still in a meaningful live-workload state for WorkStreams 2 and 3.

## Stage Runtime Snapshot

Isolated manual validation was rerun after background `assemble`, `release`, and `inspect` processes were stopped and stale leases were repaired.

Clean manual pass summary from `2026-04-22 10:54 EDT` through `10:58 EDT`:

- assemble, `3` manual `--once` runs:
  - average `20.07s`
  - min `13.57s`
  - max `23.98s`
- release, `3` manual `--once` runs:
  - average `64.92s`
  - min `62.26s`
  - max `69.56s`

Run-by-run detail:

- assemble:
  - run 1: `23.98s`
  - run 2: `22.67s`
  - run 3: `13.57s`
- release:
  - run 1: `69.56s`
  - run 2: `62.95s`
  - run 3: `62.26s`

Supporting live-history snapshot around the initial tuning change at `2026-04-22 10:41:11 EDT`:

- assemble, last `10` completed runs before tuning:
  - average `39.61s`
  - min `35.81s`
  - max `45.21s`
- assemble, last `10` completed runs after tuning but before isolated reruns:
  - average `29.12s`
  - min `17.69s`
  - max `59.10s`
- release, last `5` completed runs before tuning:
  - average `74.39s`
  - min `70.05s`
  - max `81.82s`

Interpretation:

- assemble improved meaningfully after the tuning change on this host
- the isolated reruns show cleaner assemble behavior once competing schedulers are removed
- release improved versus the earlier noisy post-tuning scheduler sample, but it is still structurally expensive, which matches the active plan’s expectation that WorkStream 3 is still required
- these measurements are useful as a baseline, not as the final optimization ceiling

## Hot Query Plan Snapshot

Focused `EXPLAIN (ANALYZE, BUFFERS)` checks were run against the current assemble and release selector queries.

Observed results:

- release candidate selection:
  - execution time about `605.6 ms`
  - mostly shared-buffer hits
  - no sign that local disk latency is the primary blocker
- assemble candidate selection:
  - execution time about `1844.2 ms`
  - `recent_pending` expands to `125,000` rows for the current `2,500` batch size
  - temp writes are present
  - lane A returned `0` rows in this snapshot, so lane B filled the batch

Interpretation:

- PostgreSQL is no longer on obviously inappropriate laptop defaults
- release selection is still dominated by repeated family aggregation, not by server misconfiguration
- assemble selection is still carrying structural cost from the large pending-window strategy
- this aligns with the next active work:
  - WorkStream 2: binary-driven completion-first assemble selection
  - WorkStream 3: release family readiness summary state

## 2026-05-26 Assemble Batch Persistence Follow-Up

Live `assemble lane-b --once` reruns on 2026-05-26 validated the post-selector assemble write path again after true binary-upsert batching was introduced.

Observed behavior:

- large logical worker batches of `15000` headers completed cleanly with `4` workers once binary upserts were internally chunked to `250` records per DB transaction
- the first larger chunk attempt failed with `OUT OF SHARED MEMORY` because too many advisory locks were held in one transaction
- larger `INSERT ... ON CONFLICT` batches also exposed deadlocks until request ordering was made deterministic
- after reducing internal chunk size and committing per chunk, DB activity shifted to short-lived sub-second to roughly one-second upsert queries rather than one large lock-heavy statement

Observed worker metrics from the clean rerun:

| Worker batch | Unique binary upserts | Headers/sec | Binary upsert ms | Binary refresh ms | Binary part upsert ms |
| ---: | ---: | ---: | ---: | ---: | ---: |
| `15000` | `8190` | `254.06` | `37399.02` | `14045.93` | `4323.02` |
| `15000` | `8443` | `197.98` | `39092.44` | `29243.82` | `3891.23` |
| `15000` | `12496` | `180.99` | `54960.88` | `20845.27` | `4345.62` |
| `15000` | `12846` | `148.11` | `57683.84` | `36487.58` | `4121.23` |

Recommendation:

- keep total assemble selection batch size and internal DB upsert chunk size as separate concepts
- do not derive DB upsert chunk size as a percentage of total batch size
- expose DB upsert chunk size, if exposed at all, as an advanced integer setting with a conservative default such as `250`
- reasoning:
  - total selected headers do not predict unique-binary density reliably
  - advisory lock pressure and row-lock conflict risk scale with unique binary count per worker, not raw header count
  - a percentage-derived chunk size becomes unsafe as soon as a batch is highly fragmented and approaches one unique binary per header

Practical tuning posture:

- user-facing stage setting:
  - `assemble batch size`
- advanced persistence setting:
  - `binary upsert DB chunk size`
- recommended starting default:
  - `250`
- recommended trial range:
  - `100` to `500`

## Tiered Recommendations

### Dev Laptop

Use this when the goal is local development and repeatable validation.

- storage: SSD preferred, NVMe ideal
- RAM: `16 GB` to `32 GB`
- CPU: `6` to `8+` cores
- baseline:
  - `shared_buffers = 1GB`
  - `effective_cache_size = 8GB`
  - `work_mem = 16MB`
  - `maintenance_work_mem = 512MB`
  - `random_page_cost = 1.1`
  - `effective_io_concurrency = 64`
  - `track_io_timing = on`
  - `default_statistics_target = 250`
  - `jit = off`

### Lower-End Self-Hosted System

Use this when hardware is constrained but still needs to run the PostgreSQL-backed indexer safely.

- storage: SSD strongly recommended
- RAM: `8 GB` practical floor
- CPU: `4` cores workable
- starting posture:
  - `shared_buffers = 512MB` to `1GB`
  - `effective_cache_size = 2GB` to `4GB`
  - `work_mem = 8MB` to `16MB`
  - `maintenance_work_mem = 256MB`
  - `random_page_cost = 1.25` to `1.75` on SSD
  - `effective_io_concurrency = 32` to `64`
  - `track_io_timing = on`
  - `default_statistics_target = 100` to `250`
  - `jit = off`

### Production Server

Use this when the goal is sustained throughput and final sign-off.

- storage: NVMe or strong SSD
- RAM: `32 GB+`
- CPU: `8+` real cores
- keep generous free disk space for ongoing churn
- starting posture:
  - `shared_buffers` near `25%` of RAM
  - `effective_cache_size` near `50%` to `75%` of RAM
  - `work_mem = 16MB` to `32MB`
  - `maintenance_work_mem = 1GB+`
  - `random_page_cost = 1.1`
  - `effective_io_concurrency = 128` to `256` on NVMe
  - `track_io_timing = on`
  - `default_statistics_target = 250+`
  - `jit = off`

## Commands Used

The following classes of commands were used during this pass:

- live setting verification from the container:

```bash
docker exec gonzb-postgres psql -U postgres -d gonzb -Atc "show shared_buffers; ..."
```

- table-level storage parameter changes:

```sql
ALTER TABLE article_headers SET (...);
ALTER TABLE binaries SET (...);
ALTER TABLE release_stage_dirty_families SET (...);
ALTER TABLE ... ALTER COLUMN ... SET STATISTICS 500;
```

- manual maintenance:

```sql
VACUUM (ANALYZE) article_headers;
VACUUM (ANALYZE) binaries;
VACUUM (ANALYZE) release_stage_dirty_families;
```

## Reclaim Runbook For Growth-Trim Tables

Use this when row retention has already been reduced and the host filesystem still has not recovered space. `VACUUM (ANALYZE)` updates planner stats and marks space reusable inside PostgreSQL. It does not usually shrink table files on disk. Use `VACUUM FULL` only when you need bytes returned to the Docker volume and host filesystem.

When to use it:

- after application-side retention cleanup is already in place
- after a normal maintenance run has removed rows successfully
- after a follow-up `VACUUM (ANALYZE)` confirms dead tuples are no longer the main blocker
- when host free space is still too tight for the next ingest or maintenance cycle

Operational constraints:

- stop the app and any background indexer stages first
- run one table at a time
- expect an exclusive lock for the duration of each table rewrite
- make sure the Docker volume and underlying host filesystem both have enough temporary free space for the rewrite
- prefer running the smallest rewrite first so you learn whether space is being returned as expected before touching the largest table

Recommended order for the current growth-trim sprint:

1. `release_family_readiness_summaries`
2. `binary_grouping_evidence`
3. `article_header_ingest_payloads`

Recommended command pattern from the host:

```bash
go run ./cmd/gonzb --config config.yaml indexer maintenance reclaim-storage --full readiness
```

Then continue with:

```bash
go run ./cmd/gonzb --config config.yaml indexer maintenance reclaim-storage --check
go run ./cmd/gonzb --config config.yaml indexer maintenance reclaim-storage --full grouping-evidence
go run ./cmd/gonzb --config config.yaml indexer maintenance reclaim-storage --full payloads
```

Recommended preflight on a tight dev machine:

```bash
go run ./cmd/gonzb --config config.yaml indexer maintenance reclaim-storage --check
```

That reports the current bytes for the allowlisted tables in the same execution order without running `VACUUM`.

Direct `psql` fallback:

```bash
docker exec -it gonzb-postgres \
  psql -U postgres -d gonzb \
  -c "VACUUM (FULL, ANALYZE) release_family_readiness_summaries;"
```

Repeat the same pattern for the next table only after the previous command completes and host free space is rechecked.

Suggested live checks between tables:

```sql
SELECT pg_size_pretty(pg_total_relation_size('release_family_readiness_summaries'));
SELECT pg_size_pretty(pg_total_relation_size('binary_grouping_evidence'));
SELECT pg_size_pretty(pg_total_relation_size('article_header_ingest_payloads'));
```

Docker-volume note:

- nothing special is required just because PostgreSQL is running in Docker
- the reclaimed bytes return to the filesystem that backs the Postgres data directory, not to the running process directly
- if the table rewrite cannot finish because the host or volume is already too full, `VACUUM FULL` can fail partway through, so do not start with the largest table first on a nearly-full dev machine

- plan inspection:

```sql
EXPLAIN (ANALYZE, BUFFERS) ...
```

- isolated manual validation:

```bash
go run ./cmd/gonzb --config config.yaml indexer maintenance repair-runtime
go run ./cmd/gonzb --config config.yaml indexer assemble --once
go run ./cmd/gonzb --config config.yaml indexer release --once
```

- repo health check:

```bash
go test ./...
```

## Final Validation Snapshot

Measured after the isolated manual reruns:

- pending unassembled headers: `1,111,001`
- near-complete releases (`90%` to `<100%`): `47`
- dirty-family rows:
  - `release_family = 6,046`
  - `base_stem = 6,022`

Additional validation:

- `go test ./...` passed on `2026-04-22`

## WorkStream 1 Sign-Off

WorkStream 1 is signed off as complete for the current dev-laptop baseline.

Sign-off basis:

- PostgreSQL tuning was implemented and persisted in the repo
- hot-table autovacuum/analyze policy was tightened and verified live
- fresh `VACUUM (ANALYZE)` completed on the target tables
- before/after configuration and runtime measurements were captured
- isolated manual `assemble --once` and `release --once` validation was rerun after clearing background stage activity
- the Go test suite passed after the tuning changes and doc updates
- the remaining major costs now point to selector and queue design, not obviously bad PostgreSQL defaults

What this sign-off does not mean:

- it does not mean throughput is fully optimized
- it does not mean production sizing is done
- it does not replace WorkStreams 2 and 3

It means the baseline PostgreSQL/runtime tuning pass is complete enough to stop being the active blocker for the next workstreams.
