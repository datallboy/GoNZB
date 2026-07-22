# Indexer Stage Flow

## Scrape

Scrape writes source facts and source-owned queues:

- `article_headers`
- `article_header_ingest_payloads`
- `article_header_crosspost_groups`
- `article_header_poster_refs`
- `article_header_assembly_queue`
- `poster_materialization_queue`

Latest scrape feeds the current day. Backfill fills older daily buckets. Scrape
is capped by downstream backlog pressure so source rows do not grow without a
consumer path.

Historical timeframe scrape is an optional third mode. An operator may define
multiple inclusive UTC date windows, including multiple windows for the same
newsgroup. Each entry has a stable ID and independent durable progress in
`indexer_scrape_timeframe_progress`; it does not move latest or backfill
checkpoints. The stage locates date boundaries with bounded XOVER probes,
persists the resulting article-number range, and then consumes that fixed range
in normal scrape batches. Changing an entry's dates resets only that entry.

XOVER may return source dates far outside the current calendar window. Scrape
provisions only the exact observed days it will admit, never a continuous date
horizon. A pass that encounters more than the configured new-day cap admits the
newest work and durably defers the remaining article-number ranges.

Runtime group tiering controls how much work each group can admit:

- hot groups get freshness priority and the largest recovery budget;
- warm groups run while queue depth and recovery lag are healthy;
- cold groups are sampled or deferred and must not starve hot groups.

Hard recovery caps reserve capacity for latest high-value work. Backfill and
low-yield recovery stop at the non-reserved limit; structured latest assembly
may continue while the independently bounded source queue has capacity.

## Assemble

## Article Cohort Schedule

`article_cohort_schedule` is the durable ranking layer between scrape and the
binary/recovery consumers. It reads source facts and binary projections, then
writes scheduler-owned queues:

- `article_cohort_candidates`
- `article_cohort_assembly_queue`
- `article_cohort_yenc_queue`

The scheduler does not mutate `article_headers`, ingest payload rows, or binary
projection ownership. It promotes complete Subject multipart posts directly to
assemble priority work and promotes suspicious opaque singleton bursts to yEnc
priority work. Weak near-time cohorts are scheduling evidence only; they do not
become binary identity proof without HEAD-complete or recovered BODY evidence.

Subject-complete cohort admission is bounded to 1,000 queue rows per scheduler
pass. The configured assembly queue limit remains a capacity limit; the smaller
transaction chunk lets the recurring scheduler drain large partitioned
backlogs without exceeding its statement timeout. Each pass materializes the
eligible article set once and shares it between the cohort-state upsert and
assembly-queue insert. The scheduler leaves join selection to PostgreSQL so
partition-spanning eligibility checks can use parallel hash or merge plans when
they are cheaper than nested loops.

## Assemble

Assemble first claims scheduler-ranked rows from
`article_cohort_assembly_queue`, then falls back to
`article_header_assembly_queue`. It hydrates exact
`(source_posted_at, article_header_id)` source facts, writes binary rows, then
deletes completed source assembly queue rows.

When scheduler-ranked cohort rows are claimable, assemble uses a cohort-only
claim path and does not evaluate broad Lane A/Lane B fallback selectors in the
same claim. This keeps complete Subject multipart cohorts ahead of expensive
general opaque work and avoids consuming the claim timeout before priority work
is locked.

Lane A extends incomplete binaries using `binary_completion_keys`. Lane B
creates or updates general binary records, with scheduler-ranked cohorts ahead
of raw newest unstructured rows.

Binary grouping evidence priority is documented in
[Binary Grouping Evidence](./binary-grouping-evidence.md). In short, complete
NNTP Subject multipart coordinates are stronger grouping evidence than random
poster/message-id context and can be stronger than a randomized recovered yEnc
`name=`.

## yEnc Recovery

yEnc recovery claims `yenc_recovery_work_items`, fetches missing article
payload details, and writes recovered identity data to recovery-owned binary
projection rows. Priority admission first consumes
`article_cohort_yenc_queue`; retry and backoff state stays in the recovery
work table.

Subject-complete posts do not need yEnc recovery for initial binary assembly.
Recovery should be admitted when HEAD evidence is incomplete, ambiguous, or
needs validation, not merely because the Subject token is obfuscated.

Recovery priority should favor near-complete binaries/releases, fresh hot-group
work, high-yield groups, warm fresh work, cold samples, and finally backfill.
Header-time/message-id/article-number cohorts may be used to prioritize probes
only after measured evidence supports the signal; exact release grouping still
requires recovered yEnc or other strong identity evidence.

The current recovery queueing contract is documented in
[yEnc Recovery Queueing](./yenc-recovery-queueing.md).

## Release Refresh And Formation

Release summary refresh aggregates binary projection rows into release-family
readiness summaries and ready candidates. Release formation consumes those
ready candidates and writes release catalog/lineage state.

## Inspect

Inspect stages consume `binary_inspection_ready_queue` and write inspection
history/evidence tables. Inspection results can improve archive, media, text,
and PAR2 visibility without using upstream source tables as progress state.

Ready-queue population is an internal part of inspection candidate selection,
not a separately scheduled supervisor stage. Queued inspection stages perform
bounded, advisory-locked top-ups when they need claimable work; operators only
configure and schedule the inspection consumers themselves.
