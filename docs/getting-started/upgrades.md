# Upgrades and backups

Always back up before changing versions. An application rollback is not enough
after a database migration has run.

## What to protect

- the GoNZB store volume, which contains SQLite settings/auth data, cached NZBs,
  and GoNZBNet identity keys;
- the PostgreSQL database;
- `.env` and `config.yaml`;
- reverse-proxy and private-network configuration.

Do not rely on copying a live PostgreSQL data directory. Use `pg_dump` or a
tested physical backup method.

## Compose backup example

Create a logical PostgreSQL backup:

```bash
docker compose exec -T postgres \
  pg_dump -U "${POSTGRES_USER:-postgres}" -d "${POSTGRES_DB:-gonzb}" \
  --format=custom > gonzb-postgres.backup
```

Back up the GoNZB named volume using your normal container-volume backup tool.
Stop GoNZB briefly or use a snapshot method that gives SQLite a consistent
point-in-time copy.

Keep backups encrypted because runtime databases contain credentials and API
keys that are not encrypted by GoNZB itself.

## Normal v0.8-or-newer upgrade

1. Read release notes and breaking changes.
2. Back up PostgreSQL, the GoNZB store, and configuration.
3. Fetch the release tag.
4. Rebuild and start the stack.
5. Watch migrations and readiness.
6. Test login, search, NZB retrieval, and enabled GoNZBNet roles.

```bash
git fetch --tags
git checkout v0.9.0
docker compose up -d --build
docker compose logs -f gonzb
```

The v0.9 test suite exercises a populated v0.8 PostgreSQL baseline and verifies
that provider and newsgroup data survive all later migrations.

## Upgrading a pre-v0.8 installation

Pre-v0.8 was an alpha schema. The v0.8 indexer introduced a new squashed
PostgreSQL baseline whose partition and queue layout differs substantially from
the final old migration chain. In-place PostgreSQL catalog upgrades from those
builds are not supported.

The supported path is:

1. Back up everything.
2. Preserve the GoNZB store/SQLite volume and configuration.
3. Initialize a fresh PostgreSQL database for the v0.9 indexer catalog.
4. Start GoNZB and let it migrate the preserved SQLite settings database.
5. Confirm users, runtime settings, NNTP providers, and API tokens.
6. Re-scrape/rebuild the replaceable indexer catalog.

The v0.9 settings migration recognizes the completed pre-v0.8 settings schema,
preserves users and operational settings, removes the retired embedded
downloader/ARR settings, and creates external SAB-compatible client settings.

Do not point v0.9 at the old PostgreSQL catalog and manually change its schema
version. The schemas are not equivalent.

## v0.9 downloader change

GoNZB no longer contains a downloader, download queue, repair/extraction
pipeline, SAB server API, or direct ARR notifier. Retired queue and downloader
settings tables are removed during migration.

Before upgrading, finish or move any work in the old embedded queue. After
upgrading:

- configure SABnzbd or another SAB-compatible client in automation software;
- optionally add it under **Admin > Settings > Download Clients** for GoNZB's
  manual **Send to downloader** action;
- point Radarr/Sonarr/Prowlarr at GoNZB only as a Newznab indexer.

## Restore testing

A backup is not complete until it has been restored to a disposable host or
database. Verify the restored instance can:

- start with the same version that created the backup;
- authenticate an administrator;
- read runtime settings and local releases;
- return Newznab capabilities and an NZB;
- retain the same GoNZBNet node ID, when applicable.
