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

## Runtime Checks

Use the admin API for runtime settings and maintenance tasks. Prefer `gonzb run
serve` for supervisor testing. Use one-shot commands only for explicit
maintenance or cleanup tasks.
