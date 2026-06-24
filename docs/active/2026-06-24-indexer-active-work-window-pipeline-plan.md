# Active Work Window Pipeline Plan

## Summary

Change the indexer from "scrape a huge backlog and let downstream stages churn"
to a bounded, moving source work-window model. The active window is a short
posted-time slice, defaulting to `15 minutes`, that scrape, assemble, yEnc
recovery, discovery-style inspect, release summary, and release formation work
through together.

This plan is the first sprint and must be implemented before source/work
partition retention. Active windows define which source/work rows are active,
complete, abandoned, or purge-eligible. Partition retention only reclaims rows
after this lifecycle says it is safe.

## Current Behavior To Replace

- Source window settings exist today:
  - `window_minutes = 15`
  - `backfill_window_days = 7`
  - `max_open_headers = 50000`
  - `resume_open_headers = 10000`
  - `max_blocking_yenc = 50000`
  - `resume_blocking_yenc = 10000`
- Scheduled scrape pauses when assemble backlog is high; `scrape_latest` gets a
  small trickle every `5 minutes` under assemble pressure.
- Scheduled scrape pauses fully when blocking yEnc backlog is high.
- Backfill is currently capped by `now - backfill_window_days`.
- Latest scrape is not date-capped today. After a long shutdown it resumes from
  `latest_checkpoint + 1`, which can scrape a stale multi-week gap unless
  backlog gates stop it.
- No purge happens automatically because rows are older than the window.

## Core Data Model

Add unpartitioned durable control tables. These tables are not source/work
payload and must not be partitioned in the retention sprint.

### `source_work_campaigns`

One row represents an intentional source-work campaign.

Required columns:

- `id bigserial primary key`
- `campaign_key text not null unique`
- `mode text not null`
  - allowed: `latest`, `backfill`, `manual_range`, `missed_latest_gap`
- `status text not null`
  - allowed: `pending`, `active`, `paused`, `complete`, `abandoned`, `failed`
- `provider_id bigint not null`
- `newsgroup_id bigint not null`
- `source_posted_at_start timestamptz`
- `source_posted_at_end timestamptz`
- `direction text not null default 'newest_to_oldest'`
  - allowed: `newest_to_oldest`, `oldest_to_newest`
- `priority integer not null default 100`
- `created_by text not null default 'system'`
- `pause_reason text not null default ''`
- `abandon_reason text not null default ''`
- `failure_reason text not null default ''`
- `windows_total integer not null default 0`
- `windows_complete integer not null default 0`
- `windows_abandoned integer not null default 0`
- `articles_scraped bigint not null default 0`
- `binaries_assembled bigint not null default 0`
- `release_candidates_seen bigint not null default 0`
- `created_at timestamptz not null default now()`
- `updated_at timestamptz not null default now()`
- `completed_at timestamptz`

Indexes:

- unique `campaign_key`
- `(status, priority, updated_at)`
- `(provider_id, newsgroup_id, mode, status)`
- `(provider_id, newsgroup_id, source_posted_at_start, source_posted_at_end)`

### `source_work_windows`

One row represents one bounded posted-time slice for a provider/newsgroup.

Required columns:

- `id bigserial primary key`
- `campaign_id bigint not null references source_work_campaigns(id)`
- `provider_id bigint not null`
- `newsgroup_id bigint not null`
- `mode text not null`
  - copied from campaign
- `status text not null`
  - allowed: `pending`, `discovering`, `scraping`, `draining`, `paused`,
    `complete`, `abandoned`, `failed`
- `source_posted_at_start timestamptz not null`
- `source_posted_at_end timestamptz not null`
- `overlap_start timestamptz not null`
- `overlap_end timestamptz not null`
- `article_number_start bigint`
- `article_number_end bigint`
- `scrape_cursor_article_number bigint`
- `discovery_confidence text not null default 'unknown'`
  - allowed: `unknown`, `exact`, `bounded`, `low`
- `drain_started_at timestamptz`
- `completed_at timestamptz`
- `paused_reason text not null default ''`
- `abandoned_reason text not null default ''`
- `failed_reason text not null default ''`
- `last_blocker text not null default ''`
- `scraped_headers bigint not null default 0`
- `assembled_binaries bigint not null default 0`
- `open_assemble_headers bigint not null default 0`
- `blocking_yenc_items bigint not null default 0`
- `release_candidates bigint not null default 0`
- `running_claims bigint not null default 0`
- `created_at timestamptz not null default now()`
- `updated_at timestamptz not null default now()`

Indexes:

