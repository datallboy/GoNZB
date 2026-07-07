# GoNZB yEnc Recovery Retention / Throughput Audit Plan

## Purpose

Audit and redesign the current GoNZB scrape → assemble → recover_yenc → release pipeline so yEnc recovery can no longer create multi-million article backlogs, uncontrolled database growth, or freshness stalls.

This is not a quick patch. Treat this as a longer Codex session. First inspect the current code, schema, migrations, config, and worker behavior. Then decide the safest implementation path and make incremental changes with tests/metrics.

## Current Problem Summary

GoNZB already has a split assembly path:

- Lane A: finish incomplete/weak binaries using stronger evidence.
- Lane B: create new binaries from NNTP evidence.
- Strong NNTP evidence can avoid yEnc recovery.
- Highly obfuscated articles are queued for `recover_yenc`.
- `recover_yenc` probes article BODY, extracts yEnc fields, and proves grouping/order.
- Release stage groups binaries using `name=` and related evidence.

The issue is admission control: a majority of useful uploads require yEnc recovery, so scrape/assemble can produce recovery work much faster than BODY probes can consume it. This creates huge recovery queues, large raw header/article tables, delayed freshness, and disk pressure.

For fully obfuscated uploads, exact grouping requires yEnc BODY evidence. Do not try to infer final grouping from XOVER/HEAD alone. Use XOVER/HEAD only for prioritization, admission, and cheap filtering.

## Goals

1. Keep hot/high-yield groups fresh.
2. Bound recovery queue depth.
3. Bound database growth under limited disk capacity.
4. Avoid global day-bucket head-of-line blocking.
5. Preserve the option to backfill later without storing infinite raw article rows.
6. Make group coverage adaptive instead of scraping all groups equally.
7. Make retention and pruning cheap, preferably by dropping partitions.
8. Add enough metrics to make future tuning data-driven.

## Non-Goals

- Do not attempt exact release assembly without yEnc proof when headers are adversarially randomized.
- Do not keep raw article/header rows indefinitely.
- Do not require all groups from a calendar day to complete before newer hot-group work can continue.
- Do not design around scraping all ~11k groups equally in the live path.

## Core Design Decision

Use per-group watermarks and tiered admission instead of one global daily bucket.

Recommended model:

```text
Hot groups       → frequent scrape, guaranteed recovery budget
Warm groups      → scrape/recover only while backlog is healthy
Cold/long-tail   → sample only; promote if yield is proven
Deferred ranges  → compact low/high article ranges for future backfill
```

Raw staging retention should be short. If recovery cannot process a window before retention/hard-cap limits, compact the remaining work to a deferred range and drop raw rows.

## Terminology

### Hot Group

A group that consistently produces completed releases at acceptable recovery cost.

Suggested criteria, configurable:

- High completed releases per yEnc probe.
- High completed binaries per article scraped.
- Good PreDB match rate after recovery, if PreDB data exists.
- Recovery lag stays within target.
- Manually allowlisted known productive groups may start as hot.

Hot groups get freshness priority and should not be blocked by cold-group backlog.

### Warm Group

A group with some proven value but lower or inconsistent yield.

Warm groups get recovery only when the global queue is below soft limits. They may be skipped or deferred when recovery pressure is high.

### Cold / Long-Tail Group

Unknown, low-yield, or expensive group.

Cold groups should not enqueue full recovery work by default. They should be sampled in small windows. If samples produce completed releases efficiently, promote to warm/hot.

### Deferred Range

A compact record of work intentionally not retained as raw rows:

```text
group_id
article_low
article_high
observed_at / posted_date window
estimated_article_count
estimated_obfuscated_count
reason
priority_score
state
```

A deferred range means: “We saw this range, but recovery capacity/storage did not justify keeping every row. Re-XOVER this range later if capacity allows.”

## Recommended Work Window Policy

Do not use one global “daily bucket must finish” gate. Use rolling per-group windows.

Suggested defaults:

### Hot Groups

