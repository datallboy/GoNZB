# yEnc Recovery Queueing

This page documents the current `recover_yenc` work queue behavior. The
grouping policy lives in [Binary Grouping Evidence](./binary-grouping-evidence.md).

## Work Table

`recover_yenc` consumes `yenc_recovery_work_items`.

The work item identity is partition-aware:

- primary key: `(source_posted_at, binary_id)`;
- article uniqueness: `(source_posted_at, article_header_id)`;
- lifecycle state: `ready`, `running`, `done`, or `stale`;
- priority state: `priority_rank`, `admission_reason`, `admission_score`,
  `group_tier`, `ready_at`, and lease fields.

Recovery must keep retry and progress state in this table. It must not write
retry state into `article_headers` or use upstream source rows as progress
markers.

## Admission Sources

Work can enter the table from these paths:

- scheduler-backed priority admission from `article_cohort_yenc_queue`;
- generic bounded admission from weak or incomplete binary projections;
- priority admission for near-complete or multipart candidates;
- priority admission for opaque near-time singleton cohorts;
- sibling admission after a successful yEnc recovery proves a nearby opaque
  cohort is worth probing;
- refresh/maintenance paths that resync recovery work for changed binary
  projections.

Subject-complete posts are not admitted simply because the visible title is
obfuscated. If HEAD has stable filename, file index/total, article part/total,
and file size, assemble should use that evidence first. yEnc may validate later
but must not override stronger complete Subject coordinates with a randomized
BODY `name=`.

## Eligibility

Generic yEnc admission is for main-payload binary projections where HEAD
evidence is incomplete, weak, provisional, or ambiguous.

Typical eligible shapes include:

- `contextual_obfuscated`, `numeric_obfuscated_set`, or `opaque_set` families;
- no recovered yEnc authority yet;
- missing or weak release-family identity;
- incomplete multipart evidence or near-complete release pressure;
- suspicious long random names such as `.bin`, `.dat`, `.tmp`, `.bak`, or
  generated placeholders;
- opaque one-part singleton bursts that may be split articles of a larger
  binary.

## Priority Ranks

`priority_rank = 0` is work likely to unlock binary grouping or release
formation:

- near-complete or multipart candidates;
- indexed multi-file candidates;
- suspicious opaque near-time cohorts;
- siblings of a successful opaque yEnc recovery.

`priority_rank = 1` is bounded weak/provisional work that may need BODY
identity but has less immediate release pressure.

`priority_rank = 2` is low-value validation or cleanup work.

## Caps

Admission respects runtime recovery capacity:

- the soft cap reduces new generic admission pressure;
- the hard cap blocks normal scrape admission and generic yEnc expansion;
- priority-0 overflow may still admit a bounded amount of work so high-yield
  cohorts do not starve behind a large priority-1 backlog.

Scrape gating is a storage guard. Recovery backlog should stay bounded, but a
full priority-1 backlog must not prevent priority-0 opaque bursts from being
sampled.

## Selection

Candidate selection is not FIFO.

Before selecting rows, stale ready items are retired and expired running leases
are returned to the pool. If priority-0 ready rows are below the configured
reservoir target, the selector tries to refill priority-0 work. The default
reservoir is five recovery batches:
`indexing.recovery_admission.priority0_reservoir_batches = 5`.

Refill order is:

1. consume scheduler materialized rows from `article_cohort_yenc_queue`;
2. fall back to the bounded opaque near-time projection scan;
3. run generic bounded seeding only when the ready queue is empty.

The selector then claims ready rows in two lanes:

- posted-time fairness lane: walks bounded posted-time buckets backward so one
  hot timeframe does not monopolize all probes;
- newest lane: takes the newest ready work after the fairness slice.

With an explicit target window, the window lane replaces the normal fairness
slice and newest work fills the rest of the batch. Without an explicit target,
the runtime split controls fairness versus newest work.

Inside each claim window, rows are ordered by:

1. `priority_rank`;
2. posted minute descending;
3. poster suffix hint;
4. message-id suffix hint;
5. per-hint group rank;
6. `date_utc` descending;
7. `article_number`;
8. `binary_id`.

The poster/message-id/minute ordering is a batch locality hint. It is not
binary grouping proof.

## Cohort Outcome Feedback

Scheduler-backed priority work records recovery yield back into
`article_cohort_candidates`.

- Successful yEnc recovery marks matching `article_cohort_yenc_queue` rows
  `done`, increments `yenc_done_count` and `yenc_recovered_count`, clears any
  cooldown, and raises the cohort score.
- `not_found` and no-op outcomes keep the normal yEnc work-item retry/backoff
  behavior, but increment `yenc_no_identity_count`.
- A zero-recovered cohort that reaches the no-identity threshold is moved to
  cooldown. While the cooldown is active, the scheduler does not enqueue more
  rows for that cohort.

This feedback loop keeps productive opaque cohorts hot while stopping random or
low-yield cohorts from repeatedly filling priority-0 capacity.

## Current Audit Notes

On 2026-07-01, ready claim selection was fast: selecting 10,000 ready rows from
`yenc_recovery_work_items` took about 33 ms on the live database.

Admission/refill is the heavier path. A read-only EXPLAIN of opaque near-time
admission over a 50,000-row recent scan took about 2.85 seconds and performed
many partitioned point lookups into binary projection tables. That cost is
acceptable as a bounded refill path, but it should not run inside assemble's
hot refresh path for every large assemble batch.

Live recovery was productive in the sampled run: a 5,000-item batch recovered
5,000 headers and merged 4,950 of them at concurrency 100. Later batches with
zero merges are useful signal that selection/admission metrics should include
`priority_rank`, `admission_reason`, and lane-level merge yield.

## Follow-Up Targets

- Add selection metrics by `priority_rank`, `admission_reason`, `group_tier`,
  and lane.
- Add merge-yield metrics by admission reason.
- Keep priority-0 refill independent of generic ready backlog.
- Move or defer expensive yEnc admission sync out of assemble's binary stats
  refresh when recovery is already over hard cap.