- `(status, provider_id, newsgroup_id, source_posted_at_start)`
- `(campaign_id, source_posted_at_start)`
- `(provider_id, newsgroup_id, mode, status, source_posted_at_start)`
- `(provider_id, newsgroup_id, source_posted_at_start, source_posted_at_end)`
- exclude or unique constraint preventing overlapping non-terminal windows for
  the same provider/newsgroup/mode/campaign unless overlap is intentional.

### Source/Work Row Trace Columns

Add to source/work tables as part of the active-window sprint:

- `source_work_window_id bigint`
- `source_posted_at timestamptz`

Rules:

- `source_posted_at` is the canonical pipeline/partition timestamp.
- `source_work_window_id` is for lifecycle tracing and exact window counts.
- Stage queries must use `source_posted_at` and active-window bounds, not only
  `source_work_window_id`, because release families can span overlap.
- These columns are nullable only during migration. New writes must populate
  them.

## Status Transitions

Campaign transitions:

- `pending -> active`
- `active -> paused`
- `paused -> active`
- `active -> complete`
- `active -> abandoned`
- `active -> failed`
- `paused -> abandoned`
- `failed -> abandoned`

Window transitions:

- `pending -> discovering`
- `discovering -> scraping`
- `scraping -> draining`
- `draining -> complete`
- `discovering|scraping|draining -> paused`
- `paused -> discovering|scraping|draining`
- `pending|discovering|scraping|draining|paused|failed -> abandoned`
- `discovering|scraping|draining -> failed`

Terminal statuses:

- Campaign: `complete`, `abandoned`.
- Window: `complete`, `abandoned`.
- `failed` is not terminal for retention. It must be retried or abandoned.

Window completion criteria:

- scrape has consumed the discovered article-number bounds;
- no unassembled article-header backlog remains above
  `resume_open_headers` for the window;
- no blocking yEnc backlog remains above `resume_blocking_yenc` for the window;
- release summary refresh and release formation have attempted the window;
- no running stage claims remain for assemble, yEnc, discovery inspect, release
  summary, or release formation within the window;
- media/archive/password inspect do not block source-window completion.

Abandon behavior:

- Abandon is explicit. It means the admin/system accepts losing future releases
  from that old source/work window.
- Abandoned windows are retention-eligible after running claims clear and
  durable release/archive/catalog guards pass.

## Default Runtime Settings

Add/keep settings under `indexing.source_window`:

- `enabled = true`
- `window_minutes = 15`
- `overlap_minutes = 15`
- `max_open_headers = 50000`
- `resume_open_headers = 10000`
- `max_blocking_yenc = 50000`
- `resume_blocking_yenc = 10000`
- `latest_stale_gap_minutes = 60`
- `max_active_windows_per_group = 1`
- `backfill_campaign_default_days = 7`
- `auto_create_missed_latest_campaign = true`
- `auto_resume_historical_campaigns = false`

Interpretation:

- `window_minutes` is the active source-work slice size.
- `overlap_minutes` protects release families crossing a boundary.
- `backfill_campaign_default_days` replaces the old
  `backfill_window_days` control as the default range when an admin creates a
  backfill campaign without an explicit start time.
- Historical campaigns do not run automatically unless explicitly resumed.

## Scrape Behavior

### Latest Mode

`scrape_latest` owns current/head work.

Algorithm:

1. Load provider/newsgroup stats.
2. Load the current active `latest` window for the provider/newsgroup.
3. If an active incomplete latest window exists, resume it.
4. If no active latest window exists, discover the newest/head posted-time
   window from XOVER near group high water.
5. If the latest checkpoint is older than `latest_stale_gap_minutes` relative
   to the discovered head window, create a paused `missed_latest_gap` campaign
   covering the old checkpoint-to-head gap.
6. Do not automatically scrape the stale gap.
7. Create a fresh current/head `latest` window and scrape only that window's
   article-number bounds.

Cold start:

- Latest starts near group head, discovers the head posted-time window, and
  processes that window.
- It does not walk from provider low water.

Long shutdown:

- Existing incomplete latest window resumes first if present.
- Otherwise latest starts at current head.
- Missed history becomes a paused campaign for admin review.

### Backfill And Manual Range

Backfill is campaign-driven.

Admin/API inputs:

- provider/newsgroup;
- `source_posted_at_start`;
- `source_posted_at_end`;
- direction, default `newest_to_oldest`;
- optional priority.

Behavior:

- Split the campaign range into `window_minutes` windows with
  `overlap_minutes` overlap for fetch/match.
- Only one active backfill/manual window per provider/newsgroup runs by default.
- Campaign windows are processed newest-to-oldest unless configured otherwise.
- Paused campaigns do not run from scheduled scrape.
- Manual trigger can run one campaign window even when scheduled scrape is
  paused, but still respects storage guard and explicit campaign status.

