# GoNZB Architecture

GoNZB is a modular monolith for Usenet downloading, aggregated search, and local Usenet/NZB indexing.

This document is the high-level reference for how the main modules fit together, what each module owns, and how the current bootstrap-plus-runtime-settings model works.

## Core Rules

1. Downloader, aggregator, and usenet indexer are separate ownership domains.
2. The aggregator must work without PostgreSQL unless the local usenet indexer is explicitly used as a source.
3. The downloader must not depend on PostgreSQL-backed indexer internals.
4. The web UI uses API surfaces instead of talking to storage directly.
5. Route registration, readiness, and runtime behavior are module-aware.
6. These deployment shapes must keep working:
   - downloader-only
   - aggregator-only
   - usenet-indexer-only
   - all-in-one

## Architecture Shape

GoNZB uses a pragmatic modular-monolith pattern:

1. `internal/runtime/wiring` is the composition root.
2. `internal/app/context.go` holds shared process state and module registrations.
3. Module facades expose behavior to HTTP, CLI, and runtime adapters.
4. Long-running services are managed through runtime modules.
5. Storage, transport, and external integrations stay at the edges.

Important module-facing contracts live in `internal/app/contracts.go`.

Current facades include:

- `DownloaderModule`
- `AggregatorModule`
- `SettingsAdmin`

## Bootstrap Config Vs Runtime Settings

GoNZB now keeps `config.yaml` intentionally small.

Bootstrap config is for values needed before the app can start:

- HTTP port
- hard module gates
- logging
- SQLite/blob/PostgreSQL bootstrap paths
- CORS

Operational settings are stored in SQLite runtime settings and are edited through the control plane:

- NNTP servers and credentials
- downloader output paths and behavior
- aggregator sources
- indexer newsgroups, stages, schedules, and enrichment settings
- maintenance and retention settings
- user, role, and user-token auth state

Newznab/NZB `apikey` values are generated account API tokens. They authenticate as the owning user and are authorized through that user's RBAC roles.

That means a fresh install usually follows this flow:

1. copy `config.yaml.example`
2. start `gonzb serve`
3. create the initial admin user at `/setup`
4. finish module configuration in `/admin/settings`

## Module Overview

### Downloader

Primary packages:

- `internal/downloader`
- `internal/engine`
- `internal/nntp`
- `internal/processor`
- `internal/nzb`
- `internal/store/sqlitejob`

What it owns:

- manual NZB enqueue
- enqueue by release ID
- queue lifecycle
- NNTP download execution
- extraction and post-processing
- queue/history/files/events APIs
- SAB-compatible downloader behavior

Storage:

- SQLite queue/job/history/event metadata
- filesystem work and output directories

Boundary rule:

- downloader features must not reach into PostgreSQL-backed indexer storage

### Aggregator

Primary packages:

- `internal/aggregator`
- `internal/aggregator/sources/*`
- `internal/resolver`
- `internal/store/blob`

What it owns:

- searching configured sources
- merging and normalizing release results
- resolving NZB payloads
- caching payloads
- native aggregated search
- Newznab-compatible search/get behavior

Current source types:

- external Newznab sources
- local blob-backed releases
- the local usenet indexer when `aggregator.sources.usenet_indexer.enabled` is enabled

Storage:

- filesystem blob store for NZB payloads
- optional SQLite-backed cache/search persistence

Boundary rule:

- aggregator behavior may use the local indexer as a source, but it should still depend on module contracts rather than indexer storage internals

### Usenet Indexer

Primary packages:

- `internal/indexing`
- `internal/indexing/scrape`
- `internal/indexing/assemble`
- `internal/indexing/release`
- `internal/indexing/inspect`
- `internal/indexing/enrich`
- `internal/indexing/scheduler`
- `internal/store/pgindex`

What it owns:

- scraping article headers from NNTP
- assembling binaries
- forming releases
- inspection and enrichment passes
- PostgreSQL-backed catalog and operational reporting
- public and admin indexer APIs

Storage:

- PostgreSQL catalog/index data

Reference:

- see the [Indexer Wiki](./wiki/indexer/README.md) for the stage-by-stage
  pipeline, schema, partition, retention, and release-formation contracts

Boundary rule:

- PostgreSQL-backed catalog ownership stays inside the usenet indexer module

### API

Primary packages:

- `internal/api`
- `internal/api/controllers`
- `internal/telemetry`

What it owns:

- route registration based on enabled modules
- request binding and validation
- transport DTO mapping
- auth, audit, and request middleware
- health and readiness endpoints

Controller rule:

- controllers should call facades or module services, not build internals directly

### Web UI

Primary packages:

- `internal/webui`
- `ui/`

What it owns:

- serving frontend assets when enabled
- setup, admin, and operator workflows on top of the API

