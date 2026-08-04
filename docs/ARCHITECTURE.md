# GoNZB Architecture

GoNZB is a modular monolith with three product domains: aggregator, Usenet
indexer, and GoNZBNet. Authentication, runtime settings, HTTP, and storage are
shared infrastructure. Download execution is deliberately outside the process.

## Ownership boundaries

### Aggregator

`internal/aggregator` owns normalized release search across external Newznab,
the local indexer, accepted GoNZBNet data, and the optional blob/cache source.
It owns Newznab-compatible search and NZB retrieval through `/api` and
`/nzb/:id`.

The aggregator can run without PostgreSQL when none of its selected sources
requires the local indexer or PostgreSQL-backed GoNZBNet data.

### Usenet indexer

`internal/indexer` and `internal/store/pgindex` own NNTP header ingestion,
partitioned article storage, binary assembly, yEnc recovery, release formation,
inspection, enrichment, retention, NZB generation, and the PostgreSQL catalog.
The maintained stage and schema contracts live in `docs/wiki/indexer/`.

The local indexer is exposed to clients by enabling it as an aggregator source;
the Newznab compatibility edge remains aggregator-owned.

### GoNZBNet

`internal/gonzbnet` owns node identity, signed federation events, pools,
membership, roles, peer synchronization, manifest exchange, validation,
coverage coordination, and direct binary evidence. It may publish from the
indexer and project accepted remote releases into the aggregator without
making those modules import GoNZBNet internals.

### External download-client adapter

`internal/downloadclient` is a small outbound adapter, not a runtime module. It
resolves an existing GoNZB release/NZB and submits it to an administrator-
configured SAB-compatible endpoint. It has no queue, NNTP body downloader,
repair/extraction pipeline, history, filesystem manager, or ARR notifier.

The adapter is invoked only by the explicit local release action and is guarded
by `download_clients.send`. The external client owns all subsequent state.

## Runtime and configuration

`config.yaml` provides bootstrap state: listener settings, hard module gates,
logging, store paths/DSNs, and GoNZBNet protocol bootstrap settings. SQLite
runtime settings provide live-editable NNTP providers, aggregator sources,
SAB-compatible clients, indexer stages, and GoNZBNet operator settings.

Runtime construction lives under `internal/runtime/wiring`. Module bindings use
interfaces from `internal/app`; concrete stores and services are not reached
across domains directly. Settings reload rebuilds affected runtime components.

## Storage

- SQLite: users, roles, sessions, API tokens, runtime settings, and optional
  aggregator cache metadata
- PostgreSQL: indexer pipeline/catalog state and GoNZBNet state
- blob store: cached and archived NZBs
- external SAB-compatible client: download queue, incomplete/complete files,
  repair, extraction, and history

SQLite secret columns are not encrypted at the application layer. Operators
must protect configuration, database volumes, snapshots, and backups.

## Request flows

### Newznab automation

```text
Radarr/Sonarr/Prowlarr
  -> GoNZB /api (account token)
  -> aggregator searches configured sources
  -> client requests NZB from GoNZB
  -> automation submits NZB to its configured downloader
  -> automation imports the completed download
```

GoNZB neither receives download-completion callbacks nor talks directly to ARR
applications.

### Manual Send to downloader

```text
authenticated operator
  -> local release detail action
  -> release resolver generates or retrieves the NZB
  -> outbound SAB addfile request
  -> client ID and optional external job ID returned
```

### Local indexing

```text
NNTP XOVER
  -> article partitions
  -> binary assembly / evidence recovery
  -> release formation
  -> inspection and enrichment
  -> public-ready release
  -> NZB generation/archive
  -> local aggregator source
```

## Authentication and exposure

Browser sessions use CSRF protection. Newznab clients use account API tokens
and inherit their account's RBAC permissions. A dedicated viewer account is the
recommended least-privilege Newznab identity.

The release submission action requires an authenticated operator/admin with
`download_clients.send`. Download-client configuration requires admin settings
permissions. The outbound adapter rejects URL credentials and HTTP redirects;
its destination and API key are administrator-controlled.

Health and readiness probes are unauthenticated for infrastructure use. The
application listener should remain private or sit behind a correctly scoped TLS
reverse proxy. GoNZBNet peer endpoints have separate signed-node security and
deployment guidance.

## Supported deployment shapes

- aggregator-only with external/federated sources
- local indexer plus aggregator/Newznab API
- standalone or integrated GoNZBNet node
- all-in-one indexer, aggregator, and GoNZBNet node

There is no downloader-only deployment. Disabling the aggregator removes the
Newznab endpoint even when the local indexer is enabled.