## Article-Number Mapping

Mapping goal: convert a posted-time window into article-number bounds without
scanning giant ranges.

Required approach:

- Use XOVER probes near the provider/group high-water or campaign cursor.
- Binary-search article numbers to find approximate lower/upper bounds for the
  target posted-time window.
- Fetch bounded XOVER ranges and filter every header by `source_posted_at`
  after retrieval.
- Store discovered `article_number_start`, `article_number_end`, and
  `discovery_confidence`.
- If mapping exceeds configured probe/range limits, mark the window `failed`
  with reason `mapping_low_confidence` or `mapping_range_too_large`.

Guardrails:

- Never fall back to scanning millions of article numbers for one window.
- Never mark a window complete until scrape has either consumed bounded ranges
  or failed/abandoned explicitly.
- Date disorder is expected; filtering by `source_posted_at` after XOVER is
  mandatory.

## Stage Query Rules

Default rule:

- Stages that create or refine source/release candidates use active windows by
  default.
- Explicit historical campaign mode is the only path that processes old
  non-active windows.

Window-aware stages:

- `scrape_latest`
- `scrape_backfill`
- `assemble`
- `recover_yenc`
- `inspect_discovery`
- `inspect_par2_ready_refresh`
- `release_summary_refresh`
- `release`

Downstream stages not blocked by source-window age after release formation:

- `inspect_archive`
- `inspect_media`
- `inspect_password`
- `release_generate_nzb`
- `release_archive_nzb`
- `release_source_purge`
- enrichment stages

Query-shape rules:

- Every window-aware stage must select candidates from the smallest bounded
  dataset first: provider/newsgroup, active window `source_posted_at` range,
  status/readiness predicate, then stable claim/order columns.
- Do not add a source-window predicate by wrapping an existing candidate query
  in a broad outer filter. The date/window predicate must be inside the first
  relation that narrows the high-volume table.
- Do not replace existing ready/status/claim predicates with only
  `source_posted_at` bounds. Window filters are additive; they must preserve the
  prior readiness semantics.
- Candidate selection must use keyset-style ordering where the existing stage
  already depends on it. Avoid `OFFSET` over source/work tables.
- Any query that can touch article headers, assembly queue, binary work,
  yEnc work, inspect ready queues, or release candidate queues must have a
  matching index path documented in the implementation commit.
- Any changed candidate query must include before/after `EXPLAIN (ANALYZE,
  BUFFERS)` evidence against representative data or a local fixture with enough
  rows to prove index use. The accepted plan must start from
  `source_posted_at`/partition pruning plus the stage's status/claim key, not a
  full heap scan followed by filtering.
- If the planner chooses a sequential scan for a high-volume stage table, stop
  and fix the index/query shape before continuing. Do not ship the active-window
  behavior relying on small development-table scans.

Assemble:

- Claims rows from `article_header_assembly_queue` joined to active windows.
- Uses `source_posted_at >= overlap_start` and
  `source_posted_at < overlap_end`.
- Does not claim old rows outside active/historical windows by default.
- Candidate order must continue to favor rows most likely to complete binaries
  and must preserve existing claim semantics. The new window predicate should be
  satisfied by an index such as
  `(source_posted_at, status, claim_until, article_header_id)` or the closest
  existing equivalent for the implemented schema.

yEnc recovery:

- Claims `yenc_recovery_work_items` in active windows.
- Blocking yEnc backlog is counted per active window first; global counts are
  diagnostic only.
- Candidate order must keep existing priority/status behavior, with
  `source_posted_at` added for pruning rather than replacing priority ranking.

Release summary and release formation:

- Candidate refresh uses source-window overlap bounds, not strict window IDs.
- Release families may include binaries from adjacent windows when
  `source_posted_at` falls inside overlap.
- A window is not complete until release summary and release formation have
  attempted the window.
- Query predicates must continue to use the same family identity, provider,
  newsgroup, completion, and readiness evidence currently required for release
  formation. The active window only limits which source candidates are searched;
  it must not weaken family matching.

Inspect:

- Discovery inspect should be window-aware because it feeds release formation.
- Archive/media/password inspect are release-level enrichment and should
  continue after source windows complete.
- Inspect ready-refresh candidate selection must keep existing ready/running
  and retry predicates. Window filters apply only to discovery/PAR2 readiness
  paths that still feed release formation.

## Index And Performance Regression Guardrails

This sprint changes candidate selection for hot stages, so performance
validation is part of the implementation, not a follow-up.

Implementation requirements:

