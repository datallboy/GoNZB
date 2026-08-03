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

## Recovery Profiles

`indexing.recovery_profile` controls how much of this queue is executable:

- `header_only` uses XOVER/HEAD evidence only. The scheduler does not admit
  opaque yEnc cohorts, `recover_yenc` does not run, and recovery rows do not
  apply scrape backpressure or block outcome retention. Existing rows are
  preserved so a later profile change can resume them.
- `balanced` is the default and recommended unattended mode. It refills and
  claims only `priority_rank = 0` work likely to unlock a multipart binary or
  release. Opaque near-time cohorts start with a 16-article evidence sample.
  Generic priority-1/2 seeding is disabled.
- `exhaustive` enables priority ranks 0-2 and generic bounded seeding. Use it
  for targeted historical work or when maximum recoverable coverage is worth
  the additional BODY traffic. Opaque cohorts start with a 32-article sample.

The `recover_yenc.enabled` stage switch remains an operational circuit breaker.
The stage must be enabled for `balanced` or `exhaustive` to issue BODY requests;
`header_only` overrides an enabled stage and still issues none.

Changing profiles is non-destructive. Lower-priority rows remain in their
current state while in a shallower profile and become eligible again when the
profile is deepened.

## Admission Sources

Work can enter the table from these paths:

- scheduler-backed priority admission from `article_cohort_yenc_queue`;
- generic bounded admission from weak or incomplete binary projections;
- priority admission for near-complete candidates with stable release-family
  evidence;
- priority admission for opaque near-time singleton cohorts;
- bounded sibling expansion after repeated recovered identity evidence proves
  an opaque cohort is worth probing;
- refresh/maintenance paths that resync recovery work for changed binary
  projections.

Assembly commits binary parts and aggregate projections without synchronously
resyncing `yenc_recovery_work_items`. Priority refill and generic bounded
backfill run under `recover_yenc`, so admission latency or a full recovery
backlog cannot roll back or stall upstream assembly.

Scheduler-backed rows are already ranked by the cohort scheduler and may bypass
the generic weak-family filename filters when creating
`yenc_recovery_work_items`. They still must resolve to a main-payload binary
without existing recovered yEnc authority, and they still respect recovery hard
caps and priority-0 overflow caps.

Subject-complete posts are not admitted simply because the visible title is
obfuscated. If HEAD has stable filename, file index/total, article part/total,
and file size, assemble should use that evidence first. yEnc may validate later
but must not override stronger complete Subject coordinates with a randomized
BODY `name=`.

For fully opaque posts where the existing HEAD-derived family is random or
empty, recovered BODY identity becomes the first strong authority. If yEnc
recovery yields a filename but no stable file-set/family key, the recovery write
path derives a fallback family from recovered `name=`, `total=`, and `size=`.
That fallback lets later recovered parts with the same yEnc coordinates merge
into the same binary instead of preserving one random singleton family per
article. This fallback must only be used when no stronger Subject/file-set
family exists.

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

- scheduler-selected opaque cohort samples;
- near-complete candidates with stable release-family evidence;
- suspicious opaque near-time cohorts;
- siblings promoted by repeated recovered evidence.

`priority_rank = 1` is bounded weak/provisional work that may need BODY
identity but has less immediate release pressure.

`priority_rank = 2` is low-value validation or cleanup work.

## Caps

Admission respects runtime recovery capacity:

- the soft cap reduces new generic admission pressure;
- the hard cap blocks normal scrape admission and generic yEnc expansion;
- priority-0 overflow may still admit a bounded amount of work so high-yield
  cohorts do not starve behind a large priority-1 backlog.

Candidate claims also consume a durable UTC-hour BODY budget. The default
limits are:

- Balanced: 25,000 requests/hour;
- Exhaustive: 100,000 requests/hour;
- inspection discovery: 1,000 representative probes/hour.

The selector locks and consumes the active recovery budget in the same
transaction that claims work, so concurrent workers cannot oversubscribe it.
When accepted local-cache or GoNZBNet peer evidence satisfies a claimed row,
the reservation is refunded before processing. Only unresolved rows consume
the BODY allowance.
Changing a limit takes effect on the next claim without clearing queue state.
The **Indexer Work** page reports used and remaining requests for the active
recovery profile.

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
2. in Exhaustive only, fall back to the bounded opaque near-time projection
   scan;
3. in Exhaustive only, run generic bounded seeding when the ready queue is
   empty.

In `exhaustive`, the selector then claims ready rows in two lanes:

- posted-time fairness lane: walks bounded posted-time buckets backward so one
  hot timeframe does not monopolize all probes;
- newest lane: takes the newest ready work after the fairness slice.

With an explicit target window, the window lane replaces the normal fairness
slice and newest work fills the rest of the batch. Without an explicit target,
the runtime split controls fairness versus newest work.

In `balanced`, both the target-window and newest selections are restricted to
priority 0. Lower-priority rows cannot fill unused batch capacity.

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
`article_cohort_candidates`. `recovery_decision` is durable and has three
states: `sample`, `promoted`, and `no_yield`.

- Successful yEnc recovery marks matching `article_cohort_yenc_queue` rows
  `done`, increments `yenc_done_count` and `yenc_recovered_count`, and records
  the recovered stable identity signal and any actual binary merge.
- `not_found` and no-op outcomes keep the normal yEnc work-item retry/backoff
  behavior, but increment `yenc_no_identity_count`.
- Two matching stable recovered signals promote a cohort in both recovery
  profiles. Exhaustive may also promote after two real grouping gains.
- Promotion expands at most 256 additional rows per scheduler pass, up to
  2,048 probes per Balanced cohort or 20,000 per Exhaustive cohort.
- A sample that reaches 16/32 completed probes without promotion becomes
  `no_yield`, whether the probes found no yEnc identity or found mutually
  random names. The scheduler stops enqueueing that cohort.
- If later ingestion increases the cohort's article count, `no_yield` reopens
  to `sample`; it is never a permanent judgment about future source data.

This feedback loop keeps productive opaque cohorts hot while stopping random or
low-yield cohorts from repeatedly filling priority-0 capacity.

When GoNZBNet evidence consumption is active, per-batch recovery order is:

1. accepted local raw-evidence cache;
2. up to the configured number of authorized pool peers;
3. local matcher/application of accepted evidence;
4. NNTP BODY for unresolved Message-IDs only.

Peer miss, timeout, conflict, disabled exchange, or ambiguous evidence does not
fail the stage and falls through to normal BODY behavior.

Opaque projection discovery reads one bounded page at a time. Its durable
window cursor advances by `(posted_at, binary_id)` and wraps only after reaching
the bottom of the selected source-day or explicit target window. This prevents
a large newest slice from being rescanned forever while older candidates in
the same window are never evaluated.