- Scrape in small rolling windows: 15–60 minutes of post time or a configured max article count.
- Always prefer latest work over old backfill.
- Target recovery lag: 2–6 hours.
- If recovery queue is above soft cap, hot groups may still admit work, but in smaller windows.
- If above hard cap, admit only work likely to complete existing binaries/releases.

### Warm Groups

- Scrape in larger but bounded windows: 1–2 hours or a configured max article count.
- Only admit recovery work if queue depth is below soft cap.
- If queue is unhealthy, convert the unprocessed window to deferred ranges.
- Target recovery lag: 12–24 hours only when capacity exists.

### Cold / Long-Tail Groups

- Do not fully scrape/recover by default.
- Sample newest ranges only, for example 500–5,000 headers or a small article-number span per run.
- Optionally yEnc-probe a small sample budget.
- Promote only if completed-release yield justifies it.
- Store deferred ranges instead of raw article rows when not selected for full processing.

### Backfill Ranges

- Run only when hot/warm queues are healthy.
- Prefer recent deferred ranges first unless there is a specific reason to backfill older ranges.
- Process in chunks, not entire historical ranges.
- Backfill should be preemptible by fresh hot-group work.

## Recovery Admission Control

Create an admission controller between assemble/scrape and `recover_yenc` queue insertion.

Admission should consider:

```text
current_recovery_queue_depth
observed_recovery_probes_per_hour
estimated_hours_to_drain
group_tier
group_score
article/window age
near-complete binary/release boost
storage pressure
partition retention deadline
```

Suggested queue limits:

```text
target_queue_depth = recovery_probes_per_hour_ewma * target_lag_hours
soft_cap = target_queue_depth
hard_cap = soft_cap * 2 or configured absolute cap
```

Behavior:

- Below soft cap: admit hot and warm work normally.
- Between soft and hard cap: admit hot work, throttle warm, sample cold only.
- Above hard cap: stop admitting new cold/warm recovery work; compact to deferred ranges; admit only hot/near-complete work.

FIFO alone is not acceptable. Use a priority queue or priority fields.

Priority should roughly follow:

```text
near_complete_binary/release
hot group fresh work
high group_score
PreDB time-window boost, if available
warm group fresh work
cold sample
backfill
```

## Group Scoring

Add or audit group-level metrics. Compute rolling scores for 1d, 3d, 7d windows.

Track at minimum:

```text
articles_scraped
headers_staged
recovery_queued
yenc_probes_attempted
yenc_probes_successful
yenc_probes_failed
binaries_created
binaries_completed
releases_created
predb_matches
bytes_staged
bytes_pruned
avg_recovery_lag
max_recovery_lag
probes_per_completed_release
completed_releases_per_10k_articles
```

Example scoring:

```text
value_score = completed_releases / max(1, yenc_probes_attempted)
pressure_score = recovery_queued / max(1, completed_releases)
freshness_score = completed_releases_recent_weighted
predb_score = predb_matches / max(1, releases_created)

group_score = weighted(value_score, freshness_score, predb_score) - weighted(pressure_score)
```

Allow manual overrides:

```text
group_tier_override = hot | warm | cold | disabled | null
```

Automatic tier transitions should be conservative. Avoid flapping by requiring multiple scoring windows before promotion/demotion.

## Retention Policy

Assume raw staging cannot be retained beyond ~48 hours.

Recommended retention levels:

### Durable Data

Keep long-term:

- user-facing release catalog rows
- normalized title and search/category metadata
- inspect/metadata output such as ffprobe/media info, file list summary, size, runtime, codecs, resolution, season/episode/movie identifiers when available
- password status: none, known, unknown, suspected encrypted, failed
- known password if the project intentionally supports storing it
- NZB archive object metadata: object key/path, size, checksum, compression, created_at
- release health flags: complete, catalog_ready, nzb_archived, article_details_purged
- group score history and aggregated metrics

