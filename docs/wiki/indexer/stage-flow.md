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

Runtime group tiering controls how much work each group can admit:

- hot groups get freshness priority and the largest recovery budget;
- warm groups run while queue depth and recovery lag are healthy;
- cold groups are sampled or deferred and must not starve hot groups.

Hard caps stop new scrape/recovery admission when downstream queues are above
their configured limits. Soft pressure should reduce backfill first, then warm
and cold work, while preserving latest hot-group freshness where possible.

## Assemble

Assemble claims queue rows from `article_header_assembly_queue`, hydrates exact
`(source_posted_at, article_header_id)` source facts, writes binary rows, then
deletes completed queue rows.

Lane A extends incomplete binaries using `binary_completion_keys`. Lane B
creates or updates general binary records from recent queue rows.

## yEnc Recovery

yEnc recovery claims `yenc_recovery_work_items`, fetches missing article
payload details, and writes recovered identity data to recovery-owned binary
projection rows. Retry and backoff state stays in the recovery work table.

Recovery priority should favor near-complete binaries/releases, fresh hot-group
work, high-yield groups, warm fresh work, cold samples, and finally backfill.
Header-time/message-id/article-number cohorts may be used to prioritize probes
only after measured evidence supports the signal; exact release grouping still
requires recovered yEnc or other strong identity evidence.

## Release Refresh And Formation

Release summary refresh aggregates binary projection rows into release-family
readiness summaries and ready candidates. Release formation consumes those
ready candidates and writes release catalog/lineage state.

## Inspect

Inspect stages consume `binary_inspection_ready_queue` and write inspection
history/evidence tables. Inspection results can improve archive, media, text,
and PAR2 visibility without using upstream source tables as progress state.
