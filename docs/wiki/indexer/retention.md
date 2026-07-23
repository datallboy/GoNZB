# Indexer Retention

## Retention Principle

Article post age is a storage coordinate, not a usefulness decision. A March
article discovered by a latest scrape in July starts its processing lifetime in
July. It must not be purged merely because `source_posted_at` is old.

Accepted work remains active until it produces a durably archived release or
is explicitly classified as exhausted/no-yield after bounded processing.

## Outcome Lifecycle

Source-day state is reconciled per provider and newsgroup. A bucket may become
no-yield only when all of the following are true:

- seven days have passed since its last ingestion or useful progress;
- the source range has been settled for at least 24 hours;
- assembly and yEnc effort budgets are exhausted;
- no ready/running queue, lease, inspection, release-dirty family, or
  actionable release candidate remains;
- no release, archive, or catalog dependency references the work.

A successful release is terminal only after its NZB is stored durably and its
durable release catalog files exist. New matching input reactivates terminal
work that has not yet been purged.

Outcome reconciliation and dry-run reporting are enabled by default.
Destructive outcome purge is disabled until an operator reviews the report and
enables it explicitly.

The **Indexer Work** page reports the ledger directly: active, successful,
no-yield, purge-eligible, and purged bucket totals plus the provider/group/day
rows with their header counts, open work, durable release count, terminal
reason, and last progress. This is the primary check before reviewing a
partition-retention dry-run.

## Partition Retention

Partition retention drops whole daily partitions for source/work/projection
tables only after every bucket represented by the day is terminal and safety
checks pass. Mixed days use bounded row cleanup for terminal roots while active
work remains. It does not drop durable release catalog, archive, NZB cache, or
enrichment tables.

Raw staging should still be short-lived, but post age and hot/warm/cold tier do
not authorize deletion. Tier affects admission priority; explicit outcome and
last-progress state control retention.

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

Missing children for stage bundles that never received work are not blockers.
Default rows block only their own source day, not unrelated retention days.

## Operational Meaning

Retention is not a backlog throttle. Latest work receives reserved recovery
capacity, backfill and low-yield recovery defer first, and sparse XOVER ranges
admit a bounded number of new source days per pass. Partition retention remains
terminal cleanup after downstream stages and release/archive safety gates
complete.

Dry-run mode must report eligible partitions, blockers, estimated row/bytes
impact when available, and the intended drop order. A dry-run must not perform
row-level cleanup as a substitute for partition drops.