Do not keep completed binary/article/segment detail long-term solely because a release is visible in the catalog. Once the NZB is generated, archived, verified, and the release has enough metadata to hydrate the frontend, article-level grouping evidence becomes disposable implementation detail.

### Short-Lived Staging Data

Retain briefly, partitioned by posted date:

- raw XOVER/header staging
- weak/incomplete article candidates
- recovery queue rows
- failed/unlinked yEnc probes

Suggested defaults:

```text
hot staging retention: up to 48h
warm staging retention: 24–48h depending on disk pressure
cold sample staging retention: 6–24h
deferred raw rows: none; keep only compact range rows
failed/unlinked probes: 24–48h
successful linked probes: retain only if needed, or compact into durable binary/release records
```

If a row is older than its retention window and not linked to a durable release/binary, it should be pruned or compacted.

## NZB Archival and Post-Archive Compaction

Add an explicit archive/compact lifecycle after release creation. This should be separate from raw scrape staging retention.

A release becomes eligible for aggressive purge when all of the following are true:

```text
release is 100% complete
NZB has been generated
NZB has been written to blob/file-system storage
NZB archive checksum/size has been verified
release has sufficient title/category/metadata for the web catalog
password state is acceptable: none, known, or intentionally allowed
```

Blob/file-system storage should be treated as the durable source for serving downloads. Suggested object layout:

```text
/nzb/YYYY/MM/DD/<release_id>.nzb.gz
/nzb/YYYY/MM/DD/<release_id>.json        optional sidecar metadata
```

Store the object key/path in the release catalog. Do not require binary/article tables to serve the NZB after archive verification.

### Post-Archive Purge Targets

For catalog-ready archived releases, purge or compact these as soon as the configured grace period expires:

- binary grouping rows
- binary segment rows
- article-to-binary link rows
- yEnc proof rows that are only needed to assemble the NZB
- temporary weak candidate rows
- release assembly work rows
- completed recovery queue rows
- raw XOVER/HEAD staging rows linked only to this archived release

Keep only compact metrics if useful:

```text
release_id
group_id(s)
article_count
binary_count
probe_count
first_posted_at
last_posted_at
completion_time
archive_object_key
```

Recommended state fields:

```text
nzb_generated_at
nzb_archived_at
nzb_archive_key
nzb_archive_size_bytes
nzb_archive_sha256
catalog_ready_at
article_details_purged_at
purge_state = pending | purged | blocked | failed
purge_block_reason
```

### Complete Release Retention Policy

This policy is separate from raw staging retention. Raw staging may be limited to 24–48h, but completed catalog-ready releases should shed article-level details much sooner.

Suggested defaults:

```text
catalog-ready archived releases: purge article/binary details after 0–6h grace
archived but metadata-incomplete releases: keep details up to 24–48h while inspection retries run
archived but password-unknown/problem releases: mark hidden or degraded; do not let them block raw staging retention indefinitely
failed NZB archive verification: keep required details until retry/deadline, then mark failed and prune by policy
```

The goal is that 100% complete, catalog-ready releases stop consuming high-cardinality database space shortly after NZB archival.

### Manual and Scheduled Maintenance

Pruning unclaimed, non-linked, or orphaned information should be available as a manual maintenance task first. It may later run on a schedule after dry-run output is trusted.

Required maintenance modes:

```text
dry-run: report rows/bytes/partitions eligible for purge
archive-verified purge: compact completed catalog-ready releases
orphan cleanup: remove unlinked binary/article/probe rows
retention prune: drop old staging partitions
range compaction: convert skipped windows to deferred ranges
```

Do not make orphan cleanup blindly delete anything that might still be needed to generate an NZB. The safe boundary is `nzb_archived_at IS NOT NULL` plus verified archive metadata, or expired non-linked staging retention.

## Table Partitioning Plan

Audit current schema first. Prefer PostgreSQL range partitioning by `posted_date` / `posted_at` day for large staging tables.

Candidate partitioned tables:

