# Schema Migrations

## Compatibility Boundary

PostgreSQL schema stability begins with v0.9.0. Schema version 36 is the
canonical v0.9 baseline:

- a new database applies the v0.9 baseline directly and is stamped at version
  36;
- a versioned v0.8 database applies the original incremental migrations through
  version 36 so existing data is preserved;
- an unversioned, non-empty database is rejected instead of being overwritten;
- a database newer than the running GoNZB build is rejected instead of being
  opened with older code.

Diagnostic PostgreSQL extensions installed by the Compose stack are not GoNZB
schema objects and do not prevent a fresh baseline from being applied.

Pre-v0.8 PostgreSQL catalogs remain outside this compatibility boundary. Their
supported upgrade path is a fresh PostgreSQL index catalog plus a re-scrape.
The separate SQLite settings store recognizes the completed pre-v0.8 settings
schema and preserves users, providers, credentials, and runtime settings.

## Migration Numbering

The next PostgreSQL migration after the v0.9 baseline is `037`. Do not reuse a
number or edit a migration after it has appeared in a release. Fresh installs
may use a newer canonical baseline in a future major release, but every
supported released schema must retain a tested forward path.

Migration startup is serialized with a PostgreSQL advisory lock. This prevents
two GoNZB processes from racing to initialize or upgrade the same database.
Each migration and its schema-version update commit in one transaction.

## Change Policy

Use additive changes whenever practical. A destructive or incompatible change
uses an expand/migrate/contract sequence:

1. **Expand:** add nullable columns, new tables, indexes, or compatibility
   projections without removing the old representation.
2. **Migrate:** backfill in bounded batches and make the operation restartable.
3. **Cut over:** deploy application code that reads the new representation and,
   when necessary, temporarily writes both representations.
4. **Contract:** remove the old representation in a later release after the
   supported upgrade window has passed.

Avoid long table rewrites, unbounded data updates, and blocking index builds in
normal startup migrations. Large backfills belong in resumable maintenance
workflows with visible progress.

GoNZB migrations are forward-only. Operators must back up PostgreSQL and the
GoNZB SQLite/store volume before upgrading. Rolling the application binary back
does not reverse a committed schema migration; restore the matching backup when
a database rollback is required.

## Required Validation

Every PostgreSQL schema change must keep these paths green:

- fresh installation from the canonical baseline;
- populated v0.8-or-newer upgrade without losing durable data;
- logical schema equivalence between the v0.9 baseline and migrations 001-036;
- migration restart/idempotency;
- serialized concurrent startup;
- rejection of unversioned non-empty and newer schemas;
- the full PostgreSQL integration and query-soak suite.

When a migration changes an index or a hot query surface, also update
[Schema And Partitions](./schema-and-partitions.md) and validate the affected
query shape with `EXPLAIN (ANALYZE, BUFFERS)` against representative data.