Boundary rule:

- the UI should not bypass the API layer

### ARR Notifier

Primary packages:

- `internal/integrations/arr`
- `internal/runtime/wiring/arr.go`

This is a runtime support module that reacts to terminal downloader outcomes and notifies external ARR tools when configured.

## Runtime Modules

Runtime module contracts define:

- `Name()`
- `Enabled()`
- `Build(ctx)`
- `Start(ctx)`
- `Reload(ctx)`
- `Close()`
- `ReadinessChecks(ctx)`

Current runtime modules:

- downloader
- aggregator
- usenet_indexer
- arr_notifier

Runtime lifecycle behavior:

- startup builds enabled modules
- server mode starts long-running runtimes
- settings changes trigger reload where supported
- readiness probes call module-owned checks
- shutdown closes module resources

This keeps startup, reload, readiness, and shutdown on the same module boundaries instead of scattering that logic across the app.

## API Surface Ownership

Routes are registered only when the owning module is enabled.

### Downloader-Owned Routes

- `GET /api/v1/queue`
- `GET /api/v1/queue/history`
- `GET /api/v1/queue/:id`
- `GET /api/v1/queue/:id/files`
- `GET /api/v1/queue/:id/events`
- `POST /api/v1/queue`
- `POST /api/v1/queue/:id/cancel`
- `POST /api/v1/queue/bulk/cancel`
- `POST /api/v1/queue/bulk/delete`
- `POST /api/v1/queue/history/clear`
- `GET /api/v1/events/queue`
- `/api/sab?mode=...`

### Aggregator-Owned Routes

- `GET /api/v1/releases/search`
- `/api?t=...`
- `GET /nzb/:id`

### Indexer-Owned Routes

- `GET /api/v1/indexer/overview`
- `GET /api/v1/indexer/stages`
- `GET /api/v1/indexer/runs`
- `POST /api/v1/indexer/stages/:stage/run`
- `POST /api/v1/indexer/stages/:stage/pause`
- `POST /api/v1/indexer/stages/:stage/resume`
- `GET /api/v1/indexer/releases`
- `GET /api/v1/indexer/releases/:id`
- `GET /api/v1/indexer/binaries/:id`
- `GET /api/v1/indexer/files/:id`
- `GET /api/v1/admin/indexer/*`

### Shared Control-Plane And Auth Routes

- `GET /api/v1/admin/settings`
- `GET /api/v1/admin/capabilities`
- `PUT /api/v1/admin/settings`
- `/api/v1/auth/*`
- `/api/v1/admin/auth/*`

### Shared Compatibility Multiplexer

- `/api?mode=...` routes to SAB-compatible downloader behavior
- `/api?t=...` routes to Newznab-compatible aggregator behavior

### Probes

- `GET /healthz`
- `GET /readyz`

## Readiness Model

Readiness is reported through module-owned checks rather than ad hoc global inspection.

Examples:

- downloader checks queue manager, SQLite store, parser, and NNTP runtime
- aggregator checks that it has a runtime, a payload store, and at least one enabled source
- usenet indexer checks PostgreSQL availability and settings store health

This is why an enabled module can start but still report a not-ready state until its runtime settings are configured.

## CLI Shape

Primary entrypoint:

- `cmd/gonzb/main.go`

Common commands:

- `gonzb serve`
- `gonzb --file <nzb>`
- `gonzb indexer scrape`
- `gonzb indexer scrape latest`
- `gonzb indexer scrape backfill --once`
- `gonzb indexer assemble --once`
- `gonzb indexer release --once`
- `gonzb indexer pipeline --once`
- `gonzb indexer inspect ...`
- `gonzb indexer enrich ...`
- `gonzb indexer maintenance`

CLI rule:

- CLI adapters should call runtime commands or module services instead of duplicating business logic

## Storage Overview

### SQLite

Used for:

- downloader metadata
- auth state
- runtime settings
- optional aggregator cache/search persistence

### Filesystem Blob Store

Used for:

- NZB payload cache files

### PostgreSQL

Used for:

- usenet indexer catalog and pipeline state

## Extension Guidelines

When adding new work:

1. decide which module owns the behavior
2. add or extend the module use case first
3. add storage or integration adapters only where needed
4. keep HTTP, CLI, and UI adapters thin
5. preserve optional deployment combinations

Avoid:

- putting business logic directly in controllers
- passing `*app.Context` deep into feature code outside runtime wiring
- creating direct cross-module storage dependencies

For indexer-specific work:

- scrape behavior belongs in `internal/indexing/scrape`
- binary matching and grouping belongs in assemble or match code
- release formation belongs in `internal/indexing/release`
- inspection and enrichment belong in their dedicated stage packages
- schema and repository changes belong in `internal/store/pgindex`