```text
article_header_stage
article_recovery_queue
weak_binary_candidates
article_yenc_probe_unlinked
assembly_work_items
```

Partition key recommendation:

```text
posted_at date/day from article Date header, normalized to UTC
```

If article Date is missing/untrusted, use scrape time as fallback, but keep both:

```text
posted_at
scraped_at
partition_day
```

Implementation notes:

- Use daily partitions for staging tables.
- Keep indexes local to partitions.
- Avoid massive DELETEs; prune by dropping old partitions.
- Create future partitions ahead of time.
- Make retention job drop partitions older than policy once no durable dependencies remain.
- Ensure all high-volume queries include partition key or bounded time windows.
- Completed release/NZB data should not depend on raw staging partitions remaining.

Suggested partition names:

```text
article_header_stage_2026_06_24
article_recovery_queue_2026_06_24
article_yenc_probe_unlinked_2026_06_24
```

## Deferred Range Table

Add or audit a compact table for skipped/deferred work.

Suggested schema:

```sql
CREATE TABLE deferred_article_ranges (
    id bigserial PRIMARY KEY,
    group_id bigint NOT NULL,
    article_low bigint NOT NULL,
    article_high bigint NOT NULL,
    posted_at_min timestamptz NULL,
    posted_at_max timestamptz NULL,
    observed_at timestamptz NOT NULL DEFAULT now(),
    estimated_article_count bigint NOT NULL,
    estimated_obfuscated_count bigint NULL,
    reason text NOT NULL,
    priority_score numeric NOT NULL DEFAULT 0,
    state text NOT NULL DEFAULT 'pending',
    attempts int NOT NULL DEFAULT 0,
    last_attempt_at timestamptz NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (group_id, article_low, article_high)
);
```

Use this when:

- recovery queue is above cap
- raw partition is nearing retention deadline
- group is cold/low priority
- storage pressure is high
- a scrape window is intentionally skipped

## Scrape Scheduler Policy

Scrape scheduler should select work by value and recovery budget, not just oldest group/day.

Recommended order:

1. Hot groups with recent unread ranges.
2. Hot groups with near-retention staging that can realistically finish.
3. Warm groups while queue is healthy.
4. Cold group sampling.
5. Deferred backfill only when recovery workers are underutilized.

Per group, maintain:

```text
last_scraped_article
last_scraped_at
last_completed_recovery_at
current_tier
score
recovery_lag
```

Do not let one group/day block all later work.

## Recover_yEnc Worker Policy

Audit whether BODY probing downloads more than necessary.

Preferred behavior:

- Fetch BODY only until yEnc header lines are parsed.
- Extract `=ybegin`, `=ypart`, `part`, `total`, `name`, `size`, `begin`, `end`.
- Do not download full article body if not needed.
- If partial reads poison/reuse issues exist, use dedicated short-lived probe connections or safely drain.
- Store probe result idempotently by article ID.

Recovery should prioritize:

1. Existing weak binaries that can become complete.
2. Articles likely to join strong family candidates.
3. Fresh hot-group work.
4. Warm-group work.
5. Cold samples.
6. Backfill.

## Audit Checklist for Codex

Before changing behavior, inspect and summarize:

- Current scrape watermarks and group scheduling.
- Current assemble Lane A / Lane B logic.
- Current `recover_yenc` queue schema and indexes.
- Current retention job and deletion strategy.
- Largest tables and indexes by disk usage.
- Whether staging tables are partitioned already.
- Whether completed releases depend on staging rows.
- Current measured yEnc BODY probes per minute/hour.
- Average and max recovery queue age.
- Group-level distribution of queued recovery work.
- Top groups by recovery backlog.
- Top groups by completed releases.
- Probes per completed release by group.
- Whether BODY probing reads only the yEnc header or full body.

## Implementation Phases

### Phase 1: Metrics and Audit

- Add missing metrics if needed.
- Add admin/debug queries for table size, queue depth, lag, and group yield.
- Produce a short audit note in the repo with current bottlenecks and candidate tables.

