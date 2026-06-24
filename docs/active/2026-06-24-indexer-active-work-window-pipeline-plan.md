# Active Work Window Pipeline Plan

## Summary

Change the indexer from "scrape a huge backlog and let downstream stages churn"
to a bounded, moving source work-window model. The active window is a short
posted-time slice, defaulting to 15 minutes, that scrape, assemble, yEnc
recovery, discovery-style inspect, release summary, and release formation work
through together. This plan prevents the system from creating tens of millions
of old source/work rows that are unlikely to form releases soon.

This is separate from partition retention. Active work windows control what
enters and remains active in the pipeline. Partition retention controls physical
cleanup after work windows are complete or explicitly abandoned.

## Current Behavior To Replace

- Source window defaults exist today:
  - `window_minutes = 15`
  - `backfill_window_days = 7`
  - `max_open_headers = 50000`
  - `resume_open_headers = 10000`
  - `max_blocking_yenc = 50000`
  - `resume_blocking_yenc = 10000`
- Scheduled scrape pauses when assemble backlog is high; `scrape_latest` gets a
  small trickle every 5 minutes under assemble pressure.
- Scheduled scrape pauses fully when blocking yEnc backlog is high.
- Backfill is currently capped by `now - backfill_window_days`.
- Latest scrape is not date-capped today. After a long shutdown it resumes from
  `latest_checkpoint + 1`, which can scrape a stale multi-week gap unless
  backlog gates stop it.
- No purge happens automatically because rows are older than the window.

## Target Model

Introduce `source_work_windows` per provider/newsgroup.

Each work window stores:

- provider ID and newsgroup ID;
- mode: `latest`, `backfill`, `manual_range`;
- `source_posted_at_start`;
- `source_posted_at_end`;
- optional overlap/grace bounds;
- discovered article-number start/end once mapped;
- status: `discovering`, `scraping`, `draining`, `complete`, `abandoned`;
- counters for scraped headers, assembled binaries, yEnc backlog, release
  candidates, and last activity.

Default behavior:

- Active work-window duration: 15 minutes.
- Boundary overlap/grace: 15 minutes, configurable.
- Keep one active window per provider/newsgroup/mode unless an admin explicitly
  starts a campaign.
- Open the next window only when current-window downstream backlog is below
  configured thresholds.
- Do not use `now - backfill_window_days` as the main pipeline saturation
  control. Replace it with moving windows and explicit campaign horizons.

## Stage Behavior

Scrape:

- `scrape_latest` creates or resumes a current/head work window.
- It scrapes only article ranges whose posted time falls in the active window.
- If the server was off for two weeks, `scrape_latest` should not automatically
  scrape the entire missed gap. It should create a fresh current/head window.
- Any missed historical gap becomes an explicit backfill campaign.

Backfill:

- Admin creates a backfill campaign with provider/newsgroup and posted-time
  range.
- The system maps posted-time windows to article-number ranges using XOVER
  probes/binary search.
- Backfill processes the campaign as a sequence of 15-minute work windows.
- Campaigns can be paused, resumed, completed, or abandoned.

Assemble:

- Claims only article headers in active `source_work_windows` unless running in
  explicit historical mode.
- Uses `source_posted_at` predicates to stay in the current window and support
  partition pruning.
- Window is not advanced until assemble backlog for that window drops below the
  resume threshold.

yEnc recovery:

- Claims only yEnc work items for active windows unless running explicit
  historical mode.
- The yEnc backlog threshold is window-scoped, not global-only.
- Window is not advanced while blocking yEnc backlog for that window exceeds
  threshold.

Release summary and release formation:

- Prefer current active windows and require candidates to be backed by the
  same source window or overlap grace.
- Release families may span the boundary overlap. The overlap exists because
  real uploads can straddle window boundaries.

Inspect:

- Discovery-style inspect can be source-window aware.
- Media/archive/password inspect are downstream release enrichment and should
  not be blocked by source posted-time windows once a release is formed.

## Restart And Purge Behavior

If the server stops during an active small window:

- On restart, resume and finish that active window even if it is older than the
  normal retention horizon.
- Because the active window is small, this should not recreate the large backlog
  problem.
- Retention must not purge partitions containing active or incomplete windows.

If the server was off for days or weeks:

- Do not automatically scrape the whole missed gap.
- `scrape_latest` should start at the current/head window.
- Historical missed time is handled by an explicit backfill campaign.

If old windows remain:

- Complete windows become eligible for partition retention.
- Abandoned windows become eligible for partition retention.
- Incomplete windows are retained until finished or explicitly abandoned.

## Partition Retention Interaction

Partitioning uses `source_posted_at` and daily partitions. Active work windows
are smaller than daily partitions, so a daily partition can contain many
windows.

Retention drop rules:

- Do not drop a day partition with any active/incomplete source work window.
- Do not drop a partition needed by running assemble/yEnc/inspect claims.
- Do not drop durable release/archive/catalog data.
- Dropping old source/work partitions is optional and independent from normal
  active-window advancement.

This means partitioning becomes a safe physical cleanup mechanism, not the
thing that decides what the pipeline should process.

## Admin Controls

Add UI/API controls for:

- active source work windows by provider/newsgroup;
- current window status and backlog;
- pause/resume/abandon a window;
- create manual backfill campaign by posted-time range;
- campaign progress by window;
- retention eligibility by daily partition;
- warning when latest checkpoint is stale but latest mode will skip to head.

## Test Plan

- Latest mode after a long shutdown creates a fresh current/head window instead
  of scraping the entire stale gap.
- Existing incomplete active window resumes after restart.
- Backfill campaign processes a historical posted-time range as multiple
  15-minute windows.
- Assemble/yEnc/release formation only select active-window rows by default.
- Release formation handles uploads crossing a window boundary via overlap.
- Media/archive inspect continue working after release formation without being
  blocked by source-window age.
- Retention dry-run refuses to drop partitions containing active/incomplete
  windows.
- `EXPLAIN` for scrape/assemble/yEnc candidate selection uses
  `source_posted_at` indexes/partition pruning.
- Run `go test ./...`, `npm run build`, and `git diff --check` before
  completing the sprint.

## Open Design Work For Plan Mode

This document intentionally leaves detailed implementation decisions for the
next planning session:

- exact `source_work_windows` schema;
- exact article-number-to-posted-time mapping algorithm;
- exact advancement thresholds;
- whether latest mode should record skipped stale gaps as campaigns;
- campaign API/UI wire shapes;
- detailed release-family boundary overlap rules.

