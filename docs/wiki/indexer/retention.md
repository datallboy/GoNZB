# Indexer Retention

## Partition Retention

Partition retention drops whole daily partitions for source/work/projection
tables after safety checks pass. It does not drop durable release catalog,
archive, NZB cache, or enrichment tables.

Raw staging is expected to be short-lived. Default policy should target roughly
24-48 hours for hot/warm staging, shorter windows for cold samples under disk
pressure, and compact deferred ranges instead of retaining raw rows for work
that cannot be consumed in time.

## Drop Order

1. Release-derived work queues.
2. Inspect ready/history/evidence and yEnc work/evidence.
3. Binary projection/work tables.
4. `binary_parts`.
5. Article support rows.
6. `article_headers`.
7. Unreferenced old `binary_core` roots only after archive/catalog gates.

## Blockers

Retention must refuse a day when any of these are true:

- active source work still exists for the day;
- assemble queue rows still exist;
- ready or running yEnc work still exists;
- running inspect work still exists;
- non-terminal release/archive dependencies still reference the day;
- default partitions contain rows;
- expected partition parents or daily children are missing.

## Operational Meaning

Retention is not a backlog throttle. Scrape/backfill caps prevent uncontrolled
growth while the system is running. Partition retention is terminal cleanup
after downstream stages and release/archive safety gates complete.

Dry-run mode must report eligible partitions, blockers, estimated row/bytes
impact when available, and the intended drop order. A dry-run must not perform
row-level cleanup as a substitute for partition drops.