### Phase 2: Partitioning, Retention, and NZB Archival

- Partition high-volume staging tables by `partition_day`/`posted_at`.
- Add retention job that drops old partitions.
- Add/verify NZB pre-generation and archive storage for complete releases.
- Store NZB archive object key, size, and checksum on the release catalog row.
- Ensure durable catalog/NZB data survives partition pruning.
- Add config for retention, archive grace, and post-archive purge windows.

### Phase 2B: Post-Archive Purge Worker

- Add a worker or maintenance command that purges article/binary details for catalog-ready archived releases.
- Start with dry-run mode.
- Require archive verification before destructive cleanup.
- Preserve compact aggregate metrics.

### Phase 3: Recovery Admission Controller

- Add configurable soft/hard queue caps.
- Add EWMA recovery throughput measurement.
- Gate recovery queue insertion based on tier, score, lag, and caps.
- Compact skipped work into `deferred_article_ranges`.

### Phase 4: Group Tiers and Scheduler

- Add group score table/materialized view/job.
- Add manual tier overrides.
- Change scrape scheduler to prioritize hot/warm/sample/backfill work.
- Avoid global daily head-of-line blocking.

### Phase 5: Recovery Priority Queue

- Add priority fields to recovery queue.
- Prefer near-complete and hot/fresh work over FIFO.
- Ensure queue ordering uses indexes efficiently.

### Phase 6: Backfill Worker

- Add optional backfill mode for deferred ranges.
- Only run when hot/warm recovery lag is healthy.
- Process in small chunks.
- Re-XOVER deferred ranges rather than storing old raw rows.

## Suggested Config Keys

```yaml
retention:
  raw_stage_hot_hours: 48
  raw_stage_warm_hours: 24
  raw_stage_cold_hours: 12
  failed_probe_hours: 48
  archived_release_detail_grace_hours: 6
  metadata_incomplete_release_hours: 48
  create_partitions_days_ahead: 2

archive:
  nzb_root: /data/nzb-archive
  nzb_compress: true
  verify_checksum_before_purge: true
  purge_article_details_after_archive: true
  purge_dry_run_default: true

recovery:
  target_hot_lag_hours: 4
  target_warm_lag_hours: 24
  soft_queue_hours: 4
  hard_queue_multiplier: 2
  absolute_hard_queue_cap: 250000
  ewma_window_minutes: 30

scrape:
  hot_window_minutes: 30
  warm_window_minutes: 120
  cold_sample_headers: 2000
  max_articles_per_group_window: 50000
  allow_global_daily_gate: false

backfill:
  enabled: true
  max_ranges_per_run: 10
  max_articles_per_range_chunk: 10000
  run_only_below_queue_ratio: 0.25
```

Treat these as defaults to review, not final truth.

## Acceptance Criteria

The redesign is successful when:

- Recovery queue remains bounded under sustained scrape load.
- Hot groups continue to receive fresh headers even when cold/warm work is deferred.
- Raw staging tables can be pruned by dropping daily partitions.
- Database size stabilizes under the configured retention window.
- Unfinished low-priority work is represented by compact deferred ranges, not millions of rows.
- Metrics can show which groups deserve hot/warm/cold status.
- `recover_yenc` backlog age is visible and controlled.
- Completed releases/NZBs remain valid after staging partitions are dropped.
- Catalog-ready archived releases can serve NZB downloads from blob/file-system storage without binary/article tables.
- Article-level details for complete archived releases are purged or compacted after the configured grace period.
- Backfill can be paused/resumed without hurting fresh indexing.

## Final Guidance

Prioritize bounded freshness over theoretical full coverage. For fully obfuscated posts, yEnc BODY proof is the real grouping authority. The application should therefore treat recovery capacity as a scarce resource and spend it where it produces the most completed releases per probe. Once a release is complete, catalog-ready, and its NZB is archived and verified, treat binary/article grouping details as disposable staging data rather than durable catalog data.
