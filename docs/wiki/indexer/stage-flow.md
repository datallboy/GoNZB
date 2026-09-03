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
corrects both boundaries across a bounded article-number horizon for
non-monotonic Date headers, persists the resulting article-number range, and
then consumes that fixed range in normal scrape batches. Every fetched row is
still filtered against the exact requested UTC window. Changing an entry's
dates resets only that entry.

Configured timeframe concurrency applies across independent entries. Article
numbers remain provider-local: the provider that supplies `GROUP` bounds and
date-boundary probes is pinned for every XOVER request in that entry. Capacity
failover must not continue the same numeric range on another provider; a
different provider requires its own boundary resolution and durable progress.
Optional start/end times narrow a window within those UTC dates. A date-only
end remains inclusive through the end of that UTC day; an end time is the exact
exclusive boundary.

XOVER may return source dates far outside the current calendar window. Scrape
provisions only the exact observed days it will admit, never a continuous date
horizon. A pass that encounters more than the configured new-day cap admits the
newest work and durably defers the remaining article-number ranges.

`scrape_deferred` is the bounded consumer for those durable ranges. It claims
one range with a lease, fetches at most its configured batch size, and writes a
smaller continuation range before completing the claim. A source-day cap may
also create smaller child ranges. Fetch failures retry with bounded attempts;
exhausted failures become operator-visible `abandoned` work instead of moving a
scrape checkpoint again. Keep this stage enabled whenever any scrape producer
is enabled.

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

Subject-complete cohort admission is bounded by the runtime-configurable
`subject_queue_batch_size` (default 1,000) per scheduler pass. The configured
assembly queue limit remains a capacity limit; the smaller transaction chunk
lets the recurring scheduler drain large partitioned backlogs without exceeding
its statement timeout. Each pass materializes the
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

After scheduler-ranked work is exhausted, the combined claim path takes a
bounded newest-first slice from `article_header_assembly_queue`. It classifies
each selected row against `binary_completion_keys`: matching rows are Lane A
extensions of incomplete binaries, while the remaining rows are Lane B general
binary work. Candidate discovery is deliberately bounded by the batch size so
a large queue cannot force the claim transaction to scan the entire backlog.

An explicitly enabled RFC3339 assemble target window temporarily restricts
cohort scheduling, assembly claims, and cross-newsgroup multipart regroup scans
to that `source_posted_at` range. Target-window cohort capacity is counted
inside the window, so an unrelated global cohort backlog cannot prevent
structured historical work from being prioritized. Target assembly consumes
those cohort rows before its bounded fallback scan. This lets a completed
historical timeframe scrape reach assembly and regrouping without first
draining an unrelated live backlog. Disable the window after the historical
work completes to restore normal global scheduler-ranked and newest-first
selection.

Binary grouping evidence priority is documented in
[Binary Grouping Evidence](./binary-grouping-evidence.md). In short, complete
NNTP Subject multipart coordinates are stronger grouping evidence than random
poster/message-id context and can be stronger than a randomized recovered yEnc
`name=`.

Binary observation refresh must select local and accepted peer parts by the
requested binary IDs and bounded source-day window before hydrating local
article metadata. Do not expand the general `binary_effective_parts` view in
this writer path: PostgreSQL can otherwise choose a hash join over every hot
`article_headers` partition for each refresh chunk. Exact local header
hydration uses `(source_posted_at, article_header_id)` after the part set is
bounded.

Assemble exposes separate advanced runtime controls for each write phase:

- `binary_upsert_db_chunk_size` controls binary metadata rows per transaction
  chunk (default 1,000).
- `binary_part_upsert_db_chunk_size` controls binary-part rows per insert chunk
  within the part-upsert transaction (default 5,000).
- `binary_stats_refresh_db_chunk_size` controls binaries per stats-refresh
  transaction while preserving UTC source-day partition boundaries (default
  500).

These settings do not change the assemble claim size. `batch_size` remains the
number of article headers claimed by each assemble worker pass.

## yEnc Recovery

yEnc recovery claims `yenc_recovery_work_items`, fetches missing article
payload details, and writes recovered identity data to recovery-owned binary
projection rows. Priority admission first consumes
`article_cohort_yenc_queue`; retry and backoff state stays in the recovery
work table.

Opaque cohorts use sample-and-promote admission. Balanced samples 16 candidates
and Exhaustive samples 32. Repeated stable recovered identity promotes bounded
additional work; an exhausted random/no-identity sample becomes durable
`no_yield`. Candidate claims are limited by a persistent UTC-hour BODY budget.
Latest XOVER traffic has priority when the NNTP pool is hot; recovery and
inspection discovery yield instead of disabling upstream scrape/assembly.

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

Release persistence enqueues eligible discovery, PAR2, archive, and media work
immediately. A bounded release cursor reconciles missed events in
`(updated_at, release_id)` order. Candidate selection therefore reads a ready
queue; it does not perform a global binary eligibility scan.

Discovery is deliberately narrow: one representative opaque main-payload file
per complete release is sampled up to 4 KiB. A useful probe either recovers a
known archive/media/PAR2/NFO signature or applies an operator content filter.
Stage metrics report recovered signatures, filtered files, terminal skips,
retryable failures, sampled files, and bytes. Discovery has its own low hourly
probe budget and yields to scrape or yEnc traffic on a hot NNTP pool.

Direct media inspection is prefix-bounded. Matroska/WebM binaries are decoded
from their EBML `Info` and `Tracks` elements and parsing stops before media
clusters, retaining duration, embedded title, dimensions, codecs, audio tracks,
languages, subtitles, and dispositions without materializing the payload.
The same streaming parser is used for Matroska members extracted from archives;
the extractor is stopped after `Tracks`. Other direct and archive-member
containers use a bounded prefix with `ffprobe`. An inconclusive prefix is
recorded explicitly; `inspect_media` does not download a complete media file
merely to obtain container metadata.

Archive-member probing uses sparse temporary archive files. Split and single
7z archives materialize only the archive header, encoded-header ranges, and a
bounded leading region. RAR, ZIP, TAR, and other 7z-readable archive families
materialize a bounded leading region across sparse volume files; ZIP also
reserves part of that budget for its trailing directory. Standard and
obfuscated split-RAR names are normalized inside the temporary workspace so
the extractor can follow the volume sequence. The selected member is streamed
from the sparse archive into the Matroska parser or a bounded `ffprobe` input.
If the selected member or enough compressed data falls outside the populated
ranges, inspection records an explicit inconclusive/extraction result rather
than downloading the entire archive family.

Ready-queue reconciliation is an internal repair path, not a separately
scheduled supervisor stage. Operators only configure and schedule the
inspection consumers themselves.
