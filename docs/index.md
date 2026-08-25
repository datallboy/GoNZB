# GoNZB

GoNZB is a self-hosted Usenet indexer, Newznab aggregator, and decentralized
GoNZBNet node. It discovers and forms releases, exposes them to Newznab clients,
and can exchange signed release data with trusted peers.

GoNZB is not a downloader. SABnzbd or another SAB-compatible client owns
download queues, repair, extraction, and completed files.

## Start here

- **New installation:** follow the [beginner setup guide](getting-started/index.md).
- **Docker user:** use the [Docker installation](getting-started/docker.md).
- **Choosing a topology:** read the [deployment recommendations](getting-started/deployment.md).
- **Existing installation:** back up first, then follow [upgrades and backups](getting-started/upgrades.md).
- **Private sharing pool:** finish a normal install before using the
  [private pool quickstart](modules/private-pool.md).
- **Understand the modules:** read about the [Usenet indexer](modules/indexer.md)
  [completed-NZB uploader](modules/uploader.md),
  [posting worker](modules/worker.md), and [GoNZBNet](modules/gonzbnet.md).

## What you can run

| Goal | Enable | External services |
| --- | --- | --- |
| Search several existing indexers | Aggregator | One or more Newznab sources |
| Build your own NZB catalog | Indexer + aggregator | PostgreSQL and an NNTP provider |
| Review completed NZBs from a posting pipeline | Uploader, optionally aggregator | A producer that emits valid NZBs |
| Post selected seedbox payloads from a separate VPS | Posting worker + uploader | qBittorrent, SSHFS or rsync, and a posting engine |
| Share signed catalog data | GoNZBNet + aggregator | PostgreSQL and reachable pool peers |
| Run everything for one operator | Indexer + aggregator + GoNZBNet | PostgreSQL, NNTP, optional peers |

For most individuals, one all-in-one GoNZB process with PostgreSQL is the best
starting point. Enable GoNZBNet only after the local indexer and Newznab API are
working.

## Important security defaults

- The Compose stack binds the web UI to `127.0.0.1` by default.
- Do not expose port 8080 directly to the public Internet.
- Use a TLS reverse proxy or a private overlay network for remote access.
- Protect the SQLite store, PostgreSQL data, node identity key, and backups.
- Create dedicated least-privilege accounts and API tokens for automation.

Continue with [Getting started](getting-started/index.md).
