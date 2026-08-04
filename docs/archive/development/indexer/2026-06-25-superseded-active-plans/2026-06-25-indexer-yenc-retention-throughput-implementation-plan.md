# Indexer yEnc Recovery, Retention, and Tiered Work Implementation Plan

This plan supersedes the dated `2026-06-24-*` sprint docs for source work windows, daily buckets, binary workbench, and partition-retention work. Use those docs only as historical context.

## Baseline

Work starts from `dev` on branch `sprint/yenc-retention-throughput-v1`; do not continue from `sprint/daily-bucket-pipeline-v2`.

Current `dev` already has:

- Cursor-based scrape latest/backfill.
- Assemble Lane A/B and yEnc recovery queue seeding.
- yEnc recovery candidate leasing with fairness ordering.
- Release NZB generation/archive and archived-source purge foundations.
- Admin stage, scrape, storage, dashboard, maintenance, release, and binary APIs.

The v2 campaign/source-window/daily-bucket work is intentionally not reused. This sprint adds a bounded recovery admission model and reporting surfaces around the existing dev pipeline.

## Design Defaults

- Recovery soft cap: `recover_yenc` EWMA probes/hour times 4 hours.
- Recovery hard cap: `min(soft_cap * 2, 250000)`.
- Bootstrap recovery throughput before samples exist: 25,000 probes/hour.
- Hot raw retention: 48 hours.
- Warm raw retention: 24 hours.
- Cold/sample retention: 12 hours.
- Failed/unlinked probe retention: 48 hours.
- Archived release source-detail purge grace: 6 hours.
- Metadata-incomplete retention grace: 48 hours.
- Partition maintenance window: today -1 through today +8 UTC days.
- Hot scrape window: 30 minutes or 50,000 articles.
- Warm scrape window: 120 minutes or 50,000 articles.
- Cold sample: 2,000 headers.
- Deferred backfill runs only below 25% of the recovery soft cap.

## Implementation Tasks

1. Add persistent control tables for group tiers, deferred article ranges, recovery capacity snapshots, daily bucket stats, and partition maintenance state.
2. Add runtime settings for retention, recovery admission, scrape tier windows, and deferred backfill.
3. Seed group profiles from configured scrape groups. Explicit configured groups on the primary provider are hot by default; materialized/wildcard groups are warm; discovered/unconfigured groups are cold unless manually overridden.
4. Add a yEnc recovery admission controller used by assemble-triggered and backfill-triggered work-item seeding.
5. Make yEnc work-item upserts idempotent on both `binary_id` and `article_header_id` conflicts.
6. Record deferred article ranges instead of staging unlimited raw work when recovery pressure is above cap.
7. Expose recovery capacity, deferred ranges, group profiles, and daily bucket stats through admin APIs.
8. Add binary workbench detail fields for article headers and yEnc recovery evidence.
9. Keep existing release archive/purge behavior, but gate source-detail purge on verified archive metadata and add retention reporting for partition blockers.
10. Add tests for admission-cap behavior, duplicate yEnc upserts, deferred ranges, and reporting queries.

## Acceptance Criteria

- Fresh DB migration succeeds.
- `go test ./...` passes.
- `npm run build` passes in `ui/`.
- `serve` starts and runs stages without campaign/source-window stalls.
- Scrape and assemble cannot grow `yenc_recovery_work_items` beyond the configured hard cap.
- `recover_yenc` keeps consuming while ready/running work exists.
- Deferred ranges are created when admission is denied by pressure.
- Admin reporting shows real recovery capacity, deferred work, group tiers, daily bucket stats, and binary yEnc evidence.
- At least one live soak run shows scrape, assemble, recover_yenc, release refresh/formation, archive, and purge reporting working together.