- Inventory the current candidate queries and indexes for scrape, assemble,
  yEnc recovery, inspect discovery/PAR2 ready refresh, release summary refresh,
  and release formation before changing predicates.
- For each stage, document:
  - old predicate/order shape;
  - new predicate/order shape;
  - exact index expected to support it;
  - whether the index already exists or is added by the sprint.
- Add or adjust indexes in the same commit that changes the query shape.
- Do not remove runtime-speed indexes for storage reasons as part of this
  sprint.
- Preserve existing stage ordering where it affects correctness, fairness, or
  throughput. Adding `source_posted_at` must not reorder work in a way that
  starves retries, high-priority yEnc work, or nearly complete release families.
- Backlog/count queries shown in admin pages must use bounded windows or
  pre-aggregated summaries. Do not add dashboard queries that count across the
  entire source/work history on every page load.
- Large reports may be explicit admin actions with a loading state; ordinary
  supervisor ticks and maintenance-page refreshes must stay bounded.

Acceptance checks:

- Capture representative `EXPLAIN (ANALYZE, BUFFERS)` plans for each changed
  hot query.
- Plans for partitioned or date-windowed tables show index scans, bitmap index
  scans, or partition pruning before large row filtering.
- Stage candidate queries have tests asserting the active-window predicate is
  present and that status/claim/readiness predicates are still present.
- Add regression tests for at least one old-row outside-window candidate and
  one valid inside-window candidate per changed stage.

## Advancement Rules

A provider/newsgroup/mode advances to the next window when:

- current window status is `complete` or `abandoned`;
- active window count for that provider/newsgroup/mode is below
  `max_active_windows_per_group`;
- storage and memory guards allow new scrape work;
- downstream backlog for the current window is below resume thresholds.

Do not advance when:

- current window is `discovering`, `scraping`, `draining`, `paused`, or
  `failed`;
- assemble or yEnc backlog exceeds high-water threshold;
- mapping failed and the window has not been abandoned;
- retention has removed required source/work rows unexpectedly.

## Admin/API Operations

Add admin APIs and UI controls for:

- list campaigns;
- create backfill/manual campaign by provider/newsgroup and posted-time range;
- pause/resume/abandon campaign;
- list source work windows;
- pause/resume/abandon window;
- show skipped latest gaps as paused campaigns;
- show current blockers per window:
  - open assemble headers;
  - blocking yEnc items;
  - running claims;
  - release candidates;
  - last activity;
  - failure reason.

Admin UI pages:

- Add source-work campaign/window panel to the existing admin scrape or
  maintenance area.
- Do not hide abandoned/failed windows; they are critical retention context.

## Partition Retention Interaction

Partitioning uses `source_posted_at` and daily partitions. Active work windows
are smaller than daily partitions, so a daily partition can contain many
windows.

Retention drop rules:

- Do not drop a day partition with any non-terminal source work window.
- Non-terminal means `pending`, `discovering`, `scraping`, `draining`,
  `paused`, or `failed`.
- Only `complete` and `abandoned` windows can be retention-eligible.
- Do not drop a partition needed by running assemble/yEnc/inspect claims.
- Do not drop durable release/archive/catalog data.
- Dropping old source/work partitions is optional and independent from normal
  active-window advancement.

## Migration And Compatibility

This sprint may add new non-partitioned control tables and nullable trace
columns before partitioning exists.

Compatibility rules:

- Existing rows without `source_work_window_id` remain processable only through
  compatibility paths until the active-window migration is complete.
- New source/work writes must populate `source_posted_at`.
- New source/work writes should populate `source_work_window_id` when they come
  from a managed window.
- Existing `scrape_checkpoints.backfill_until_date` should be treated as legacy
  once campaigns exist.

## Test Plan

- Latest mode after a long shutdown creates a fresh current/head window instead
  of scraping the stale gap.
- Skipped latest gap is recorded as a paused `missed_latest_gap` campaign.
- Existing incomplete window resumes after restart.
- Backfill campaign processes a historical posted-time range as multiple
  `15 minute` windows.
- Backfill campaign pauses/resumes/abandons correctly.
- Article-number mapping refuses huge low-confidence scans.
- Assemble/yEnc/release formation only select active-window rows by default.
- Release formation handles uploads crossing a window boundary via overlap.
- Media/archive/password inspect continue working after release formation
  without source-window age restrictions.
- Retention dry-run refuses to drop partitions containing non-terminal windows.
- `EXPLAIN` for scrape/assemble/yEnc/release candidate selection uses
  `source_posted_at` indexes or partition pruning.
- Run `go test ./...`, `npm run build`, and `git diff --check` before
  completing the sprint.
