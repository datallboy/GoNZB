# Indexer Operations Playbook

## Quick Checks

Run targeted tests after hot query or schema changes:

```sh
go test ./internal/store/pgindex ./internal/indexing/assemble ./internal/runtime/wiring
```

Check patch hygiene:

```sh
git diff --check
```

## Database Checks

List partitioned target tables:

```sql
SELECT cls.relname
FROM pg_class cls
JOIN pg_namespace ns ON ns.oid = cls.relnamespace
JOIN pg_partitioned_table pt ON pt.partrelid = cls.oid
WHERE ns.nspname = 'public'
ORDER BY cls.relname;
```

Check default partitions:

```sql
SELECT inhparent::regclass AS parent, inhrelid::regclass AS child
FROM pg_inherits
JOIN pg_class child ON child.oid = inhrelid
WHERE pg_get_expr(child.relpartbound, child.oid) = 'DEFAULT'
ORDER BY parent::text;
```

Normal latest indexing proactively provisions the scrape bundle for the current
UTC day and two days ahead. Exact older days are provisioned from the dates
actually returned by XOVER before their rows are written. Backfill does not
require widening a date horizon.

A scrape pass introduces at most 32 new source days by default. Additional
article-number ranges appear in the deferred-range view and drain separately so
they do not block latest checks.

Any default-partition row is an operator-visible fault. Pause all indexer
writers, run the default-rehome dry-run for the affected day, then execute the
offline rehome. Do not schedule default rehome while indexer stages are active.

Outcome retention is audit-only by default. Review terminal reasons, archive
durability, and default-partition health before enabling destructive purge.

## Runtime Checks

Use the admin API for runtime settings and maintenance tasks. Prefer `gonzb
serve` for supervisor testing. Use one-shot commands only for explicit
maintenance or cleanup tasks.
